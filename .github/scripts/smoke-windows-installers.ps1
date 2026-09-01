param(
  [Parameter(Mandatory = $true)]
  [string]$BundleRoot,
  [Parameter(Mandatory = $true)]
  [string]$SmokeRoot,
  [Parameter(Mandatory = $true)]
  [string]$BridgeSmokeScript
)

$ErrorActionPreference = 'Stop'
$ProductName = 'XIASS Tools'
$ExecutableName = 'xiass-tools.exe'
$OfflineWebView2RuntimeName = 'MicrosoftEdgeWebView2RuntimeInstaller.exe'
$MinimumOfflineInstallerBytes = 80MB
$InstallerOperationTimeoutSeconds = 300
$RequiredLicenseNames = @(
  'CC-BY-NC-SA-4.0-LEGALCODE.txt',
  'XIASS-Tools-MIT.txt',
  'XIASS-Tools-Nextgen-CC-BY-NC-SA-4.0.txt',
  'ORIGIN_AND_LICENSE.md',
  'THIRD_PARTY_NOTICES.md',
  'Tauri-APACHE-2.0.txt',
  'Tauri-MIT.txt',
  'jsQR-APACHE-2.0.txt',
  'Lucide-ISC-and-MIT.txt',
  'protobufjs-BSD-3-Clause.txt',
  'React-MIT.txt',
  'CLIProxyAPI-MIT.txt'
)
$BridgeSmokeScript = (Resolve-Path -LiteralPath $BridgeSmokeScript -ErrorAction Stop).Path
$UninstallRoots = @(
  'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*',
  'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*',
  'HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*'
)

function Get-XiassUninstallEntry {
  foreach ($root in $UninstallRoots) {
    $entry = Get-ItemProperty $root -ErrorAction SilentlyContinue |
      Where-Object { $_.DisplayName -eq $ProductName } |
      Select-Object -First 1
    if ($entry) { return $entry }
  }
  return $null
}

function Get-PathFromCommand([string]$CommandLine) {
  $value = $CommandLine.Trim()
  if (-not $value) { return $null }
  if ($value.StartsWith('"')) {
    $endQuote = $value.IndexOf('"', 1)
    if ($endQuote -gt 1) { return $value.Substring(1, $endQuote - 1) }
  }
  return ($value -split '\s+', 2)[0]
}

function Get-XiassMsiBinaryNames([string]$MsiPath) {
  $installer = $null
  $database = $null
  $view = $null
  $record = $null
  $names = [System.Collections.Generic.List[string]]::new()
  try {
    # WiX stores Tauri's offline WebView2 payload in the MSI Binary table. Query
    # the installer database rather than relying on a runner that already has
    # WebView2 installed.
    $installer = New-Object -ComObject WindowsInstaller.Installer
    $database = $installer.OpenDatabase($MsiPath, 0)
    $view = $database.OpenView('SELECT `Name` FROM `Binary`')
    $view.Execute()
    while ($true) {
      $record = $view.Fetch()
      if ($null -eq $record) { break }
      $names.Add([string]$record.StringData(1))
      [void][System.Runtime.InteropServices.Marshal]::FinalReleaseComObject($record)
      $record = $null
    }
  }
  finally {
    if ($view) { $view.Close() }
    if ($database) { $database.Close() }
    foreach ($comObject in @($record, $view, $database, $installer)) {
      if ($null -ne $comObject -and [System.Runtime.InteropServices.Marshal]::IsComObject($comObject)) {
        [void][System.Runtime.InteropServices.Marshal]::FinalReleaseComObject($comObject)
      }
    }
  }
  return $names.ToArray()
}

