$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$root = Split-Path -Parent $PSScriptRoot
$resources = Join-Path $root 'resources'
$zip = Join-Path $resources 'wintun.zip'
$extract = Join-Path $resources 'wintun-extract'
$dll = Join-Path $resources 'wintun.dll'
$license = Join-Path $resources 'wintun-LICENSE.txt'
$url = 'https://www.wintun.net/builds/wintun-0.14.1.zip'
$expected = '07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51'

New-Item -ItemType Directory -Force -Path $resources | Out-Null
Write-Host "Downloading $url"
Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing

$hash = (Get-FileHash -Path $zip -Algorithm SHA256).Hash.ToLowerInvariant()
if ($hash -ne $expected) {
  throw "wintun zip hash mismatch: got $hash expected $expected"
}

if (Test-Path $extract) {
  Remove-Item -Recurse -Force $extract
}
Expand-Archive -Path $zip -DestinationPath $extract -Force
Copy-Item (Join-Path $extract 'wintun\bin\amd64\wintun.dll') $dll -Force
Copy-Item (Join-Path $extract 'wintun\LICENSE.txt') $license -Force
Remove-Item -Recurse -Force $extract
Remove-Item -Force $zip

Get-Item $dll | Select-Object FullName, Length
Write-Host "OK: wintun.dll ready"
