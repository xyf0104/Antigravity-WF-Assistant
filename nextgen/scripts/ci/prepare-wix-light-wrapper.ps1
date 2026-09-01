Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# Tauri 2.9 uses WiX 3.14. Its validation pass depends on legacy Windows
# scripting components absent from hosted runners. Download the exact
# Tauri-pinned toolset, verify it, then wrap only light.exe. Packaging remains
# covered by extraction, silent installation, app startup, and WF bridge smoke
# tests.
$wixArchiveUrl = 'https://github.com/wixtoolset/wix3/releases/download/wix3141rtm/wix314-binaries.zip'
$expectedSha256 = '6ac824e1642d6f7277d0ed7ea09411a508f6116ba6fae0aa5f2c7daa2ff43d31'
$wixDirectory = Join-Path $env:LOCALAPPDATA 'tauri\WixTools314'
$archivePath = Join-Path $env:RUNNER_TEMP 'xiass-wix314-binaries.zip'
$wrapperSource = Join-Path $PSScriptRoot 'wix-light-wrapper.rs'
$requiredFiles = @(
  'candle.exe',
  'candle.exe.config',
  'darice.cub',
  'light.exe',
  'light.exe.config',
  'wconsole.dll',
  'winterop.dll',
  'wix.dll',
  'WixUIExtension.dll',
  'WixUtilExtension.dll'
)

if (-not (Test-Path $wrapperSource -PathType Leaf)) {
  throw "WiX linker wrapper source is missing: $wrapperSource"
}

Invoke-WebRequest -Uri $wixArchiveUrl -OutFile $archivePath
$actualSha256 = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actualSha256 -ne $expectedSha256) {
  throw "WiX archive SHA-256 mismatch. Expected $expectedSha256, got $actualSha256"
}

Remove-Item -LiteralPath $wixDirectory -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Path $wixDirectory -Force | Out-Null
Expand-Archive -LiteralPath $archivePath -DestinationPath $wixDirectory -Force

foreach ($requiredFile in $requiredFiles) {
  $requiredPath = Join-Path $wixDirectory $requiredFile
  if (-not (Test-Path $requiredPath -PathType Leaf)) {
    throw "WiX archive is missing required file: $requiredFile"
  }
}

$originalLight = Join-Path $wixDirectory 'light.exe'
$realLight = Join-Path $wixDirectory 'light.real.exe'
$originalLightConfig = Join-Path $wixDirectory 'light.exe.config'
$realLightConfig = Join-Path $wixDirectory 'light.real.exe.config'
Move-Item -LiteralPath $originalLight -Destination $realLight -Force
# .NET resolves an executable's runtime configuration from its own file name.
# Keep the original file for Tauri's toolset validation and give the renamed
# official linker the matching configuration it would normally load.
Copy-Item -LiteralPath $originalLightConfig -Destination $realLightConfig -Force
& rustc --edition 2021 $wrapperSource -O -o $originalLight
if ($LASTEXITCODE -ne 0) {
  throw "Failed to compile the WiX linker wrapper (exit=$LASTEXITCODE)"
}

$diagnosticPath = Join-Path $env:RUNNER_TEMP 'xiass-wix-light.log'
Remove-Item -LiteralPath $diagnosticPath -Force -ErrorAction SilentlyContinue
"XIASS_WIX_LIGHT_LOG=$diagnosticPath" | Out-File -FilePath $env:GITHUB_ENV -Encoding utf8 -Append

& $originalLight '-?'
if ($LASTEXITCODE -ne 0) {
  throw "Prepared WiX linker wrapper failed its startup check (exit=$LASTEXITCODE)"
}

if (
  -not (Test-Path $originalLight -PathType Leaf) -or
  -not (Test-Path $realLight -PathType Leaf) -or
  -not (Test-Path $realLightConfig -PathType Leaf)
) {
  throw 'WiX linker wrapper preparation did not produce the required linker runtime files'
}

Write-Host "Prepared verified WiX 3.14 linker wrapper. Diagnostics: $diagnosticPath"
