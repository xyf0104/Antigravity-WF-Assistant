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
$WebView2RuntimeAppGuid = '{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
$MinimumOfflineInstallerBytes = 80MB
$InstallerOperationTimeoutSeconds = 300
$WebView2PrerequisiteTimeoutSeconds = 300
$InstallerDiagnosticLogTailLines = 160
$WebView2RuntimeRegistryPaths = @(
  "HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\$WebView2RuntimeAppGuid",
  "HKLM:\SOFTWARE\Microsoft\EdgeUpdate\Clients\$WebView2RuntimeAppGuid",
  "HKCU:\SOFTWARE\Microsoft\EdgeUpdate\Clients\$WebView2RuntimeAppGuid"
)
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
    # The Windows Installer COM wrappers exposed on hosted Windows runners do
    # not consistently surface a Close() method. Releasing the COM references
    # is sufficient and works across both Windows PowerShell and PowerShell 7.
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

function Get-XiassWebView2RuntimeVersions {
  $versions = [System.Collections.Generic.List[string]]::new()
  foreach ($registryPath in $WebView2RuntimeRegistryPaths) {
    if (-not (Test-Path -LiteralPath $registryPath)) { continue }
    try {
      $version = Get-ItemPropertyValue -LiteralPath $registryPath -Name 'pv' -ErrorAction Stop
    }
    catch {
      continue
    }
    if (-not [string]::IsNullOrWhiteSpace([string]$version)) {
      $versions.Add("$registryPath=$version")
    }
  }
  return $versions.ToArray()
}

function Test-XiassWebView2RuntimeInstalled {
  return (@(Get-XiassWebView2RuntimeVersions).Count -gt 0)
}

function Wait-XiassWebView2Runtime([string]$Label) {
  for ($attempt = 0; $attempt -lt 30; $attempt++) {
    if (Test-XiassWebView2RuntimeInstalled) { return }
    Start-Sleep -Seconds 1
  }
  throw "$Label completed without creating any WebView2 runtime registry entry expected by the Tauri MSI template"
}

