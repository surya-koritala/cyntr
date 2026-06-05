# Cyntr installer for native Windows (PowerShell).
# Builds the single binary from source. Requires Go 1.26+.
#
#   iwr -useb https://raw.githubusercontent.com/surya-koritala/cyntr/main/scripts/install.ps1 | iex
#
# or, from a checkout:  .\scripts\install.ps1

$ErrorActionPreference = "Stop"

Write-Host "Installing Cyntr (native Windows)..." -ForegroundColor Cyan

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "Go not found. Install Go 1.26+ from https://go.dev/dl/ and re-run." -ForegroundColor Yellow
    exit 1
}

if (-not (Test-Path "./cmd/cyntr")) {
    Write-Host "Run this from a cyntr checkout (cmd/cyntr not found)." -ForegroundColor Yellow
    Write-Host "  git clone https://github.com/surya-koritala/cyntr.git; cd cyntr; .\scripts\install.ps1"
    exit 1
}

$out = "cyntr.exe"
Write-Host "Building $out ..."
go build -o $out ./cmd/cyntr
if ($LASTEXITCODE -ne 0) { Write-Host "Build failed." -ForegroundColor Red; exit 1 }

Write-Host "Built $out" -ForegroundColor Green
Write-Host ""
Write-Host "Next:"
Write-Host "  .\$out init      # configure provider, channels, first agent"
Write-Host "  .\$out start     # start the gateway (dashboard at http://localhost:7700)"
Write-Host ""
Write-Host "Native Windows runs the CLI, gateway, and tools without WSL." -ForegroundColor Cyan
