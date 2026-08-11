package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func (m model) View() string {
	w, h := m.width, m.height
	if w < 60 {
		w = 80
	}
	if h < 20 {
		h = 30
	}

	if m.helpOn {
		return m.helpView(w, h)
	}

	head := m.header(w)
	pressBox := box("pressure", m.pressureLines(w-4), w)
	cpuBox := box(fmt.Sprintf("cpu, %d threads", len(m.m.cores)), m.cpuLines(w-4), w)

	var mid string
	if w >= 110 {
		bw := w / 3
		lw := w - 2*bw
		memL := m.memLines(bw - 4)
		dskL := m.diskLines(bw-4, 3)
		netL := m.netLines(lw - 4)
		equalize(&memL, &dskL, &netL)
		mid = lipgloss.JoinHorizontal(lipgloss.Top,
			box("memory", memL, bw),
			box("disks", dskL, bw),
			box("network", netL, lw))
	} else {
		mid = lipgloss.JoinVertical(lipgloss.Left,
			box("memory", m.memLines(w-4), w),
			box("disks", m.diskLines(w-4, 5), w),
			box("network", m.netLines(w-4), w))
	}

	// WiFi history box, only once wifi has been seen this session.
	wifiBox := ""
	wifiH := 0
	if m.wifiSeen() {
		title := "wifi history, 18 min window"
		if m.wifiLong {
			title = "wifi history, 6 h window"
		}
		if m.m.wifi.ssid != "" {
			title += ", " + m.m.wifi.ssid
		}
		wifiBox = box(title, m.wifiGraphLines(w-4, 5), w)
		wifiH = lipgloss.Height(wifiBox)
	}

	// Microsoft To Do panel, opt-in via 'y' (requires 'perfadvisor todo login').
	todoBox := ""
	todoH := 0
	if m.todoOn {
		todoBox = box("today, Microsoft To Do", m.todoLines(w-4), w)
		todoH = lipgloss.Height(todoBox)
	}

	// The process box stays compact; the history graph absorbs spare height.
	procRows := 15
	fixed := lipgloss.Height(head) + lipgloss.Height(pressBox) + lipgloss.Height(cpuBox) + lipgloss.Height(mid) + wifiH + todoH + 1
	gh := h - fixed - (procRows + 5) - 3
	if gh < 6 {
		procRows -= 6 - gh
		if procRows < 5 {
			procRows = 5
		}
		gh = h - fixed - (procRows + 5) - 3
	}
	if gh < 4 {
		gh = 4
	}
	if gh > 34 {
		gh = 34
	}
	histBox := box("history, newest right", m.graphLines(w-4, gh), w)

	title := fmt.Sprintf("processes, %d running", m.m.nProcs)
	if m.treeMode {
		title += ", tree"
	}
	procBox := box(title, m.procLines(w-4, procRows), w)

	out := head + "\n" + pressBox + "\n" + cpuBox + "\n" + histBox + "\n" +
		mid + "\n" + procBox + "\n"
	if wifiBox != "" {
		out += wifiBox + "\n"
	}
	if todoBox != "" {
		out += todoBox + "\n"
	}
	return out + m.statusLine(w)
}

// todoLines renders the opt-in Microsoft To Do panel: open tasks due today
// plus overdue ones. Live view only; tasks never land in reports or exports.
func (m model) todoLines(iw int) []string {
	if m.todoErr != "" {
		return []string{cDim.Render(pad(m.todoErr, iw))}
	}
	if len(m.todoItems) == 0 {
		return []string{cDim.Render("no open tasks due today")}
	}
	listW := 16
	var lines []string
	for i, t := range m.todoItems {
		if i >= 8 {
			lines = append(lines, cDim.Render(fmt.Sprintf("...and %d more", len(m.todoItems)-8)))
			break
		}
		mark := cGreen.Render("- ")
		title := t.Title
		if t.Overdue {
			mark = cRed.Render("! ")
			title += " (overdue, due " + t.DueDate + ")"
		}
		lines = append(lines, mark+pad(title, iw-2-listW)+cDim.Render(pad(" "+t.List, listW)))
	}
	return lines
}

// wifiSeen reports whether wifi was connected at any point this session.
func (m model) wifiSeen() bool {
	if m.m.wifi.connected {
		return true
	}
	for _, v := range m.wifiHist {
		if v > 0 {
			return true
		}
	}
	return false
}

