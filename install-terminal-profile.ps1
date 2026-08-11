# Registers a "perfadvisor" profile in Windows Terminal with the custom icon,
# using Terminal's JSON fragment mechanism. Per-user, no admin required.
# Run once from the project folder: .\install-terminal-profile.ps1
# Then restart Windows Terminal; "perfadvisor" appears in the profile dropdown.
$ErrorActionPreference = "Stop"

$exe  = Join-Path $PSScriptRoot "perfadvisor.exe"
$ico  = Join-Path $PSScriptRoot "assets\perfadvisor.ico"
if (-not (Test-Path $exe)) { Write-Host "perfadvisor.exe not found next to this script. Build first (.\build.ps1)."; exit 1 }
if (-not (Test-Path $ico)) { Write-Host "assets\perfadvisor.ico not found."; exit 1 }

$fragDir  = Join-Path $env:LOCALAPPDATA "Microsoft\Windows Terminal\Fragments\perfadvisor"
New-Item -ItemType Directory -Force -Path $fragDir | Out-Null

$fragment = [ordered]@{
    profiles = @(
        [ordered]@{
            name              = "perfadvisor"
            commandline       = $exe
            icon              = $ico
            startingDirectory = $PSScriptRoot
        }
    )
} | ConvertTo-Json -Depth 5

Set-Content -Path (Join-Path $fragDir "perfadvisor.json") -Value $fragment -Encoding UTF8
Write-Host "Terminal profile installed: $fragDir\perfadvisor.json"
Write-Host "Restart Windows Terminal, then pick 'perfadvisor' from the tab dropdown (or set a keybinding)."
