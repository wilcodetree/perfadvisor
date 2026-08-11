package rules

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"perfadvisor/internal/collect"
)

var allRules = []rule{
	bootSlow, bootDegraders, autorunOverload, syncClientsAtBoot, heavyAutoruns,
	memoryPressure, cpuSustained, diskFull, longUptime, pendingReboot,
	powerSaver, appHangs, autoServices,
}

func matchAny(hay string, needles ...string) bool {
	hay = strings.ToLower(hay)
	for _, n := range needles {
		if strings.Contains(hay, n) {
			return true
		}
	}
	return false
}

func secs(ms int64) string { return fmt.Sprintf("%.1f s", float64(ms)/1000) }

func bootSlow(s *collect.Snapshot) *Finding {
	ms := s.LatestBootMs()
	if ms < 60000 {
		return nil
	}
	sev := Medium
	if ms >= 120000 {
		sev = High
	}
	return &Finding{
		ID: "boot-slow", Severity: sev, Title: "Last measured boot was slow",
		Summary:  "Windows measured the last full boot at " + secs(ms) + ". Under 30 to 40 seconds is normal for a laptop with an SSD.",
		Evidence: []string{"Diagnostics-Performance event 100, boot duration " + secs(ms)},
		Advice: []string{
			"Work through the startup findings below; autoruns are the usual cause.",
			"Install pending Windows updates and restart once; update sessions inflate boot times.",
		},
	}
}

func bootDegraders(s *collect.Snapshot) *Finding {
	type agg struct {
		ms int64
		n  int
	}
	sums := map[string]*agg{}
	cutoff := time.Now().AddDate(0, 0, -30)
	for _, e := range s.BootEvents {
		if e.EventID == 100 || e.App == "" || e.Time.Before(cutoff) {
			continue
		}
		a := sums[e.App]
		if a == nil {
			a = &agg{}
			sums[e.App] = a
		}
		a.ms += e.DegradationMs
		a.n++
	}
	if len(sums) == 0 {
		return nil
	}
	type row struct {
		app string
		agg *agg
	}
	var rows []row
	var worst int64
	for app, a := range sums {
		rows = append(rows, row{app, a})
		if a.ms > worst {
			worst = a.ms
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].agg.ms > rows[j].agg.ms })
	if len(rows) > 5 {
		rows = rows[:5]
	}
	sev := Info
	if worst >= 60000 {
		sev = High
	} else if worst >= 15000 {
		sev = Medium
	}
	var ev []string
	for _, r := range rows {
		ev = append(ev, fmt.Sprintf("%s: %s of boot degradation across %d boot(s), last 30 days", r.app, secs(r.agg.ms), r.agg.n))
	}
	return &Finding{
		ID: "boot-degraders", Severity: sev, Title: "Windows named what degraded your boots",
		Summary:  "The Diagnostics-Performance log blames specific apps, drivers, or services for slowing recent boots.",
		Evidence: ev,
		Advice: []string{
			"Update the worst offender first; degradation events often disappear after a vendor update.",
			"If it is an app you do not need at logon, disable it in Task Manager, Startup apps.",
		},
	}
}

func autorunOverload(s *collect.Snapshot) *Finding {
	on := s.EnabledAutoruns()
	if len(on) < 8 {
		return nil
	}
	sev := Medium
	if len(on) >= 15 {
		sev = High
	}
	var ev []string
	for i, a := range on {
		if i >= 15 {
			ev = append(ev, fmt.Sprintf("...and %d more", len(on)-15))
			break
		}
		ev = append(ev, a.Name+"  ["+a.Source+"]")
	}
	return &Finding{
		ID: "autorun-overload", Severity: sev, Title: fmt.Sprintf("%d programs start at logon", len(on)),
		Summary:  "Every autorun adds to logon time and stays resident afterwards. Most machines need only a handful.",
		Evidence: ev,
		Advice: []string{
			"Open Task Manager, Startup apps, and disable everything you do not need within minutes of logging in.",
			"Disabling is safe and reversible; the program still works when you start it yourself.",
		},
	}
}

