package tui

import (
	"fmt"
	"sort"
	"strings"
)

func (m model) sortLess() func(a, b procRow) bool {
	switch m.sortMode {
	case sortMem:
		return func(a, b procRow) bool { return a.mem > b.mem }
	case sortDisk:
		return func(a, b procRow) bool { return a.disk > b.disk }
	default:
		return func(a, b procRow) bool { return a.cpu > b.cpu }
	}
}

// procLines renders the process table: 1 header, n rows, 1 range footer.
func (m model) procLines(iw, n int) []string {
	rows := make([]procRow, len(m.m.rows))
	copy(rows, m.m.rows)
	less := m.sortLess()
	if m.treeMode {
		rows = flattenTree(rows, less)
	} else {
		sort.SliceStable(rows, func(i, j int) bool { return less(rows[i], rows[j]) })
	}
	if n < 1 {
		n = 1
	}
	total := len(rows)
	off := m.scroll
	if off > total-n {
		off = total - n
	}
	if off < 0 {
		off = 0
	}
	end := off + n
	if end > total {
		end = total
	}
	nameW := iw - 42
	if nameW < 12 {
		nameW = 12
	}
	cpuLbl, memLbl, dskLbl := "CPU%", "MEM%", "DISK/s"
	switch m.sortMode {
	case sortMem:
		memLbl = ">MEM%"
	case sortDisk:
		dskLbl = ">DISK/s"
	default:
		cpuLbl = ">CPU%"
	}
	out := []string{cDim.Render(rpad("PID", 6) + " " + pad("PROCESS", nameW) +
		rpad(cpuLbl, 8) + rpad(memLbl, 7) + rpad("MEM MB", 9) + rpad(dskLbl, 11))}
	memTotMB := m.m.memTotalGB * 1024
	for _, r := range rows[off:end] {
		memPct := 0.0
		if memTotMB > 0 {
			memPct = r.mem / memTotMB * 100
		}
		cpuCell := rpad(fmt.Sprintf("%.1f", r.cpu), 8)
		switch {
		case r.cpu >= 50:
			cpuCell = cRed.Render(cpuCell)
		case r.cpu >= 20:
			cpuCell = cYell.Render(cpuCell)
		}
		diskCell := rpad(shortBps(r.disk), 11)
		switch {
		case r.disk >= 10*(1<<20):
			diskCell = cRed.Render(diskCell)
		case r.disk >= 1<<20:
			diskCell = cYell.Render(diskCell)
		}
		out = append(out, rpad(fmt.Sprint(r.pid), 6)+" "+pad(r.name, nameW)+cpuCell+
			rpad(fmt.Sprintf("%.1f", memPct), 7)+rpad(fmt.Sprintf("%.0f", r.mem), 9)+diskCell)
	}
	out = append(out, cDim.Render(fmt.Sprintf("%d-%d of %d", off+1, end, total)))
	return out
}

// shortBps hides sub-KB noise so the DISK column stays readable.
func shortBps(bps float64) string {
	if bps < 1024 {
		return "-"
	}
	return humanBps(bps)
}

// flattenTree turns the flat process list into parent-child display order,
// siblings sorted by the active sort key.
func flattenTree(rows []procRow, less func(a, b procRow) bool) []procRow {
	inSet := map[int32]bool{}
	for _, r := range rows {
		inSet[r.pid] = true
	}
	children := map[int32][]procRow{}
	var roots []procRow
	for _, r := range rows {
		if r.ppid == 0 || r.ppid == r.pid || !inSet[r.ppid] {
			roots = append(roots, r)
		} else {
			children[r.ppid] = append(children[r.ppid], r)
		}
	}
	sortRows := func(rs []procRow) {
		sort.SliceStable(rs, func(i, j int) bool { return less(rs[i], rs[j]) })
	}
	sortRows(roots)
	for _, c := range children {
		sortRows(c)
	}
	out := make([]procRow, 0, len(rows))
	visited := map[int32]bool{}
	var walk func(r procRow, depth int)
	walk = func(r procRow, depth int) {
		if visited[r.pid] || depth > 10 {
			return
		}
		visited[r.pid] = true
		if depth > 0 {
			r.name = strings.Repeat("  ", depth-1) + "└ " + r.name
		}
		out = append(out, r)
		for _, c := range children[r.pid] {
			walk(c, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	return out
}