function Assert-XiassOfflineWebView2Payload([System.IO.FileInfo]$Msi, [System.IO.FileInfo]$Nsis) {
  foreach ($installer in @($Msi, $Nsis)) {
    if ($installer.Length -lt $MinimumOfflineInstallerBytes) {
      throw "$($installer.Name) is too small to contain the offline WebView2 runtime ($($installer.Length) bytes)"
    }
  }

  $msiBinaryNames = @(Get-XiassMsiBinaryNames $Msi.FullName)
  if (-not ($msiBinaryNames -contains $OfflineWebView2RuntimeName)) {
    throw "MSI does not contain the expected offline WebView2 Binary table entry: $OfflineWebView2RuntimeName"
  }

  # Listing the installer directly proves that the same offline payload is
  # embedded in Setup, instead of merely trusting the tauri.conf.json setting.
  $sevenZip = (Get-Command 7z.exe -ErrorAction Stop).Source
  $nsisEntries = @(& $sevenZip l -ba $Nsis.FullName)
  if ($LASTEXITCODE -ne 0) {
    throw "NSIS listing failed with code $LASTEXITCODE"
  }
  $offlineRuntimeEntries = @(
    $nsisEntries | Where-Object { $_ -match [Regex]::Escape($OfflineWebView2RuntimeName) }
  )
  if ($offlineRuntimeEntries.Count -eq 0) {
    throw "NSIS Setup does not contain the expected offline WebView2 payload: $OfflineWebView2RuntimeName"
  }
}

function Find-XiassExecutable {
  $entry = Get-XiassUninstallEntry
  $candidateDirectories = [System.Collections.Generic.List[string]]::new()
  if ($entry) {
    if ($entry.InstallLocation) {
      $candidateDirectories.Add([string]$entry.InstallLocation)
    }
    foreach ($command in @($entry.DisplayIcon, $entry.UninstallString, $entry.QuietUninstallString)) {
      if (-not $command) { continue }
      $commandPath = Get-PathFromCommand ([string]$command)
      if ($commandPath) {
        $candidateDirectories.Add((Split-Path -Parent $commandPath))
      }
    }
  }
  $candidateDirectories.Add((Join-Path $env:LOCALAPPDATA $ProductName))
  $candidateDirectories.Add((Join-Path $env:LOCALAPPDATA "Programs\$ProductName"))
  $candidateDirectories.Add((Join-Path $env:ProgramFiles $ProductName))
  if (${env:ProgramFiles(x86)}) {
    $candidateDirectories.Add((Join-Path ${env:ProgramFiles(x86)} $ProductName))
  }

  foreach ($directory in $candidateDirectories | Select-Object -Unique) {
    if (-not $directory -or -not (Test-Path $directory -PathType Container)) { continue }
    $direct = Join-Path $directory $ExecutableName
    if (Test-Path $direct -PathType Leaf) { return (Get-Item $direct) }
    $nested = Get-ChildItem $directory -Recurse -File -Filter $ExecutableName -ErrorAction SilentlyContinue |
      Select-Object -First 1
    if ($nested) { return $nested }
  }
  throw "Installed $ProductName executable was not found"
}

function Assert-XiassInstalledPayload([System.IO.FileInfo]$App, [string]$Label) {
  $installRoot = $App.Directory.FullName
  foreach ($sidecarPattern in @('xiass-wf-bridge*.exe', 'xiass-cliproxy*.exe')) {
    $matches = @(Get-ChildItem $installRoot -Recurse -File -Filter $sidecarPattern -ErrorAction SilentlyContinue)
    if ($matches.Count -ne 1 -or $matches[0].Length -le 0) {
      throw "$Label installation is missing sidecar $sidecarPattern"
    }
  }
  foreach ($license in $RequiredLicenseNames) {
    $matches = @(Get-ChildItem $installRoot -Recurse -File -Filter $license -ErrorAction SilentlyContinue)
    if ($matches.Count -ne 1 -or $matches[0].Length -le 0) {
      throw "$Label installation is missing license resource $license"
    }
  }
}

function Get-XiassInstalledWFBridge([System.IO.FileInfo]$App, [string]$Label) {
  $matches = @(Get-ChildItem $App.Directory.FullName -Recurse -File -Filter 'xiass-wf-bridge*.exe' -ErrorAction SilentlyContinue)
  if ($matches.Count -ne 1 -or $matches[0].Length -le 0) {
    throw "$Label installation is missing the XIASS WF bridge sidecar"
  }
  return $matches[0]
}

function Invoke-XiassInstalledWFBridgeSmoke([System.IO.FileInfo]$App, [string]$Label) {
  $bridge = Get-XiassInstalledWFBridge $App $Label
  & node $BridgeSmokeScript --binary $bridge.FullName --platform windows
  if ($LASTEXITCODE -ne 0) {
    throw "$Label installation WF bridge smoke failed with code $LASTEXITCODE"
  }
}

