[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

# This test intentionally runs only against an installer created seconds ago on
# an ephemeral GitHub-hosted Windows worker. It never removes pre-existing
# files, shortcuts, or registry entries: an unexpected existing state is a
# release-blocking test failure rather than something CI is allowed to clean up.
if ($env:GITHUB_ACTIONS -ne 'true') {
  throw 'Windows Installer Lifecycle Smoke Test may run only on a GitHub Actions hosted runner.'
}

$sourceRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$versionPath = Join-Path $sourceRoot 'VERSION'
if (-not (Test-Path -LiteralPath $versionPath -PathType Leaf)) {
  throw 'VERSION file is missing.'
}
$version = (Get-Content -LiteralPath $versionPath -Raw).Trim()
if ($version -notmatch '^\d+\.\d+\.\d+$') {
  throw "VERSION must use MAJOR.MINOR.PATCH form, got: $version"
}

$appName = 'XIASS Tools'
$legacyUninstallSubKey = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\AntigravityWFAssistant'
$setupPath = Join-Path $sourceRoot (Join-Path 'build\bin' "XIASS-Tools-Windows-x64-v$version-Setup.exe")
$installDirectory = Join-Path $env:LOCALAPPDATA (Join-Path 'Programs' $appName)
$mainExecutable = Join-Path $installDirectory "$appName.exe"
$uninstaller = Join-Path $installDirectory "Uninstall $appName.exe"
$programsDirectory = [Environment]::GetFolderPath([Environment+SpecialFolder]::Programs)
$startMenuDirectory = Join-Path $programsDirectory $appName
$startMenuMainShortcut = Join-Path $startMenuDirectory "$appName.lnk"
$startMenuUninstallShortcut = Join-Path $startMenuDirectory "卸载 $appName.lnk"
$desktopDirectory = [Environment]::GetFolderPath([Environment+SpecialFolder]::DesktopDirectory)
$desktopShortcut = Join-Path $desktopDirectory "$appName.lnk"

if (-not (Test-Path -LiteralPath $setupPath -PathType Leaf)) {
  throw "The expected Setup EXE was not produced: $setupPath"
}
if ((Get-Item -LiteralPath $setupPath).Length -le 0) {
  throw 'The Setup EXE is empty.'
}

function Get-UninstallEntries {
  $entries = @()
  # A current-user uninstall key can be surfaced through both Registry64 and
  # Registry32 on a 64-bit runner even when it is one physical entry. Inspect
  # both views, but de-duplicate exact metadata aliases so the smoke test does
  # not mistake a single successful install for two separate registrations.
  # Different metadata remains a separate entry and still fails the test.
  $seen = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::OrdinalIgnoreCase)
  foreach ($view in @([Microsoft.Win32.RegistryView]::Registry64, [Microsoft.Win32.RegistryView]::Registry32)) {
    $base = [Microsoft.Win32.RegistryKey]::OpenBaseKey([Microsoft.Win32.RegistryHive]::CurrentUser, $view)
    try {
      $key = $base.OpenSubKey($legacyUninstallSubKey, $false)
      if ($null -ne $key) {
        try {
          $entry = [pscustomobject]@{
            View = $view.ToString()
            DisplayName = [string]$key.GetValue('DisplayName', $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            DisplayVersion = [string]$key.GetValue('DisplayVersion', $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            Publisher = [string]$key.GetValue('Publisher', $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            InstallLocation = [string]$key.GetValue('InstallLocation', $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            DisplayIcon = [string]$key.GetValue('DisplayIcon', $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            UninstallString = [string]$key.GetValue('UninstallString', $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
          }
          $identity = [string]::Join("`0", @(
            $entry.DisplayName,
            $entry.DisplayVersion,
            $entry.Publisher,
            $entry.InstallLocation,
            $entry.DisplayIcon,
            $entry.UninstallString
          ))
          if ($seen.Add($identity)) {
            $entries += $entry
          }
        } finally {
          $key.Dispose()
        }
      }
    } finally {
      $base.Dispose()
    }
  }
  return @($entries)
}

function Assert-PathMissing([string]$path, [string]$description) {
  if (Test-Path -LiteralPath $path) {
    throw "Pre-existing $description was found. The smoke test will not overwrite or remove it: $path"
  }
}

function Get-NativeShortcutTarget([string]$shortcutPath) {
  # WScript.Shell is normally enough, but its PowerShell 7 COM projection can
  # return an empty TargetPath for a valid shortcut whose display name is
  # Unicode. Fall back to the native Unicode IShellLinkW interface before
  # treating the installer output as invalid.
  if (-not ('XiassToolsInstallerSmoke.NativeShortcut' -as [type])) {
    Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
using System.Text;

namespace XiassToolsInstallerSmoke
{
    [ComImport]
    [Guid("000214F9-0000-0000-C000-000000000046")]
    [InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    internal interface IShellLinkW
    {
        void GetPath([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder path, int capacity, IntPtr findData, uint flags);
    }

    [ComImport]
    [Guid("0000010B-0000-0000-C000-000000000046")]
    [InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    internal interface IPersistFile
    {
        void GetClassID(out Guid classId);
        [PreserveSig] int IsDirty();
        void Save([MarshalAs(UnmanagedType.LPWStr)] string fileName, bool remember);
        void SaveCompleted([MarshalAs(UnmanagedType.LPWStr)] string fileName);
        void Load([MarshalAs(UnmanagedType.LPWStr)] string fileName, uint mode);
    }

    public static class NativeShortcut
    {
        private static readonly Guid ShellLinkClassId = new Guid("00021401-0000-0000-C000-000000000046");

        public static string GetTargetPath(string shortcutPath)
        {
            Type shellLinkType = Type.GetTypeFromCLSID(ShellLinkClassId);
            if (shellLinkType == null)
            {
                throw new InvalidOperationException("Windows Shell Link COM class is unavailable.");
            }

            object shellLink = Activator.CreateInstance(shellLinkType);
            try
            {
                ((IPersistFile)shellLink).Load(shortcutPath, 0);
                var target = new StringBuilder(32768);
                ((IShellLinkW)shellLink).GetPath(target, target.Capacity, IntPtr.Zero, 0);
                return target.ToString();
            }
            finally
            {
                if (shellLink != null && Marshal.IsComObject(shellLink))
                {
                    Marshal.FinalReleaseComObject(shellLink);
                }
            }
        }
    }
}
'@
  }

  return ([XiassToolsInstallerSmoke.NativeShortcut]::GetTargetPath($shortcutPath)).Trim().Trim([char]'"')
}

function Assert-ShortcutTarget([string]$shortcutPath, [string]$expectedTarget) {
  if (-not (Test-Path -LiteralPath $shortcutPath -PathType Leaf)) {
    throw "Expected Start Menu shortcut is missing: $shortcutPath"
  }
  $shell = New-Object -ComObject WScript.Shell
  try {
    $shortcut = $shell.CreateShortcut($shortcutPath)
    # WScript may preserve outer quotes around a target supplied by an
    # installer. They are syntax, not part of the filesystem path.
    $targetPath = ([string]$shortcut.TargetPath).Trim().Trim([char]'"')
    if ([string]::IsNullOrWhiteSpace($targetPath)) {
      $targetPath = Get-NativeShortcutTarget $shortcutPath
    }
    if ([string]::IsNullOrWhiteSpace($targetPath) -or -not (Test-Path -LiteralPath $targetPath -PathType Leaf)) {
      $targetLeaf = [System.IO.Path]::GetFileName($targetPath)
      throw "Shortcut target is missing for $shortcutPath (target filename: $targetLeaf)"
    }
    $actualTarget = (Get-Item -LiteralPath $targetPath).FullName
    $expectedResolvedTarget = (Get-Item -LiteralPath $expectedTarget).FullName
    if (-not [string]::Equals($actualTarget, $expectedResolvedTarget, [System.StringComparison]::OrdinalIgnoreCase)) {
      throw "Shortcut target mismatch for $shortcutPath"
    }
  } finally {
    if ($null -ne $shortcut) { [void][Runtime.InteropServices.Marshal]::ReleaseComObject($shortcut) }
    [void][Runtime.InteropServices.Marshal]::ReleaseComObject($shell)
  }
}

function Test-InstallerStateAbsent {
  return -not (Test-Path -LiteralPath $installDirectory) `
    -and -not (Test-Path -LiteralPath $startMenuDirectory) `
    -and -not (Test-Path -LiteralPath $desktopShortcut) `
    -and ((Get-UninstallEntries).Count -eq 0)
}

if ((Get-UninstallEntries).Count -ne 0) {
  throw 'A current-user XIASS Tools / legacy upgrade uninstall entry already exists. Refusing to mutate a persistent state.'
}
Assert-PathMissing $installDirectory 'installation directory'
Assert-PathMissing $startMenuDirectory 'Start Menu directory'
Assert-PathMissing $desktopShortcut 'desktop shortcut'

Write-Host 'Installing the freshly built Setup EXE in silent mode…'
$installProcess = Start-Process -FilePath $setupPath -ArgumentList @('/S') -Wait -PassThru
if ($installProcess.ExitCode -ne 0) {
  throw "Silent installer exited with code $($installProcess.ExitCode)."
}

foreach ($path in @($mainExecutable, $uninstaller)) {
  if (-not (Test-Path -LiteralPath $path -PathType Leaf) -or (Get-Item -LiteralPath $path).Length -le 0) {
    throw "Installed executable is missing or empty: $path"
  }
}
if (Test-Path -LiteralPath $desktopShortcut) {
  throw 'A desktop shortcut was created by the silent default installation, but the optional desktop component must remain opt-in.'
}

$entries = @(Get-UninstallEntries)
if ($entries.Count -ne 1) {
  throw "Expected exactly one current-user uninstall entry after installation, found $($entries.Count)."
}
$entry = $entries[0]
$expectedUninstallString = '"' + $uninstaller + '"'
if ($entry.DisplayName -ne $appName -or $entry.DisplayVersion -ne $version -or $entry.Publisher -ne $appName -or $entry.InstallLocation -ne $installDirectory -or $entry.DisplayIcon -ne $mainExecutable -or $entry.UninstallString -ne $expectedUninstallString) {
  throw "Uninstall metadata does not match the freshly installed XIASS Tools package (registry $($entry.View) view)."
}
Assert-ShortcutTarget $startMenuMainShortcut $mainExecutable
Assert-ShortcutTarget $startMenuUninstallShortcut $uninstaller

Write-Host 'Silently uninstalling the freshly installed package…'
$uninstallProcess = Start-Process -FilePath $uninstaller -ArgumentList @('/S') -Wait -PassThru
if ($uninstallProcess.ExitCode -ne 0) {
  throw "Silent uninstaller exited with code $($uninstallProcess.ExitCode)."
}

$deadline = [DateTime]::UtcNow.AddSeconds(30)
while (-not (Test-InstallerStateAbsent)) {
  if ([DateTime]::UtcNow -ge $deadline) {
    throw 'Installer lifecycle smoke test timed out: uninstall left files, shortcuts, a desktop link, or a registry entry behind.'
  }
  Start-Sleep -Milliseconds 250
}

Write-Host 'Windows Installer Lifecycle Smoke Test passed.'
