package export

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"perfadvisor/internal/collect"
	"perfadvisor/internal/rules"
)

// WriteBundle writes a markdown diagnostic bundle designed to be pasted into
// Claude for a tailored second opinion. perfadvisor itself sends nothing
// anywhere; the file stays on this device until the user shares it.
func WriteBundle(snap *collect.Snapshot, findings []rules.Finding, dir, version string) (string, error) {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	w("# perfadvisor diagnostic bundle")
	w("")
	w("Suggested prompt: \"This is a local performance diagnostic of my Windows laptop (perfadvisor v%s). Explain what is most likely slowing it down, rank the three most impactful actions I can take myself, and flag anything I should raise with IT instead.\"", version)
	w("")
	w("## System")
	w("")
	w("- Host: %s (%s)", snap.Hostname, snap.OSVersion)
	w("- Taken: %s, sample window %d s, elevated: %v", snap.TakenAt.Format("2006-01-02 15:04"), snap.SampleSeconds, snap.Elevated)
	w("- CPU: %s, %d logical cores, avg %.0f%% peak %.0f%% during the sample", snap.CPUModel, snap.LogicalCores, snap.CPUAvgPercent, snap.CPUPeakPercent)
	w("- Memory: %.1f of %.1f GB in use (%.0f%%)", snap.Memory.UsedMB/1024, snap.Memory.TotalMB/1024, snap.Memory.UsedPercent)
	for _, d := range snap.Disks {
		sys := ""
		if d.System {
			sys = " (system)"
		}
		w("- Disk %s%s: %.0f GB free of %.0f GB (%.0f%% used)", d.Mount, sys, d.FreeGB, d.TotalGB, d.UsedPercent)
	}
	w("- Uptime: %.1f days, kernel session since %s", snap.Uptime.Hours()/24, snap.BootTime.Format("2006-01-02 15:04"))
	if snap.PowerPlan.Name != "" {
		w("- Power plan: %s", snap.PowerPlan.Name)
	}
	for _, r := range snap.PendingReboot {
		w("- Pending reboot: %s", r)
	}

	w("")
	w("## Pressure and saturation (live counters, sampled during this run)")
	w("")
	w("Same signals the live dashboard's pressure gauges and verdict line use, averaged and peaked")
	w("across the whole sample window rather than one instant.")
	if snap.Pressure.OK {
		p := snap.Pressure
		w("- CPU queue (runnable threads waiting): avg %.1f, peak %.1f, on %d logical cores", p.CPUQueueAvg, p.CPUQueuePeak, snap.LogicalCores)
		if p.CPUPerfPctAvg > 0 {
			w("- CPU clock: avg %.0f%% of base, min %.0f%% of base (below 100%% is normal turbo-down; sustained below 75%% usually means thermal or power throttling)", p.CPUPerfPctAvg, p.CPUPerfPctMin)
		}
		w("- Disk: latency avg %.1f ms / peak %.1f ms, queue avg %.1f / peak %.1f", p.DiskLatMsAvg, p.DiskLatMsPeak, p.DiskQueueAvg, p.DiskQueuePeak)
		w("- Memory hard faults (pages read back from disk): avg %.0f/s, peak %.0f/s; high values while RAM is also full mean active swapping", p.HardFaultsAvg, p.HardFaultsPeak)
		if p.GPUPctAvg > 0 || p.GPUPctPeak > 0 {
			w("- GPU (busiest engine, Task Manager style): avg %.0f%%, peak %.0f%%", p.GPUPctAvg, p.GPUPctPeak)
		}
	} else {
		w("- Performance counters were unavailable this run.")
	}
	if snap.Cores.NCores > 0 {
		w("- Core spread: busiest %s averaged %.0f%%, quietest core averaged %.0f%%, across %d logical cores (a wide gap points at a single-thread-bound workload)",
			snap.Cores.MaxCoreName, snap.Cores.MaxCoreAvg, snap.Cores.MinCoreAvg, snap.Cores.NCores)
	}
	if snap.Wifi.OK && snap.Wifi.Samples > 0 {
		w("- WiFi %s: signal avg %.0f%%, worst %d%%, link rate avg %.0f Mbps, %d disconnect(s) during the sample",
			snap.Wifi.SSID, snap.Wifi.SignalPctAvg, snap.Wifi.SignalPctMin, snap.Wifi.LinkMbpsAvg, snap.Wifi.Drops)
	} else if snap.Wifi.OK {
		w("- WiFi: not connected during the sample (on ethernet, or disconnected throughout)")
	}
	if snap.Power.OK {
		bs, be := "unknown", "unknown"
		if snap.Power.BatteryPctStart >= 0 {
			bs = fmt.Sprintf("%d%%", snap.Power.BatteryPctStart)
		}
		if snap.Power.BatteryPctEnd >= 0 {
			be = fmt.Sprintf("%d%%", snap.Power.BatteryPctEnd)
		}
		power := "AC power throughout, no battery"
		if snap.Power.HasBattery {
			power = fmt.Sprintf("battery %s at the start, %s at the end", bs, be)
			if snap.Power.OnACStart || snap.Power.OnACEnd {
				power += " (on AC for part of the sample)"
			} else {
				power += " (on battery throughout)"
			}
		}
		if snap.Power.SaverActive {
			power += "; battery saver was on at the end of the sample (caps CPU)"
		}
		w("- Power: %s", power)
	}

	w("")
	w("## Built-in findings (rule-based, offline)")
	for _, f := range findings {
		w("")
		w("### [%s] %s", f.Severity, f.Title)
		w("")
		w("%s", f.Summary)
		for _, e := range f.Evidence {
			w("- Evidence: %s", e)
		}
		for _, a := range f.Advice {
			w("- Advice: %s", a)
		}
	}
	if len(findings) == 0 {
		w("")
		w("None. Nothing crossed a threshold.")
	}

	w("")
	w("## Top processes by CPU (share of one core, can exceed 100)")
	w("")
	w("| Process | CPU %% | Memory MB |")
	w("|---|---:|---:|")
	for _, p := range snap.TopCPU {
		w("| %s | %.1f | %.0f |", p.Name, p.CPUPercent, p.MemoryMB)
	}
	w("")
	w("## Top processes by memory")
	w("")
	w("| Process | Memory MB | CPU %% |")
	w("|---|---:|---:|")
	for _, p := range snap.TopMem {
		w("| %s | %.0f | %.1f |", p.Name, p.MemoryMB, p.CPUPercent)
	}

	w("")
	w("## Autoruns at logon")
	w("")
	w("| Name | Source | State | Command |")
	w("|---|---|---|---|")
	for _, a := range snap.Autoruns {
		state := "disabled"
		if a.Enabled {
			state = "enabled"
			if !a.EnabledKnown {
				state = "enabled (assumed)"
			}
		}
		w("| %s | %s | %s | %s |", a.Name, a.Source, state, strings.ReplaceAll(a.Command, "|", "\\|"))
	}

	if len(snap.BootEvents) > 0 {
		w("")
		w("## Boot history (Diagnostics-Performance log, newest first)")
		w("")
		w("| When | Event | Detail | ms |")
		w("|---|---|---|---:|")
		for _, e := range snap.BootEvents {
			if e.EventID == 100 {
				w("| %s | 100 | full boot | %d |", e.Time.Format("2006-01-02 15:04"), e.BootMs)
			} else {
				w("| %s | %d | %s | %d |", e.Time.Format("2006-01-02 15:04"), e.EventID, e.App, e.DegradationMs)
			}
		}
	}

	if len(snap.HangEvents) > 0 {
		w("")
		w("## Application hangs, last 14 days")
		w("")
		for _, h := range snap.HangEvents {
			w("- %s: %s", h.Time.Format("2006-01-02 15:04"), h.App)
		}
	}

	if len(snap.AutoServices) > 0 {
		w("")
		w("## Third-party services with automatic (non-delayed) start")
		w("")
		w("%s", strings.Join(snap.AutoServices, ", "))
	}

	if len(snap.Unchecked) > 0 {
		w("")
		w("## Not checked this run")
		w("")
		for _, u := range snap.Unchecked {
			w("- %s: %s", u.What, u.Why)
		}
	}

	path := filepath.Join(dir, "perfadvisor-bundle-"+snap.TakenAt.Format("20060102-150405")+".md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