// wifiGraphLines: signal as an absolute 0-100 line, link rate scaled to its
// own peak, plus a drop counter. A line falling to zero is a disconnect.
func (m model) wifiGraphLines(iw, gh int) []string {
	wf := m.m.wifi
	sigH, linkH := m.wifiHist, m.linkHist
	if m.wifiLong {
		sigH, linkH = m.wifiHistL, m.linkHistL
	}
	drops := 0
	for i := 1; i < len(sigH); i++ {
		if sigH[i] == 0 && sigH[i-1] > 0 {
			drops++
		}
	}
	var state string
	switch {
	case !wf.connected:
		state = cRed.Render("disconnected")
	case wf.signalPct < 50:
		state = cRed.Render(fmt.Sprintf("signal %d%%", wf.signalPct))
	case wf.signalPct < 70:
		state = cYell.Render(fmt.Sprintf("signal %d%%", wf.signalPct))
	default:
		state = cGreen.Render(fmt.Sprintf("signal %d%%", wf.signalPct))
	}
	if wf.connected && wf.linkMbps > 0 {
		state += cDim.Render(fmt.Sprintf(", link %.0f Mbps", wf.linkMbps))
	}
	legend := cGreen.Render("── signal % ") + cCyan.Render("  ── link (scaled)  ") + state
	dropTxt := fmt.Sprintf("   %d drop(s) in window", drops)
	if drops > 0 {
		legend += cRed.Render(dropTxt)
	} else {
		legend += cDim.Render(dropTxt)
	}
	if m.wifiLong {
		legend += cDim.Render("   each point = worst of 30 s, w toggles")
	} else {
		legend += cDim.Render("   w toggles 6 h window")
	}
	g := graph(iw, gh,
		series{scaleTo(linkH), cCyan},
		series{sigH, cGreen})
	return append([]string{legend}, g...)
}

func (m model) header(w int) string {
	up := 0.0
	if !m.bootTime.IsZero() {
		up = time.Since(m.bootTime).Hours() / 24
	}
	left := cTitle.Render("perfadvisor") + "  " + m.hostname + cDim.Render(fmt.Sprintf("  up %.1fd", up))
	right := cDim.Render("v" + m.version + "  " + time.Now().Format("2006-01-02 15:04:05"))
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line1 := left + strings.Repeat(" ", gap) + right
	line2 := cDim.Render(pad("q quit  i info  c/m/d sort  t tree  p cores  w wifi  y tasks  j/k scroll  a analyze  e export  o open", w))
	return line1 + "\n" + line2
}

func (m model) statusLine(w int) string {
	return cDim.Render(pad(" "+m.status, w))
}

// helpView is the fullscreen info overlay: key reference, CLI reference,
// then the embedded README. Scrollable; i or esc closes it.
func (m model) helpView(w, h int) string {
	lines := m.helpLines(w - 4)
	visible := h - 5
	if visible < 5 {
		visible = 5
	}
	off := m.helpScroll
	if off > len(lines)-visible {
		off = len(lines) - visible
	}
	if off < 0 {
		off = 0
	}
	end := off + visible
	if end > len(lines) {
		end = len(lines)
	}
	title := fmt.Sprintf("perfadvisor v%s, info, line %d-%d of %d", m.version, off+1, end, len(lines))
	return box(title, lines[off:end], w) + "\n" +
		cDim.Render(pad(" i or esc close, j/k or PgUp/PgDn scroll, g top, q quit", w))
}

func (m model) helpLines(iw int) []string {
	var lines []string
	add := func(s string) { lines = append(lines, pad(s, iw)) }
	addT := func(s string) { lines = append(lines, cTitle.Render(pad(s, iw))) }
	addT("Keys and toggles")
	for _, k := range [][2]string{
		{"q", "quit"},
		{"i", "this info screen (i or esc closes)"},
		{"c / m / d", "sort processes by CPU, memory, or disk IO"},
		{"t", "process tree view on/off"},
		{"p", "per-core history graphs on/off"},
		{"w", "wifi graph window: 18 min or 6 h (worst per 30 s)"},
		{"y", "Microsoft To Do panel on/off (run 'perfadvisor todo login' once)"},
		{"j / k, PgUp/PgDn", "scroll the process list"},
		{"a", "run full analysis, writes the HTML advice report"},
		{"e", "export the markdown diagnostic bundle for Claude"},
		{"o", "open the last report or bundle"},
		{"F11", "fullscreen (Windows Terminal feature)"},
	} {
		add("  " + pad(k[0], 18) + k[1])
	}
	add("")
	addT("Command line")
	for _, k := range [][2]string{
		{"perfadvisor", "start this dashboard"},
		{"perfadvisor analyze", "headless analysis; -seconds N, -out DIR, -open"},
		{"perfadvisor export", "markdown bundle; -seconds N, -out DIR"},
		{"perfadvisor todo login|logout", "Microsoft To Do sign-in (the only network feature)"},
		{"perfadvisor version | help", "version and full CLI help"},
	} {
		add("  " + pad(k[0], 32) + k[1])
	}
	add("")
	addT("README")
	for _, l := range strings.Split(m.readme, "\n") {
		add(strings.ReplaceAll(l, "\t", "    "))
	}
	return lines
}

