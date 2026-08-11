# Builds perfadvisor.exe. Run from the project root in PowerShell: .\build.ps1
# First run needs internet access (go mod tidy downloads dependencies).
$ErrorActionPreference = "Stop"
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "Go is not installed. Install it first: winget install GoLang.Go (or https://go.dev/dl/), then re-run."
    exit 1
}
Set-Location $PSScriptRoot

# Embed the icon and version info (winres\) via go-winres; auto-install once.
$goWinres = "go-winres"
if (-not (Get-Command go-winres -ErrorAction SilentlyContinue)) {
    $gopathBin = Join-Path (go env GOPATH) "bin\go-winres.exe"
    if (-not (Test-Path $gopathBin)) {
        Write-Host "Installing go-winres (one-time)..."
        go install github.com/tc-hib/go-winres@latest
    }
    $goWinres = $gopathBin
}
& $goWinres make
if ($LASTEXITCODE -ne 0) { Write-Host "go-winres failed; building without icon."; }

go mod tidy
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$env:GOOS = "windows"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w" -o perfadvisor.exe .
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$size = [math]::Round((Get-Item .\perfadvisor.exe).Length / 1MB, 1)
Write-Host "Built perfadvisor.exe ($size MB)"
