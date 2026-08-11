# perfadvisor, running it on another device

## Just run it (no install)

perfadvisor is a single portable exe. To use it on another Windows machine:

1. Copy `perfadvisor.exe` to the machine (USB stick, share, anything). Put it in
   a folder where you can write, for example `C:\Tools\perfadvisor\`; reports are
   written to a `reports` subfolder next to the exe.
2. Double-click it, or run it from a terminal. Nothing is installed, nothing is
   written outside its own folder (fallback: `%LOCALAPPDATA%\perfadvisor\reports`
   when the exe folder is read-only), no admin rights required.

Requirements on the target machine:

- Windows 10 or 11, 64-bit (x64). That is all; no runtimes, no dependencies.
- Best experience in Windows Terminal (graphs use braille and block characters,
  which need a font like Cascadia Mono; the classic console works but looks rougher).

First-run notes:

- SmartScreen: the exe is unsigned, so Windows may show "Windows protected your
  PC". Click "More info", then "Run anyway". Alternative: right-click the exe,
  Properties, check "Unblock", OK.
- Antivirus may scan the exe on first launch; that is a one-time delay.
- Optional: run it elevated once ("Run as administrator") to unlock the deeper
  boot analysis (the Diagnostics-Performance event log needs elevation to read).

Usage after that: run without arguments for the live dashboard, `perfadvisor
analyze -open` for the HTML advice report, `perfadvisor export` for the markdown
bundle to paste into Claude, `perfadvisor help` for everything.

## Build it from source instead

1. Install Go: `winget install GoLang.Go` (or https://go.dev/dl/).
2. Copy this project folder to the machine.
3. In PowerShell, from the project root: `.\build.ps1`
   The first build downloads the Go dependencies and the go-winres tool (icon
   embedding), so it needs internet access. Result: `perfadvisor.exe`.

## Icon

The icon lives in `assets\perfadvisor.ico` (for shortcuts) and `winres\` (build
input). It is embedded into the exe at build time by go-winres, so Explorer,
the Start menu, the taskbar, and Task Manager show it automatically.

Windows Terminal tabs are a special case: the tab icon belongs to the Terminal
profile, not to the running exe. To get the perfadvisor icon in the tab, run
`.\install-terminal-profile.ps1` once (per user, no admin). It registers a
"perfadvisor" profile via Terminal's fragment mechanism with the icon, the exe,
and the project folder as starting directory. Restart Windows Terminal and
start perfadvisor from the tab dropdown. To remove it again, delete
`%LOCALAPPDATA%\Microsoft\Windows Terminal\Fragments\perfadvisor\`.

To change the icon itself, replace the PNGs in `winres\` and rebuild.

## Sharing it

Handing the exe to someone else is fine; it is a single portable file with no
install step. Wider distribution (managed rollout, code signing so
SmartScreen stays quiet, support) is a separate decision from running it
yourself, since it needs its own signing and support plan. All collected
data stays on the device; a report leaves the machine only if its owner
shares it.