function Get-XiassDesktopShortcut {
  $desktop = [Environment]::GetFolderPath([Environment+SpecialFolder]::DesktopDirectory)
  if (-not $desktop) { return $null }
  return (Join-Path $desktop "$ProductName.lnk")
}

function Get-XiassStartMenuShortcut {
  $programs = [Environment]::GetFolderPath([Environment+SpecialFolder]::Programs)
  if (-not $programs) { return $null }
  return (Join-Path $programs "$ProductName.lnk")
}

function Assert-XiassShortcuts([string]$Label) {
  $desktopShortcut = Get-XiassDesktopShortcut
  $startMenuShortcut = Get-XiassStartMenuShortcut
  if (-not $desktopShortcut -or -not (Test-Path $desktopShortcut -PathType Leaf)) {
    throw "$Label installation did not create the $ProductName desktop shortcut"
  }
  if (-not $startMenuShortcut -or -not (Test-Path $startMenuShortcut -PathType Leaf)) {
    throw "$Label installation did not create the $ProductName Start menu shortcut"
  }
}

function Assert-XiassShortcutsRemoved([string]$Label) {
  foreach ($shortcut in @(Get-XiassDesktopShortcut, Get-XiassStartMenuShortcut)) {
    if ($shortcut -and (Test-Path $shortcut -PathType Leaf)) {
      throw "$Label uninstall left a $ProductName shortcut behind: $shortcut"
    }
  }
}

function Stop-XiassSmokeProcessTree([System.Diagnostics.Process]$Process) {
  if ($null -eq $Process) { return }
  $Process.Refresh()
  if ($Process.HasExited) { return }

  # The desktop host can start both packaged sidecars. Killing only the host
  # would leave a child bridge running through the next installer lifecycle.
  & taskkill.exe /PID $Process.Id /T /F 2>$null | Out-Null
  $Process.WaitForExit()
}

function Invoke-XiassInstallerOperation {
  param(
    [Parameter(Mandatory = $true)]
    [string]$FilePath,
    [Parameter(Mandatory = $true)]
    [string[]]$ArgumentList,
    [Parameter(Mandatory = $true)]
    [string]$Label,
    [int]$TimeoutSeconds = $InstallerOperationTimeoutSeconds
  )

  $process = Start-Process -FilePath $FilePath -ArgumentList $ArgumentList -PassThru
  if (-not $process.WaitForExit($TimeoutSeconds * 1000)) {
    & taskkill.exe /PID $process.Id /T /F 2>$null | Out-Null
    $process.WaitForExit(10000) | Out-Null
    throw "$Label did not finish within $TimeoutSeconds seconds"
  }
  $process.Refresh()
  return $process.ExitCode
}

function Wait-XiassFrontend([System.IO.FileInfo]$App, [string]$DataDirectory, [string]$Label) {
  New-Item -ItemType Directory -Force -Path $DataDirectory | Out-Null
  $stdoutPath = Join-Path $DataDirectory 'stdout.log'
  $stderrPath = Join-Path $DataDirectory 'stderr.log'
  $previousDataDir = $env:XIASS_TOOLS_DATA_DIR
  $process = $null
  try {
    $env:XIASS_TOOLS_DATA_DIR = $DataDirectory
    $process = Start-Process -FilePath $App.FullName -PassThru `
      -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
    $frontendReady = $false
    for ($attempt = 0; $attempt -lt 45; $attempt++) {
      $process.Refresh()
      $appLog = if (Test-Path (Join-Path $DataDirectory 'logs')) {
        ((Get-ChildItem (Join-Path $DataDirectory 'logs') -Filter 'app.log*' -File -ErrorAction SilentlyContinue | ForEach-Object {
          Get-Content $_.FullName -Raw -ErrorAction SilentlyContinue
        }) -join "`n")
      } else { '' }
      if ($process.HasExited) {
        Get-Content $stderrPath -ErrorAction SilentlyContinue | Write-Host
        $appLog | Write-Host
        throw "$Label installation exited before the frontend became ready with code $($process.ExitCode)"
      }
      if ($appLog.Contains('[Diagnostics] 前端启动超时')) {
        Get-Content $stderrPath -ErrorAction SilentlyContinue | Write-Host
        $appLog | Write-Host
        throw "$Label installation reported a frontend startup timeout"
      }
      if ($appLog.Contains('[Diagnostics] 前端已就绪')) {
        $frontendReady = $true
        break
      }
      Start-Sleep -Seconds 1
    }
    if (-not $frontendReady) {
      Get-Content $stderrPath -ErrorAction SilentlyContinue | Write-Host
      throw "$Label installation did not report frontend ready within 45 seconds"
    }
  }
  finally {
    $env:XIASS_TOOLS_DATA_DIR = $previousDataDir
    if ($process -and -not $process.HasExited) {
      Stop-XiassSmokeProcessTree $process
    }
  }
}