func syncClientsAtBoot(s *collect.Snapshot) *Finding {
	var hits []string
	for _, a := range s.EnabledAutoruns() {
		if matchAny(a.Name+" "+a.Command, "onedrive", "dropbox", "googledrive", "google drive", "nextcloud", "box.exe", "boxsync") {
			hits = append(hits, a.Name)
		}
	}
	if len(hits) == 0 {
		return nil
	}
	sev := Info
	if len(hits) >= 2 {
		sev = Medium
	}
	return &Finding{
		ID: "sync-at-boot", Severity: sev, Title: "File sync client(s) start at logon",
		Summary:  "Sync clients scan and hash files right after logon, which is exactly when the machine is busiest.",
		Evidence: []string{"Enabled at logon: " + strings.Join(hits, ", ")},
		Advice: []string{
			"Keep only the sync client you actually use; disable the rest at startup.",
			"Use selective sync or Files On-Demand to exclude large folders from the initial scan.",
		},
	}
}

func heavyAutoruns(s *collect.Snapshot) *Finding {
	var hits []string
	for _, a := range s.EnabledAutoruns() {
		if matchAny(a.Name+" "+a.Command, "teams", "slack", "discord", "spotify", "steam", "epicgames", "creative cloud", "creativecloud", "zoom", "webex") {
			hits = append(hits, a.Name)
		}
	}
	if len(hits) == 0 {
		return nil
	}
	sev := Info
	if len(hits) >= 3 {
		sev = Medium
	}
	return &Finding{
		ID: "heavy-autoruns", Severity: sev, Title: "Known heavy applications start at logon",
		Summary:  "These applications are heavyweight at startup and stay resident in memory.",
		Evidence: []string{"Enabled at logon: " + strings.Join(hits, ", ")},
		Advice:   []string{"Start them on demand instead: disable in Task Manager, Startup apps, and pin them to the taskbar."},
	}
}

func memoryPressure(s *collect.Snapshot) *Finding {
	p := s.Memory.UsedPercent
	if p < 85 {
		return nil
	}
	sev := Medium
	if p >= 92 {
		sev = High
	}
	ev := []string{fmt.Sprintf("%.0f%% of %.1f GB in use during the sample", p, s.Memory.TotalMB/1024)}
	for i, ps := range s.TopMem {
		if i >= 5 {
			break
		}
		ev = append(ev, fmt.Sprintf("%s: %.0f MB", ps.Name, ps.MemoryMB))
	}
	return &Finding{
		ID: "memory-pressure", Severity: sev, Title: "Memory is under pressure",
		Summary:  "When RAM runs out, Windows swaps to disk and everything feels blocked. This is the most common cause of a laptop that stutters.",
		Evidence: ev,
		Advice: []string{
			"Close or restart the biggest consumers; browsers and Electron apps grow over days.",
			"Restart the machine if it has been up for a long time.",
			"If this happens daily with your normal workload, more RAM is the honest fix; include this report when you ask IT.",
		},
	}
}

func cpuSustained(s *collect.Snapshot) *Finding {
	if s.CPUAvgPercent < 60 {
		return nil
	}
	sev := Medium
	if s.CPUAvgPercent >= 80 {
		sev = High
	}
	ev := []string{fmt.Sprintf("Average %.0f%%, peak %.0f%% over %d seconds", s.CPUAvgPercent, s.CPUPeakPercent, s.SampleSeconds)}
	for i, ps := range s.TopCPU {
		if i >= 5 {
			break
		}
		ev = append(ev, fmt.Sprintf("%s: %.0f%% of one core", ps.Name, ps.CPUPercent))
	}
	return &Finding{
		ID: "cpu-sustained", Severity: sev, Title: "CPU load stayed high during the sample",
		Summary:  "Sustained load means something is working constantly in the background, not just a burst.",
		Evidence: ev,
		Advice: []string{
			"Identify the top process; if it is an antivirus or backup scan, it should finish, but daily recurrence is worth a word with IT.",
			"If it is an app you use, update it; runaway CPU is usually a bug that is already fixed upstream.",
		},
	}
}

