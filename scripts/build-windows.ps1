param(
  [switch]$Nsis
)

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
$bin = Join-Path $root 'build\bin'

function Clear-BuildBin {
  if (-not (Test-Path $bin)) {
    New-Item -ItemType Directory -Force -Path $bin | Out-Null
    return
  }
  Get-ChildItem $bin -File -Force | Remove-Item -Force
  Write-Host "Cleared $bin"
}

Clear-BuildBin

$nsisDir = Join-Path ${env:ProgramFiles(x86)} 'NSIS'
if (Test-Path $nsisDir) {
  $env:Path = "$nsisDir;$env:Path"
}

Push-Location $root
try {
  $args = @(
    'build',
    '-platform', 'windows/amd64',
    '-trimpath',
    '-ldflags', '-s -w'
  )
  if ($Nsis) {
    $args += '-nsis'
  }
  & wails @args
  if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
  }
} finally {
  Pop-Location
}

Get-ChildItem $bin -File | Format-Table Name, @{ N = 'MB'; E = { [math]::Round($_.Length / 1MB, 2) } }, LastWriteTime -AutoSize
