package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	cDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	cTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	cGreen = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	cYell  = lipgloss.NewStyle().Foreground(lipgloss.Color("221"))
	cRed   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	cCyan  = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	cMag   = lipgloss.NewStyle().Foreground(lipgloss.Color("176"))
	boxSt  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("238")).Padding(0, 1)
)

func gradeStyle(pct float64) lipgloss.Style {
	switch {
	case pct >= 85:
		return cRed
	case pct >= 60:
		return cYell
	default:
		return cGreen
	}
}

// meter renders a [████░░░░] gauge of exactly `width` cells, colored by load.
func meter(pct float64, width int) string {
	p := pct
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return meterC(pct, width, gradeStyle(p))
}

// meterC is meter with an explicit fill style (for gauges where high is good).
func meterC(pct float64, width int, st lipgloss.Style) string {
	if width < 4 {
		width = 4
	}
	inner := width - 2
	p := pct
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	full := int(p/100*float64(inner) + 0.5)
	if full > inner {
		full = inner
	}
	return cDim.Render("[") +
		st.Render(strings.Repeat("█", full)) +
		cDim.Render(strings.Repeat("░", inner-full)+"]")
}

var sparkChars = []rune("▁▂▃▄▅▆▇█")

// sparkRow renders one row of a block graph (used for the small net sparklines).
func sparkRow(hist []float64, width, rows, row int) string {
	var b strings.Builder
	lo := float64(rows-1-row) * 100 / float64(rows)
	hi := lo + 100/float64(rows)
	start := len(hist) - width
	for i := start; i < len(hist); i++ {
		if i < 0 {
			b.WriteString(" ")
			continue
		}
		v := hist[i]
		switch {
		case v <= lo:
			b.WriteString(" ")
		case v >= hi:
			b.WriteString(gradeStyle(v).Render("█"))
		default:
			idx := int((v - lo) / (hi - lo) * 7)
			b.WriteString(gradeStyle(v).Render(string(sparkChars[idx])))
		}
	}
	return b.String()
}

// scaleTo maps arbitrary positive values onto 0..100 against their own max.
func scaleTo(hist []float64) []float64 {
	var top float64
	for _, v := range hist {
		if v > top {
			top = v
		}
	}
	if top <= 0 {
		top = 1
	}
	out := make([]float64, len(hist))
	for i, v := range hist {
		out[i] = v / top * 100
	}
	return out
}

// series is one line in the braille graph; vals are on a 0..100 scale.
type series struct {
	vals  []float64
	style lipgloss.Style
}

// braille dot bit for (row 0..3, col 0..1) inside one cell.
var brailleBits = [4][2]int{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}

// graph renders a multi-series braille line chart, w cells wide and hCells
// tall (resolution 2w x 4hCells dots). Later series draw on top of earlier
// ones, so put the most important series last.
func graph(w, hCells int, ss ...series) []string {
	if w < 2 {
		w = 2
	}
	if hCells < 1 {
		hCells = 1
	}
	dw, dh := w*2, hCells*4
	type cell struct {
		bits  int
		color int
	}
	grid := make([]cell, w*hCells)
	setDot := func(x, y, colIdx int) {
		if x < 0 || x >= dw || y < 0 || y >= dh {
			return
		}
		c := &grid[(y/4)*w+x/2]
		c.bits |= brailleBits[y%4][x%2]
		c.color = colIdx
	}
	for si, s := range ss {
		if len(s.vals) == 0 {
			continue
		}
		start := len(s.vals) - dw
		prevY := -1
		for x := 0; x < dw; x++ {
			i := start + x
			if i < 0 {
				continue
			}
			v := s.vals[i]
			if v < 0 {
				v = 0
			}
			if v > 100 {
				v = 100
			}
			y := dh - 1 - int(v/100*float64(dh-1)+0.5)
			setDot(x, y, si)
			// connect steep jumps so the series reads as a line
			if prevY >= 0 {
				step := 1
				if y < prevY {
					step = -1
				}
				for yy := prevY + step; yy != y && yy != y+step; yy += step {
					setDot(x, yy, si)
				}
			}
			prevY = y
		}
	}
	lines := make([]string, hCells)
	for cy := 0; cy < hCells; cy++ {
		var b strings.Builder
		for cx := 0; cx < w; cx++ {
			c := grid[cy*w+cx]
			if c.bits == 0 {
				b.WriteString(" ")
				continue
			}
			b.WriteString(ss[c.color].style.Render(string(rune(0x2800 + c.bits))))
		}
		lines[cy] = b.String()
	}
	return lines
}

func humanBps(bps float64) string {
	switch {
	case bps >= 1<<30:
		return fmt.Sprintf("%.1f GB/s", bps/(1<<30))
	case bps >= 1<<20:
		return fmt.Sprintf("%.1f MB/s", bps/(1<<20))
	case bps >= 1<<10:
		return fmt.Sprintf("%.1f KB/s", bps/(1<<10))
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

func humanBytes(n uint64) string {
	f := float64(n)
	switch {
	case f >= 1<<40:
		return fmt.Sprintf("%.1f TB", f/(1<<40))
	case f >= 1<<30:
		return fmt.Sprintf("%.1f GB", f/(1<<30))
	case f >= 1<<20:
		return fmt.Sprintf("%.1f MB", f/(1<<20))
	default:
		return fmt.Sprintf("%.0f KB", f/(1<<10))
	}
}

// pad truncates or right-pads a plain (uncolored) string to exactly w cells.
func pad(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		if w <= 3 {
			return string(r[:w])
		}
		return string(r[:w-3]) + "..."
	}
	return s + strings.Repeat(" ", w-len(r))
}

// rpad right-aligns a plain string in exactly w cells.
func rpad(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		return string(r[:w])
	}
	return strings.Repeat(" ", w-len(r)) + s
}

// box wraps content lines in a rounded border with a title; total width w.
func box(title string, lines []string, w int) string {
	inner := w - 4
	if inner < 8 {
		inner = 8
	}
	content := cTitle.Render(pad(title, inner))
	for _, l := range lines {
		content += "\n" + l
	}
	return boxSt.Width(w - 2).Render(content)
}

// equalize pads the shorter line slices with blanks so boxes match heights.
func equalize(all ...*[]string) {
	longest := 0
	for _, l := range all {
		if len(*l) > longest {
			longest = len(*l)
		}
	}
	for _, l := range all {
		for len(*l) < longest {
			*l = append(*l, "")
		}
	}
}
