$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$goBin = Join-Path $projectRoot '.tools\go\bin'
$python = Join-Path $projectRoot '.venv\Scripts\python.exe'
$goExe = Join-Path $goBin 'go.exe'

if (-not (Test-Path -LiteralPath $goExe)) {
    throw "Local Go toolchain not found: $goExe"
}
if (-not (Test-Path -LiteralPath $python)) {
    throw "Local Python environment not found: $python"
}

$env:PATH = "$goBin;$(Split-Path -Parent $python);$env:PATH"
$env:GOCACHE = Join-Path $projectRoot '.tools\gocache'
$env:GOPATH = Join-Path $projectRoot '.tools\gopath'

& $python (Join-Path $projectRoot 'build_release.py')
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
