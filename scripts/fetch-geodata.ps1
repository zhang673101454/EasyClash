$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$root = Split-Path -Parent $PSScriptRoot
$resources = Join-Path $root 'resources'
$base = 'https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest'
$files = @(
  @{ Name = 'geoip.metadb'; Url = "$base/geoip.metadb" },
  @{ Name = 'geosite.dat'; Url = "$base/geosite.dat" }
)

New-Item -ItemType Directory -Force -Path $resources | Out-Null
foreach ($item in $files) {
  $dest = Join-Path $resources $item.Name
  $tmp = Join-Path $env:TEMP ("easyclash-" + $item.Name)
  Write-Host "Downloading $($item.Url)"
  curl.exe -L --ssl-no-revoke -o $tmp $item.Url --connect-timeout 30 --max-time 600 --retry 3
  if (-not (Test-Path $tmp) -or (Get-Item $tmp).Length -lt 1024) {
    throw "download failed: $($item.Name)"
  }
  Move-Item -Force $tmp $dest
  Get-Item $dest | Select-Object FullName, Length
}
Write-Host 'OK: geodata ready in resources/'
