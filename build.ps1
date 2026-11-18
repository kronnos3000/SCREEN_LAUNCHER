# Builds both CR2 and SD variants of ScreenLauncher.
# Usage: .\build.ps1 [-Version 1.0.0] [-Variant CR2|SD|all]
# Output: ScreenLauncher-CR2.exe, ScreenLauncher-SD.exe
param(
    [string]$Version = "dev",
    [string]$Variant = "all",
    [string]$Out = $PSScriptRoot
)
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$env:Path = [Environment]::GetEnvironmentVariable('Path', 'Machine') + ';' + $env:Path
Set-Location $root

function Build-Variant {
    param([string]$V, [string]$Ver)

    $outFile = Join-Path $Out "ScreenLauncher-$V.exe"

    Write-Host "Building $V variant (v$Ver)..." -ForegroundColor Cyan
    # Use explicit quoting for ldflags to ensure -X flags are passed correctly
    & go build "-ldflags=-H windowsgui -s -w -X main.Variant=$V -X main.Version=$Ver" -o $outFile .
    if ($LASTEXITCODE) { throw "Build failed for variant $V" }

    $size = (Get-Item $outFile).Length / 1MB
    Write-Host ("  -> $outFile ({0:N2} MB)" -f $size) -ForegroundColor Green
    return $outFile
}

# Vet first
Write-Host "Running go vet..." -ForegroundColor Gray
go vet . 2>$null

$built = @()
if ($Variant -eq "all" -or $Variant -eq "CR2") {
    $built += Build-Variant "CR2" $Version
}
if ($Variant -eq "all" -or $Variant -eq "SD") {
    $built += Build-Variant "SD" $Version
}

Write-Host "`nBuilt $($built.Count) variant(s):" -ForegroundColor Yellow
$built | ForEach-Object { Write-Host "  $_" }

# If this is a release build, create a zip for GitHub
if ($Version -ne "dev") {
    $zipFile = Join-Path $Out "ScreenLauncher-v$Version.zip"
    Compress-Archive -Path $built -DestinationPath $zipFile -Force
    Write-Host "`nRelease archive: $zipFile" -ForegroundColor Magenta
}
