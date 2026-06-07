# Cyntr installer for native Windows (PowerShell).
# Downloads the prebuilt single binary (no Go required).
#
#   iwr -useb https://raw.githubusercontent.com/surya-koritala/cyntr/main/scripts/install.ps1 | iex
#
# Options (environment variables):
#   $env:INSTALL_DIR   where to install (default: %LOCALAPPDATA%\Programs\cyntr)
#   $env:CYNTR_VERSION pin a version (default: latest release)

$ErrorActionPreference = "Stop"
$Repo = "surya-koritala/cyntr"

Write-Host "Installing Cyntr (native Windows)..." -ForegroundColor Cyan

# --- detect arch ---
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }

# --- resolve version ---
$version = $env:CYNTR_VERSION
if (-not $version) {
    try {
        $rel = Invoke-RestMethod -UseBasicParsing "https://api.github.com/repos/$Repo/releases/latest"
        $version = $rel.tag_name
    } catch {
        Write-Host "Could not determine the latest version. Set `$env:CYNTR_VERSION and re-run." -ForegroundColor Red
        exit 1
    }
}

$asset = "cyntr_windows_$arch.exe"
$url   = "https://github.com/$Repo/releases/download/$version/$asset"

$installDir = $env:INSTALL_DIR
if (-not $installDir) { $installDir = Join-Path $env:LOCALAPPDATA "Programs\cyntr" }
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
$dest = Join-Path $installDir "cyntr.exe"

Write-Host "Downloading Cyntr $version ($arch)..."
try {
    Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $dest
} catch {
    Write-Host "No prebuilt binary for windows/$arch at $version." -ForegroundColor Red
    Write-Host "Build from source instead (requires Go 1.26+):" -ForegroundColor Yellow
    Write-Host "  git clone https://github.com/$Repo.git; cd cyntr; go build -o cyntr.exe ./cmd/cyntr"
    exit 1
}

# --- verify checksum (best effort) ---
try {
    $sumLine = (Invoke-WebRequest -UseBasicParsing -Uri "$url.sha256").Content
    $expected = ($sumLine -split '\s+')[0]
    $actual = (Get-FileHash -Algorithm SHA256 $dest).Hash.ToLower()
    if ($expected -and $actual -ne $expected) {
        Write-Host "Checksum verification failed (expected $expected, got $actual)." -ForegroundColor Red
        exit 1
    }
    if ($expected) { Write-Host "Checksum verified." -ForegroundColor Green }
} catch { } # sidecar absent — skip

Write-Host "Installed to $dest" -ForegroundColor Green

# --- ensure install dir is on PATH for this user ---
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$installDir", "User")
    Write-Host "Added $installDir to your user PATH (restart the shell to use 'cyntr' directly)." -ForegroundColor Yellow
}

Write-Host ""
& $dest version
Write-Host ""
Write-Host "Next:"
Write-Host "  cyntr init      # configure provider, channels, first agent"
Write-Host "  cyntr start     # start the gateway (dashboard at http://localhost:7700)"
Write-Host ""
Write-Host "Native Windows runs the CLI, gateway, and tools without WSL." -ForegroundColor Cyan
