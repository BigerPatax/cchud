# This file is ASCII only so Windows PowerShell 5.1 reads it correctly without a BOM.
# Download cchud from the latest GitHub Release, verify SHA256, install, then run `cchud setup` (Windows).
#
# Usage (PowerShell):
#   irm https://raw.githubusercontent.com/BigerPatax/cchud/main/scripts/install.ps1 | iex
#
# Optional environment variables:
#   $env:CCHUD_VERSION = 'v0.1.0'     install a specific version (default: latest)
#   $env:CCHUD_INSTALL_DIR = 'path'   install directory (default: %USERPROFILE%\.local\bin)
#   $env:CCHUD_SKIP_SETUP = '1'       install the binary only, do not touch Claude Code settings.json

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repo = 'BigerPatax/cchud'
$ver = if ($env:CCHUD_VERSION) { $env:CCHUD_VERSION } else { 'latest' }
$base = if ($ver -eq 'latest') { "https://github.com/$repo/releases/latest/download" } else { "https://github.com/$repo/releases/download/$ver" }
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$asset = "cchud-windows-$arch.exe"
$dest = if ($env:CCHUD_INSTALL_DIR) { $env:CCHUD_INSTALL_DIR } else { Join-Path $env:USERPROFILE '.local\bin' }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ('cchud-' + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    Write-Host "Downloading $base/$asset"
    $file = Join-Path $tmp $asset
    Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile $file
    Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS" -OutFile (Join-Path $tmp 'SHA256SUMS')

    $expected = $null
    foreach ($line in Get-Content (Join-Path $tmp 'SHA256SUMS')) {
        $parts = $line -split '\s+'
        if ($parts.Count -ge 2 -and $parts[1].TrimStart('*') -eq $asset) { $expected = $parts[0].ToLower() }
    }
    $actual = (Get-FileHash -Algorithm SHA256 -Path $file).Hash.ToLower()
    if (-not $expected -or $expected -ne $actual) {
        throw "SHA256 mismatch, nothing installed. expected=$expected actual=$actual"
    }
    Write-Host 'SHA256 verified'

    New-Item -ItemType Directory -Force -Path $dest | Out-Null
    $target = Join-Path $dest 'cchud.exe'
    # A running exe cannot be overwritten but can be renamed: stop the daemon, rename the old file, move the new one in.
    if (Test-Path $target) {
        try { & $target stop | Out-Null } catch {}
        Start-Sleep -Milliseconds 300
        Move-Item -Force -Path $target -Destination ("$target." + [guid]::NewGuid() + '.old')
    }
    Move-Item -Force -Path $file -Destination $target
    # Old copies that are still running stay locked; they are removed on a later install.
    Get-ChildItem -Path $dest -Filter 'cchud.exe.*.old' -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue
    Write-Host "Installed $target"
    & $target version

    if ($env:CCHUD_SKIP_SETUP -eq '1') {
        Write-Host 'Skipped setup because CCHUD_SKIP_SETUP=1. See README for manual configuration.'
    } else {
        & $target setup
        if ($LASTEXITCODE -ne 0) { throw "cchud setup failed with exit code $LASTEXITCODE" }
    }
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
