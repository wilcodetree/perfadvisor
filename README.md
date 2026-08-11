# perfadvisor

A small, portable Windows diagnostic advisor. Run it, and it tells you what is
making your laptop heavy or blocked, especially at startup, and what to do
about it. The advice is the product; the live monitor view is a supporting
feature.

## What it does

- Samples the live system (CPU, memory, disks, per-process usage) for about a minute.
- Live saturation signals via Windows performance counters (PDH, English counter
  names so any Windows language works): processor queue, CPU throttling, disk
  queue and latency, hard faults, GPU engine utilization, per-core spread, wifi
  signal and disconnects, and battery/AC state. `analyze` and `export` sample
  these for the whole run (average and peak), not just the live dashboard.
- Mines what Windows already logged: boot duration and boot degradation culprits
  (Diagnostics-Performance log), application hangs, autorun inventory including
  Task Manager's enabled/disabled state, power plan, pending reboots, and
  third-party automatic services.
- Runs a curated offline rule set, including throttling, disk-bound, gpu-saturated,
  memory-thrashing, single-thread-bound core imbalance, and wifi instability, and
  writes a self-contained HTML report with ranked findings and concrete advice.
- Exports a markdown diagnostic bundle you can paste into Claude for a tailored
  second opinion, including the raw pressure/wifi/battery numbers even when no
  rule fired.

Everything stays on this device. No network calls, no central collection, no
background process. One explicit exception exists: the optional Microsoft To Do
panel ('y' in the dashboard) calls Microsoft Graph with your own account, and
only after you run `perfadvisor todo login` once. It is off by default, its
token is stored locally, and task data is shown live only, never written into
reports or exports. Best-effort as a standard user; the report lists anything
it could not read. Running elevated unlocks deeper boot analysis.

## Run on another device

See `INSTALL.md`: copy the single exe, no install, no admin. SmartScreen note
included there.

## Build

Requires Go (winget install GoLang.Go). Then, in PowerShell, from this folder:

    .\build.ps1

First build downloads dependencies (gopsutil, bubbletea, x/sys) and produces
`perfadvisor.exe`, a single portable binary. Copy it anywhere; no install.

## Use

    perfadvisor              live monitor (TUI): pressure gauges (cpu queue,
                             throttling, disk latency, hard faults, gpu), a live
                             bottleneck verdict, a combined cpu/ram/disk/net/gpu
                             history line graph, wifi signal, battery state, and
                             a compact process table with per-process disk IO, and a wifi
                             history graph (signal, link rate, disconnects) for flaky-wifi
                             offices; 'w' toggles an 18 min or 6 h window (long window
                             keeps the worst value of each 30 s bucket).
                             Keys: q quit, i info, c/m/d sort, t tree, p per-core history,
                             j/k scroll, a analyze, e export, o open last report
                             (F11 in Windows Terminal for fullscreen)
    perfadvisor analyze      full analysis, writes the HTML report (add -open to open it)
    perfadvisor export       writes the markdown bundle for Claude
    perfadvisor help         all flags

Reports and bundles land in a `reports` folder next to the exe if writable,
otherwise in `%LOCALAPPDATA%\perfadvisor\reports`. Override with `-out DIR`.

The dashboard adapts to the terminal: wide screens get a three-column layout,
tall portrait screens get stacked full-width panels and a taller process table.

## Layout

    main.go              entry point and CLI
    internal/collect     snapshot types, live sampling, Windows collectors
    internal/rules       rule engine and the v1 rule set
    internal/report      HTML report (embedded template)
    internal/export      markdown diagnostic bundle
    internal/tui         live monitor view (bubbletea)

## Status

Personal project, in daily use on one machine. Everything runs locally and
nothing is collected centrally by design. If you hand a report to someone
else, remember it lists running processes and installed software on that
specific machine, so only share it with the owner's knowledge.
