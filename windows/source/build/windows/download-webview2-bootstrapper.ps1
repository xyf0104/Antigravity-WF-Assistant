[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [ValidateNotNullOrEmpty()]
  [string]$Destination
)

$ErrorActionPreference = 'Stop'
$bootstrapperUri = 'https://go.microsoft.com/fwlink/p/?LinkId=2124703'
$expectedFilename = 'MicrosoftEdgeWebview2Setup.exe'
$safeDestination = $false

try {
  $Destination = [IO.Path]::GetFullPath($Destination)
  $temporaryRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
  if (-not $Destination.StartsWith($temporaryRoot, [StringComparison]::OrdinalIgnoreCase) -or
      [IO.Path]::GetFileName($Destination) -ne $expectedFilename) {
    throw 'Bootstrapper 只能下载到安装器专用临时目录。'
  }
  $safeDestination = $true

  # Windows PowerShell on older Windows 10 releases can otherwise negotiate an
  # obsolete TLS default. The Microsoft fwlink and Authenticode signature are
  # both verified before the downloaded executable is allowed to run.
  [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
  Invoke-WebRequest -UseBasicParsing -MaximumRedirection 10 -Uri $bootstrapperUri -OutFile $Destination

  $download = Get-Item -LiteralPath $Destination
  if ($download.Length -lt 100KB) {
    throw '下载结果不是完整的 WebView2 Evergreen Bootstrapper。'
  }

  $signature = Get-AuthenticodeSignature -LiteralPath $Destination
  if ($signature.Status -ne [System.Management.Automation.SignatureStatus]::Valid -or
      $null -eq $signature.SignerCertificate -or
      $signature.SignerCertificate.Subject -notmatch '(^|,\s*)CN=Microsoft Corporation(,|$)') {
    throw 'WebView2 Evergreen Bootstrapper 的 Microsoft 数字签名无效。'
  }
} catch {
  if ($safeDestination -and (Test-Path -LiteralPath $Destination -PathType Leaf)) {
    Remove-Item -LiteralPath $Destination -Force -ErrorAction SilentlyContinue
  }
  Write-Error '无法从 Microsoft 安全下载 WebView2 Evergreen Bootstrapper。请检查网络、代理或企业安全策略。'
  exit 1
}

exit 0
