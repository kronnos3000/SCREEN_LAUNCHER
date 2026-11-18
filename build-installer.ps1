# Builds the app and "CCorps Meeting Launcher Setup.exe".
# Usage: .\build-installer.ps1 [-Out E:\]
# The JobTread key is read from Construction-Corp\.env and embedded in the setup exe.
# Treat the setup exe like the key itself.
param([string]$Out = $PSScriptRoot)
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$env:Path = [Environment]::GetEnvironmentVariable('Path', 'Machine') + ';' + $env:Path
Set-Location $root

go vet . ./installer 2>$null
go build -ldflags '-H windowsgui -s -w' -o ScreenLauncher.exe .
if ($LASTEXITCODE) { throw 'app build failed' }

$pl = Join-Path $root 'installer\payload'
New-Item -ItemType Directory -Force $pl | Out-Null
Copy-Item ScreenLauncher.exe "$pl\app.exe" -Force

# Icon: the shield (top 72% of the logo), centred on a transparent 256x256 square, PNG-in-ICO.
Add-Type -AssemblyName System.Drawing
$src = [System.Drawing.Image]::FromFile("$root\assets\logo.png")
$sh = [int]($src.Height * 0.72); $side = [Math]::Max($src.Width, $sh)
$bmp = New-Object System.Drawing.Bitmap 256, 256
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.InterpolationMode = 'HighQualityBicubic'; $g.SmoothingMode = 'HighQuality'; $g.PixelOffsetMode = 'HighQuality'
$scale = 256 / $side; $dw = $src.Width * $scale; $dh = $sh * $scale
$g.DrawImage($src, [System.Drawing.RectangleF]::new((256 - $dw) / 2, (256 - $dh) / 2, $dw, $dh),
    [System.Drawing.RectangleF]::new(0, 0, $src.Width, $sh), [System.Drawing.GraphicsUnit]::Pixel)
$g.Dispose(); $src.Dispose()
$ms = New-Object System.IO.MemoryStream; $bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png); $bmp.Dispose()
$png = $ms.ToArray()
$ico = New-Object System.IO.MemoryStream; $w = New-Object System.IO.BinaryWriter $ico
$w.Write([uint16]0); $w.Write([uint16]1); $w.Write([uint16]1)                    # ICONDIR
$w.Write([byte]0); $w.Write([byte]0); $w.Write([byte]0); $w.Write([byte]0)       # 256x256, no palette
$w.Write([uint16]1); $w.Write([uint16]32); $w.Write([uint32]$png.Length); $w.Write([uint32]22)
$w.Write($png); $w.Flush()
[IO.File]::WriteAllBytes("$pl\app.ico", $ico.ToArray())

# Settings: current preferences, without this PC's .env path.
$json = Get-Content (Join-Path $root 'ScreenLauncher-presets.json') -Raw -Encoding utf8
$json = $json -replace '"keyFile":\s*"[^"]*"', '"keyFile": ""'
[IO.File]::WriteAllText("$pl\settings.json", $json, (New-Object System.Text.UTF8Encoding $false))

# Key: from Construction-Corp\.env (never printed).
$envFile = Join-Path $env:USERPROFILE 'OneDrive\Business\Documents\CODE\Construction-Corp\.env'
$line = Get-Content $envFile | Where-Object { $_ -match '^JOBTREAD_GRANT_KEY=' } | Select-Object -First 1
$key = ($line -replace '^JOBTREAD_GRANT_KEY=', '').Trim().Trim('"').Trim("'")
if (-not $key) { throw 'JOBTREAD_GRANT_KEY not found in Construction-Corp\.env' }
[IO.File]::WriteAllText("$pl\key.txt", $key)

try {
    $setup = Join-Path $Out 'CCorps Meeting Launcher Setup.exe'
    go build -ldflags '-H windowsgui -s -w' -o $setup ./installer
    if ($LASTEXITCODE) { throw 'installer build failed' }
    "Built: $setup ({0:N1} MB)" -f ((Get-Item $setup).Length / 1MB)
} finally {
    Remove-Item "$pl\key.txt" -Force -ErrorAction SilentlyContinue   # never leave the key lying around
}