func diskFull(s *collect.Snapshot) *Finding {
	d := s.SystemDisk()
	if d == nil || d.TotalGB == 0 {
		return nil
	}
	freePct := 100 - d.UsedPercent
	if freePct >= 15 {
		return nil
	}
	sev := Medium
	if freePct < 8 {
		sev = High
	}
	return &Finding{
		ID: "disk-full", Severity: sev, Title: "System drive is nearly full",
		Summary:  "A nearly full system SSD slows writes and can block Windows updates.",
		Evidence: []string{fmt.Sprintf("%s %.0f GB free of %.0f GB (%.0f%% used)", d.Mount, d.FreeGB, d.TotalGB, d.UsedPercent)},
		Advice: []string{
			"Run Storage Sense (Settings, System, Storage) and empty Downloads and the recycle bin.",
			"Uninstall applications you no longer use.",
			"Move large local folders to your cloud drive with Files On-Demand.",
		},
	}
}

func longUptime(s *collect.Snapshot) *Finding {
	days := int(s.Uptime.Hours() / 24)
	if days < 14 {
		return nil
	}
	sev := Medium
	if days >= 30 {
		sev = High
	}
	return &Finding{
		ID: "long-uptime", Severity: sev, Title: fmt.Sprintf("No real restart for %d days", days),
		Summary:  "With fast startup, shutting down does not fully reset Windows; only Restart does. Long sessions accumulate leaks and pending updates.",
		Evidence: []string{"Kernel session started " + s.BootTime.Format("2006-01-02 15:04")},
		Advice:   []string{"Use Start, Power, Restart (not Shut down) once a week."},
	}
}

func pendingReboot(s *collect.Snapshot) *Finding {
	if len(s.PendingReboot) == 0 {
		return nil
	}
	return &Finding{
		ID: "pending-reboot", Severity: Medium, Title: "A reboot is pending",
		Summary:  "Windows is waiting to finish servicing work; until then, updates sit half-applied and can degrade performance.",
		Evidence: s.PendingReboot,
		Advice:   []string{"Restart the machine at the next natural break."},
	}
}

func powerSaver(s *collect.Snapshot) *Finding {
	name := strings.ToLower(s.PowerPlan.Name + " " + s.PowerPlan.Raw)
	if name == " " {
		return nil
	}
	if !strings.Contains(name, "saver") && !strings.Contains(name, "besparing") &&
		!strings.Contains(name, "a1841308-3541-4fab-bc81-f71556f20b4a") {
		return nil
	}
	return &Finding{
		ID: "power-saver", Severity: Medium, Title: "Power saver plan is active",
		Summary:  "The power saver plan caps CPU speed. Fine on battery in a pinch, slow everywhere else.",
		Evidence: []string{"Active plan: " + s.PowerPlan.Name},
		Advice:   []string{"Switch to Balanced (Settings, System, Power) at least when plugged in."},
	}
}

func appHangs(s *collect.Snapshot) *Finding {
	if len(s.HangEvents) == 0 {
		return nil
	}
	counts := map[string]int{}
	for _, h := range s.HangEvents {
		counts[h.App]++
	}
	type row struct {
		app string
		n   int
	}
	var rows []row
	for app, n := range counts {
		rows = append(rows, row{app, n})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].n > rows[j].n })
	sev := Info
	if rows[0].n >= 3 {
		sev = Medium
	}
	var ev []string
	for i, r := range rows {
		if i >= 5 {
			break
		}
		ev = append(ev, fmt.Sprintf("%s: hung %d time(s) in the last 14 days", r.app, r.n))
	}
	return &Finding{
		ID: "app-hangs", Severity: sev, Title: "Applications froze recently",
		Summary:  "Windows logged 'not responding' events. Repeat offenders point at the app, not at the machine.",
		Evidence: ev,
		Advice: []string{
			"Update the repeat offender; for Office apps, also try disabling add-ins.",
			"If it keeps hanging after an update, include this report when you raise it with IT.",
		},
	}
}

func autoServices(s *collect.Snapshot) *Finding {
	if len(s.AutoServices) < 8 {
		return nil
	}
	ev := []string{strings.Join(s.AutoServices, ", ")}
	return &Finding{
		ID: "auto-services", Severity: Info, Title: fmt.Sprintf("%d third-party services start automatically", len(s.AutoServices)),
		Summary:  "Each automatic service adds work to the boot path. Some are needed; leftovers from uninstalled software are not.",
		Evidence: ev,
		Advice:   []string{"Review with IT before changing service start types; this list is informational."},
	}
}