function Export-XiassMsiBinary([string]$MsiPath, [string]$BinaryName, [string]$DestinationPath) {
  $installer = $null
  $database = $null
  $view = $null
  $record = $null
  $output = $null
  try {
    $escapedBinaryName = $BinaryName.Replace("'", "''")
    $installer = New-Object -ComObject WindowsInstaller.Installer
    $database = $installer.OpenDatabase($MsiPath, 0)
    $view = $database.OpenView("SELECT `Name`, `Data` FROM `Binary` WHERE `Name` = '$escapedBinaryName'")
    $view.Execute()
    $record = $view.Fetch()
    if ($null -eq $record) {
      throw "MSI Binary table does not contain $BinaryName"
    }

    $destinationDirectory = Split-Path -Parent $DestinationPath
    if ($destinationDirectory) {
      New-Item -ItemType Directory -Force -Path $destinationDirectory | Out-Null
    }
    $output = [System.IO.File]::Open(
      $DestinationPath,
      [System.IO.FileMode]::Create,
      [System.IO.FileAccess]::Write,
      [System.IO.FileShare]::None
    )
    $byteEncoding = [System.Text.Encoding]::GetEncoding('iso-8859-1')
    $chunkLength = 32768
    while ($true) {
      $chunk = [string]$record.ReadStream(2, $chunkLength, 1)
      if ($chunk.Length -eq 0) { break }
      [byte[]]$bytes = $byteEncoding.GetBytes($chunk)
      $output.Write($bytes, 0, $bytes.Length)
      if ($bytes.Length -lt $chunkLength) { break }
    }
  }
  finally {
    if ($output) { $output.Dispose() }
    foreach ($comObject in @($record, $view, $database, $installer)) {
      if ($null -ne $comObject -and [System.Runtime.InteropServices.Marshal]::IsComObject($comObject)) {
        [void][System.Runtime.InteropServices.Marshal]::FinalReleaseComObject($comObject)
      }
    }
  }

  if (-not (Test-Path -LiteralPath $DestinationPath -PathType Leaf)) {
    throw "MSI Binary table export did not create $DestinationPath"
  }
  $exported = Get-Item -LiteralPath $DestinationPath
  if ($exported.Length -lt $MinimumOfflineInstallerBytes) {
    throw "MSI Binary table export is too small for the offline WebView2 runtime ($($exported.Length) bytes)"
  }
  $headerStream = $null
  try {
    $headerStream = [System.IO.File]::OpenRead($DestinationPath)
    [byte[]]$header = New-Object byte[] 2
    if ($headerStream.Read($header, 0, $header.Length) -ne $header.Length) {
      throw 'MSI Binary table export is too short to be a PE executable payload'
    }
  }
  finally {
    if ($headerStream) { $headerStream.Dispose() }
  }
  if ($header[0] -ne 0x4D -or $header[1] -ne 0x5A) {
    throw 'MSI Binary table export is not a PE executable payload'
  }
  return $exported
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
  foreach ($shortcut in @(
    (Get-XiassDesktopShortcut),
    (Get-XiassStartMenuShortcut)
  )) {
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

function Write-XiassInstallerDiagnostics([string]$Label, [string]$DiagnosticLogPath) {
  Write-Host "[Diagnostics] $Label timed out or failed."
  $runtimeVersions = @(Get-XiassWebView2RuntimeVersions)
  if ($runtimeVersions.Count -eq 0) {
    Write-Host '[Diagnostics] No Tauri-compatible WebView2 runtime registry entry was found.'
  } else {
    Write-Host '[Diagnostics] WebView2 runtime registry entries:'
    $runtimeVersions | ForEach-Object { Write-Host "[Diagnostics] $_" }
  }

  try {
    $installerProcesses = @(
      Get-CimInstance Win32_Process -ErrorAction Stop |
        Where-Object {
          $_.Name -in @(
            'msiexec.exe',
            'MicrosoftEdgeWebView2RuntimeInstaller.exe',
            'MicrosoftEdgeWebview2Setup.exe',
            'MicrosoftEdgeUpdate.exe'
          )
        } |
        Select-Object ProcessId, ParentProcessId, Name, CommandLine
    )
    if ($installerProcesses.Count -eq 0) {
      Write-Host '[Diagnostics] No active MSI, WebView2, or Edge Update process was found.'
    } else {
      Write-Host '[Diagnostics] Active MSI/WebView2 process tree:'
      $installerProcesses | Format-Table -AutoSize | Out-String | Write-Host
    }
  }
  catch {
    Write-Host "[Diagnostics] Could not enumerate installer processes: $($_.Exception.Message)"
  }

  if ($DiagnosticLogPath -and (Test-Path -LiteralPath $DiagnosticLogPath -PathType Leaf)) {
    Write-Host "[Diagnostics] Tail of ${DiagnosticLogPath}:"
    Get-Content -LiteralPath $DiagnosticLogPath -Tail $InstallerDiagnosticLogTailLines -ErrorAction SilentlyContinue |
      Write-Host
  }
}

function Invoke-XiassInstallerOperation {
  param(
    [Parameter(Mandatory = $true)]
    [string]$FilePath,
    [Parameter(Mandatory = $true)]
    [string[]]$ArgumentList,
    [Parameter(Mandatory = $true)]
    [string]$Label,
    [int]$TimeoutSeconds = $InstallerOperationTimeoutSeconds,
    [string]$DiagnosticLogPath = ''
  )

  $process = Start-Process -FilePath $FilePath -ArgumentList $ArgumentList -PassThru
  if (-not $process.WaitForExit($TimeoutSeconds * 1000)) {
    Write-XiassInstallerDiagnostics -Label $Label -DiagnosticLogPath $DiagnosticLogPath
    & taskkill.exe /PID $process.Id /T /F 2>$null | Out-Null
    $process.WaitForExit(10000) | Out-Null
    throw "$Label did not finish within $TimeoutSeconds seconds"
  }
  $process.Refresh()
  return $process.ExitCode
}

function Invoke-XiassOfflineWebView2Prerequisite([System.IO.FileInfo]$Msi, [string]$SmokeRoot) {
  $runtimeVersions = @(Get-XiassWebView2RuntimeVersions)
  if ($runtimeVersions.Count -gt 0) {
    Write-Host "WebView2 runtime is already present: $($runtimeVersions -join '; ')"
    return
  }

  # Tauri's MSI runs this same payload from a deferred custom action. Install
  # it first so the subsequent real MSI lifecycle does not nest an installer
  # under Windows Installer on hosted runners.
  $payloadPath = Join-Path $SmokeRoot $OfflineWebView2RuntimeName
  $payload = Export-XiassMsiBinary -MsiPath $Msi.FullName -BinaryName $OfflineWebView2RuntimeName -DestinationPath $payloadPath
  $prerequisiteArguments = @{
    FilePath = $payload.FullName
    ArgumentList = @('/silent', '/install')
    Label = 'Embedded offline WebView2 runtime prerequisite'
    TimeoutSeconds = $WebView2PrerequisiteTimeoutSeconds
  }
  $exitCode = Invoke-XiassInstallerOperation @prerequisiteArguments
  if ($exitCode -notin @(0, 3010)) {
    Write-XiassInstallerDiagnostics -Label 'Embedded offline WebView2 runtime prerequisite' -DiagnosticLogPath ''
    throw "Embedded offline WebView2 runtime prerequisite failed with code $exitCode"
  }
  Wait-XiassWebView2Runtime 'Embedded offline WebView2 runtime prerequisite'
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
Invoke-XiassOfflineWebView2Prerequisite $msi[0] $root
$msiInstallLog = Join-Path $root 'msi-install.log'

try {
  $msiInstallExitCode = Invoke-XiassInstallerOperation -FilePath 'msiexec.exe' -ArgumentList @('/i', $msi[0].FullName, '/quiet', '/norestart', '/l*v', $msiInstallLog) -Label 'MSI installation' -DiagnosticLogPath $msiInstallLog
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
