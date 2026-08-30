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
$webView2DownloaderPath = Join-Path $PSScriptRoot 'download-webview2-bootstrapper.ps1'
if (-not (Test-Path -LiteralPath $webView2DownloaderPath -PathType Leaf)) {
  throw 'WebView2 Bootstrapper downloader script is missing.'
}
$webView2Tokens = $null
$webView2ParseErrors = $null
[void][System.Management.Automation.Language.Parser]::ParseFile(
  $webView2DownloaderPath,
  [ref]$webView2Tokens,
  [ref]$webView2ParseErrors
)
if ($webView2ParseErrors.Count -ne 0) {
  throw "WebView2 Bootstrapper downloader has PowerShell syntax errors: $($webView2ParseErrors[0].Message)"
}
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

function Get-WebView2RuntimeVersions {
  $versions = @()
  $clientKey = 'SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
  foreach ($hive in @([Microsoft.Win32.RegistryHive]::LocalMachine, [Microsoft.Win32.RegistryHive]::CurrentUser)) {
    $base = [Microsoft.Win32.RegistryKey]::OpenBaseKey($hive, [Microsoft.Win32.RegistryView]::Registry32)
    try {
      $key = $base.OpenSubKey($clientKey, $false)
      if ($null -ne $key) {
        try {
          $version = [string]$key.GetValue('pv', $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
          $parsed = [version]'0.0.0.0'
          if ([version]::TryParse($version, [ref]$parsed) -and $parsed -gt [version]'0.0.0.0') {
            $versions += $parsed.ToString()
          }
        } finally {
          $key.Dispose()
        }
      }
    } finally {
      $base.Dispose()
    }
  }
  return @($versions | Sort-Object -Unique)
}

function Assert-PathMissing([string]$path, [string]$description) {
  if (Test-Path -LiteralPath $path) {
    throw "Pre-existing $description was found. The smoke test will not overwrite or remove it: $path"
  }
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

function Assert-VersionMetadata([string]$path, [string]$description) {
  if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
    throw "$description is missing: $path"
  }
  $versionInfo = (Get-Item -LiteralPath $path).VersionInfo
  # Wails' neutral-language resource may expose FileVersion/ProductVersion
  # strings as empty through the current Windows locale even while the PE
  # fixed-version block is correct. Include both surfaces; the fixed block is
  # the authoritative version resource consumed by Windows itself.
  $fixedVersions = @(
    "$($versionInfo.FileMajorPart).$($versionInfo.FileMinorPart).$($versionInfo.FileBuildPart).$($versionInfo.FilePrivatePart)",
    "$($versionInfo.ProductMajorPart).$($versionInfo.ProductMinorPart).$($versionInfo.ProductBuildPart).$($versionInfo.ProductPrivatePart)"
  )
  $versionValues = @(
    [string]$versionInfo.FileVersion,
    [string]$versionInfo.ProductVersion
  ) + $fixedVersions
  $versionValues = @($versionValues | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
  Write-Host "$description version metadata: $($versionValues -join '; ')"
  $expectedPattern = '(^|[^0-9])' + [regex]::Escape($version) + '(?:\.0)?($|[^0-9])'
  if (-not ($versionValues | Where-Object { $_ -match $expectedPattern })) {
    throw "$description version metadata does not include $version."
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
Assert-VersionMetadata $setupPath 'Setup EXE'

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
Assert-VersionMetadata $mainExecutable 'Installed XIASS Tools executable'
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
if (-not (Test-Path -LiteralPath $startMenuUninstallShortcut -PathType Leaf)) {
  throw "Expected Start Menu uninstall shortcut is missing: $startMenuUninstallShortcut"
}
$webView2Versions = @(Get-WebView2RuntimeVersions)
if ($webView2Versions.Count -eq 0) {
  throw 'WebView2 Runtime was not available after installation.'
}
Write-Host "Detected WebView2 Runtime after installation: $($webView2Versions -join ', ')"

Write-Host 'Silently uninstalling the freshly installed package through its Start Menu shortcut…'
# This validates the user-facing Shell action itself. Some PowerShell COM
# projections report an empty TargetPath for a valid Unicode-named .lnk, so a
# successful shell invocation plus complete cleanup is stronger evidence than
# accepting that property value or merely checking the shortcut file exists.
[void](Start-Process -FilePath $startMenuUninstallShortcut -ArgumentList @('/S') -PassThru)

$deadline = [DateTime]::UtcNow.AddSeconds(30)
while (-not (Test-InstallerStateAbsent)) {
  if ([DateTime]::UtcNow -ge $deadline) {
    throw 'Installer lifecycle smoke test timed out: the Start Menu uninstall shortcut did not remove files, shortcuts, a desktop link, and its registry entry.'
  }
  Start-Sleep -Milliseconds 250
}

Write-Host 'Windows Installer Lifecycle Smoke Test passed.'