// topProc returns the process with the highest value for the given metric.
func (m model) topProc(val func(procRow) float64) (string, float64) {
	var name string
	best := 0.0
	for _, r := range m.m.rows {
		if v := val(r); v > best {
			best, name = v, r.name
		}
	}
	return name, best
}

// verdictLine turns the pressure gauges into one plain-language conclusion.
func (m model) verdictLine(iw int) string {
	p := m.m.pressure
	cores := len(m.m.cores)
	if cores == 0 {
		cores = 1
	}
	sev := 0
	text := "healthy: nothing saturated"
	set := func(s int, t string) {
		if s > sev {
			sev, text = s, t
		}
	}
	if p.ok {
		if p.cpuPerfPct >= 0 && p.cpuPerfPct < 75 {
			set(1, fmt.Sprintf("cpu throttled to %.0f%% of base clock (thermal or power limit)", p.cpuPerfPct))
		}
		if p.gpuPct > 90 {
			set(1, fmt.Sprintf("gpu saturated: %.0f%%", p.gpuPct))
		}
		if p.cpuQueue > float64(2*cores) {
			t := fmt.Sprintf("cpu-saturated: run queue %.0f on %d threads", p.cpuQueue, cores)
			if name, v := m.topProc(func(r procRow) float64 { return r.cpu }); name != "" {
				t += fmt.Sprintf(", top: %s %.0f%%", name, v)
			}
			set(2, t)
		}
		if p.hardFaults > 200 && m.m.memPct > 80 {
			t := fmt.Sprintf("memory thrashing: %.0f hard faults/s at %.0f%% RAM", p.hardFaults, m.m.memPct)
			if name, v := m.topProc(func(r procRow) float64 { return r.mem }); name != "" {
				t += fmt.Sprintf(", top: %s %.0f MB", name, v)
			}
			set(2, t)
		}
		if p.diskLatMs > 25 || p.diskQueue > 2 {
			t := fmt.Sprintf("disk-bound: %.1f ms latency, queue %.1f", max(p.diskLatMs, 0), max(p.diskQueue, 0))
			if name, v := m.topProc(func(r procRow) float64 { return r.disk }); name != "" && v > 1024 {
				t += ", top: " + name + " " + humanBps(v)
			}
			set(2, t)
		}
	}
	if sev == 0 && m.m.memPct > 92 {
		set(1, fmt.Sprintf("memory nearly full: %.0f%% used", m.m.memPct))
	}
	st := cGreen
	if sev == 2 {
		st = cRed
	} else if sev == 1 {
		st = cYell
	}
	left := st.Render(text)

	right := ""
	if pw := m.m.power; pw.ok {
		pct := "?"
		if pw.percent >= 0 {
			pct = fmt.Sprintf("%d%%", pw.percent)
		}
		switch {
		case pw.hasBattery && !pw.onAC && pw.saver:
			right = cYell.Render("on battery " + pct + ", saver on")
		case pw.hasBattery && !pw.onAC:
			right = cYell.Render("on battery " + pct)
		case pw.hasBattery && pw.saver:
			right = cYell.Render("AC, battery " + pct + ", saver on (caps cpu)")
		case pw.hasBattery:
			right = cDim.Render("AC, battery " + pct)
		default:
			right = cDim.Render("AC power")
		}
	}
	gap := iw - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// pressureLines: verdict, then five saturation gauges.
func (m model) pressureLines(iw int) []string {
	verdict := m.verdictLine(iw)
	p := m.m.pressure
	if !p.ok {
		return []string{verdict, cDim.Render("performance counters unavailable on this system")}
	}
	cores := len(m.m.cores)
	if cores == 0 {
		cores = 1
	}
	cw := iw / 5
	if cw < 14 {
		cw = 14
	}
	type gauge struct {
		label string
		fill  float64
		st    lipgloss.Style
	}
	var g []gauge
	if p.cpuQueue < 0 {
		g = append(g, gauge{"cpu queue --", 0, cDim})
	} else {
		fill := p.cpuQueue / float64(2*cores) * 100
		g = append(g, gauge{fmt.Sprintf("cpu queue %.1f", p.cpuQueue), fill, gradeStyle(fill)})
	}
	if p.cpuPerfPct < 0 {
		g = append(g, gauge{"speed --", 0, cDim})
	} else {
		st := cGreen
		if p.cpuPerfPct < 75 {
			st = cRed
		} else if p.cpuPerfPct < 90 {
			st = cYell
		}
		label := fmt.Sprintf("speed %.0f%%", p.cpuPerfPct)
		if m.baseGHz > 0 {
			label = fmt.Sprintf("speed %.1fGHz %.0f%%", m.baseGHz*p.cpuPerfPct/100, p.cpuPerfPct)
		}
		g = append(g, gauge{label, min(p.cpuPerfPct, 100), st})
	}
	if p.diskLatMs < 0 && p.diskQueue < 0 {
		g = append(g, gauge{"disk --", 0, cDim})
	} else {
		fill := max(p.diskLatMs/40*100, p.diskQueue/4*100)
		g = append(g, gauge{fmt.Sprintf("disk %.1fms q%.1f", max(p.diskLatMs, 0), max(p.diskQueue, 0)), fill, gradeStyle(fill)})
	}
	if p.hardFaults < 0 {
		g = append(g, gauge{"hard faults --", 0, cDim})
	} else {
		fill := p.hardFaults / 400 * 100
		g = append(g, gauge{fmt.Sprintf("hard faults %.0f/s", p.hardFaults), fill, gradeStyle(fill)})
	}
	if p.gpuPct < 0 {
		g = append(g, gauge{"gpu --", 0, cDim})
	} else {
		g = append(g, gauge{fmt.Sprintf("gpu %.0f%%", p.gpuPct), p.gpuPct, gradeStyle(p.gpuPct)})
	}
	var labels, meters string
	for i, x := range g {
		if i > 0 {
			labels += " "
			meters += " "
		}
		labels += cDim.Render(pad(x.label, cw-1))
		meters += meterC(x.fill, cw-1, x.st)
	}
	return []string{verdict, labels, meters}
}

// cpuLines: total gauge plus either the meter grid or per-core history.
func (m model) cpuLines(iw int) []string {
	gw := iw - 30
	if gw > 50 {
		gw = 50
	}
	if gw < 10 {
		gw = 10
	}
	info := fmt.Sprintf(" %5.1f%%", m.m.cpuPct)
	if m.baseGHz > 0 {
		info += cDim.Render(fmt.Sprintf("  base %.1f GHz", m.baseGHz))
	}
	if t := m.tempString(); t != "" {
		info += cDim.Render("  " + t)
	}
	lines := []string{meter(m.m.cpuPct, gw) + info}

	if m.coreZoom {
		for i, hist := range m.coresHist {
			cur := 0.0
			if len(hist) > 0 {
				cur = hist[len(hist)-1]
			}
			lines = append(lines, pad(fmt.Sprintf("C%d", i), 4)+
				rpad(fmt.Sprintf("%.0f%%", cur), 5)+" "+sparkRow(hist, iw-10, 1, 0))
		}
		return lines
	}
	const cellW = 21
	cols := (iw + 1) / (cellW + 1)
	if cols < 1 {
		cols = 1
	}
	var row string
	count := 0
	for i, v := range m.m.cores {
		cell := pad(fmt.Sprintf("C%d", i), 3) + " " + meter(v, 12) + rpad(fmt.Sprintf("%.0f%%", v), 5)
		if count == 0 {
			row = cell
		} else {
			row += " " + cell
		}
		count++
		if count == cols {
			lines = append(lines, row)
			row = ""
			count = 0
		}
	}
	if row != "" {
		lines = append(lines, row)
	}
	return lines
}

func (m model) tempString() string {
	if len(m.m.temps) == 0 {
		return ""
	}
	var ts []string
	for _, t := range m.m.temps {
		ts = append(ts, fmt.Sprintf("%s %.0fC", t.label, t.c))
	}
	return strings.Join(ts, "  ")
}

// graphLines: legend plus the multi-series braille line chart.
func (m model) graphLines(iw, gh int) []string {
	var dsum float64
	for _, d := range m.m.disks {
		dsum += d.readBps + d.writeBps
	}
	legend := cGreen.Render("── cpu ") + fmt.Sprintf("%.0f%%", m.m.cpuPct) +
		cCyan.Render("   ── ram ") + fmt.Sprintf("%.0f%%", m.m.memPct) +
		cYell.Render("   ── disk ") + humanBps(dsum) +
		cMag.Render("   ── net ") + humanBps(m.m.net.downBps+m.m.net.upBps)
	ss := []series{
		{scaleTo(m.netHist), cMag},
		{scaleTo(m.diskHist), cYell},
	}
	if m.m.pressure.gpuPct >= 0 {
		legend += cRed.Render("   ── gpu ") + fmt.Sprintf("%.0f%%", m.m.pressure.gpuPct)
		ss = append(ss, series{m.gpuHist, cRed})
	}
	legend += cDim.Render("   disk and net scaled to own peak")
	ss = append(ss, series{m.memHist, cCyan}, series{m.cpuHist, cGreen})
	return append([]string{legend}, graph(iw, gh, ss...)...)
}

func (m model) memLines(iw int) []string {
	gw := iw - 13
	if gw > 40 {
		gw = 40
	}
	if gw < 8 {
		gw = 8
	}
	return []string{
		"RAM  " + meter(m.m.memPct, gw) + rpad(fmt.Sprintf("%.1f%%", m.m.memPct), 7),
		cDim.Render(pad(fmt.Sprintf("     %.1f / %.1f GB used", m.m.memUsedGB, m.m.memTotalGB), iw)),
		cDim.Render(pad(fmt.Sprintf("     %.1f GB available", m.m.memAvailGB), iw)),
		"Swap " + meter(m.m.swapPct, gw) + rpad(fmt.Sprintf("%.1f%%", m.m.swapPct), 7),
		cDim.Render(pad(fmt.Sprintf("     %.1f / %.1f GB used (pagefile)", m.m.swapUsedGB, m.m.swapTotGB), iw)),
	}
}

func (m model) diskLines(iw, maxDisks int) []string {
	var lines []string
	for i, d := range m.m.disks {
		if i >= maxDisks {
			lines = append(lines, cDim.Render(fmt.Sprintf("...and %d more", len(m.m.disks)-maxDisks)))
			break
		}
		name := d.mount
		if d.system {
			name += "*"
		}
		lines = append(lines,
			pad(name, 4)+meter(d.usedPct, 12)+rpad(fmt.Sprintf("%.0f%%", d.usedPct), 5)+
				cDim.Render(fmt.Sprintf("  %.0fG free", d.freeGB)))
		lines = append(lines,
			cDim.Render(pad(fmt.Sprintf("     of %.0f GB, R %s, W %s",
				d.totalGB, humanBps(d.readBps), humanBps(d.writeBps)), iw)))
	}
	if len(lines) == 0 {
		lines = []string{cDim.Render("no disks visible")}
	}
	return lines
}

func (m model) netLines(iw int) []string {
	lines := []string{
		cCyan.Render("v down ") + pad(humanBps(m.m.net.downBps), 12) +
			cDim.Render(" total "+humanBytes(m.m.net.downTotal)),
		sparkRow(scaleTo(m.downHist), iw, 1, 0),
		cCyan.Render("^ up   ") + pad(humanBps(m.m.net.upBps), 12) +
			cDim.Render(" total "+humanBytes(m.m.net.upTotal)),
		sparkRow(scaleTo(m.upHist), iw, 1, 0),
	}
	if wf := m.m.wifi; wf.connected {
		st := cGreen
		if wf.signalPct < 50 {
			st = cRed
		} else if wf.signalPct < 70 {
			st = cYell
		}
		link := ""
		if wf.linkMbps > 0 {
			link = fmt.Sprintf("  %.0f Mbps", wf.linkMbps)
		}
		lines = append(lines,
			cCyan.Render("wifi   ")+pad(wf.ssid, iw-7),
			"       "+meterC(float64(wf.signalPct), 12, st)+
				rpad(fmt.Sprintf("%d%%", wf.signalPct), 5)+cDim.Render(link))
	}
	return lines
}