function Wait-XiassUninstalled([string]$Label) {
  for ($attempt = 0; $attempt -lt 30; $attempt++) {
    if (-not (Get-XiassUninstallEntry)) { return }
    Start-Sleep -Seconds 1
  }
  throw "$Label uninstall registration still exists after 30 seconds"
}

$bundle = (Resolve-Path $BundleRoot).Path
$root = [System.IO.Path]::GetFullPath($SmokeRoot)
New-Item -ItemType Directory -Force -Path $root | Out-Null
$msi = @(Get-ChildItem $bundle -Recurse -File -Filter '*.msi')
$nsis = @(Get-ChildItem $bundle -Recurse -File -Filter '*-setup.exe')
if ($msi.Count -ne 1) { throw "Expected one MSI installer, found $($msi.Count)" }
if ($nsis.Count -ne 1) { throw "Expected one NSIS installer, found $($nsis.Count)" }
if (Get-XiassUninstallEntry) { throw "$ProductName is already installed on the clean CI runner" }
Assert-XiassOfflineWebView2Payload $msi[0] $nsis[0]

try {
  $msiInstallExitCode = Invoke-XiassInstallerOperation -FilePath 'msiexec.exe' -ArgumentList @('/i', $msi[0].FullName, '/quiet', '/norestart') -Label 'MSI installation'
  if ($msiInstallExitCode -ne 0) { throw "MSI installation failed with code $msiInstallExitCode" }
  $msiApp = Find-XiassExecutable
  Assert-XiassInstalledPayload $msiApp 'MSI'
  Invoke-XiassInstalledWFBridgeSmoke $msiApp 'MSI'
  Wait-XiassFrontend $msiApp (Join-Path $root 'msi-data') 'MSI'
}
finally {
  if (Get-XiassUninstallEntry) {
    $msiUninstallExitCode = Invoke-XiassInstallerOperation -FilePath 'msiexec.exe' -ArgumentList @('/x', $msi[0].FullName, '/quiet', '/norestart') -Label 'MSI uninstall'
    if ($msiUninstallExitCode -ne 0) { throw "MSI uninstall failed with code $msiUninstallExitCode" }
    Wait-XiassUninstalled 'MSI'
  }
}

try {
  $nsisInstallExitCode = Invoke-XiassInstallerOperation -FilePath $nsis[0].FullName -ArgumentList @('/S') -Label 'NSIS installation'
  if ($nsisInstallExitCode -ne 0) { throw "NSIS installation failed with code $nsisInstallExitCode" }
  $nsisApp = Find-XiassExecutable
  Assert-XiassInstalledPayload $nsisApp 'NSIS'
  Invoke-XiassInstalledWFBridgeSmoke $nsisApp 'NSIS'
  Assert-XiassShortcuts 'NSIS'
  Wait-XiassFrontend $nsisApp (Join-Path $root 'nsis-data') 'NSIS'
}
finally {
  $entry = Get-XiassUninstallEntry
  if ($entry) {
    $uninstallCommand = if ($entry.QuietUninstallString) {
      [string]$entry.QuietUninstallString
    } else {
      [string]$entry.UninstallString
    }
    $uninstaller = Get-PathFromCommand $uninstallCommand
    if (-not $uninstaller -or -not (Test-Path $uninstaller -PathType Leaf)) {
      throw 'NSIS uninstaller was not found from the uninstall registration'
    }
    $nsisUninstallExitCode = Invoke-XiassInstallerOperation -FilePath $uninstaller -ArgumentList @('/S') -Label 'NSIS uninstall'
    if ($nsisUninstallExitCode -ne 0) { throw "NSIS uninstall failed with code $nsisUninstallExitCode" }
    Wait-XiassUninstalled 'NSIS'
    Assert-XiassShortcutsRemoved 'NSIS'
  }
}

Write-Host 'MSI and NSIS installation/startup/uninstall smoke tests passed.'
