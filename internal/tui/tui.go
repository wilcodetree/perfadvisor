package tui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"

	"perfadvisor/internal/collect"
	"perfadvisor/internal/export"
	"perfadvisor/internal/report"
	"perfadvisor/internal/rules"
	"perfadvisor/internal/todo"
)

const analyzeSeconds = 30 // TUI-triggered analysis samples shorter than the CLI default

const (
	sortCPU = iota
	sortMem
	sortDisk
)

var curVersion string

func Run(version, readme string) error {
	curVersion = version
	m := model{version: version, readme: readme, status: "ready"}
	if hn, err := os.Hostname(); err == nil {
		m.hostname = hn
	}
	if bt, err := host.BootTime(); err == nil {
		m.bootTime = time.Unix(int64(bt), 0)
	}
	if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
		m.baseGHz = infos[0].Mhz / 1000
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

type model struct {
	version  string
	hostname string
	bootTime time.Time
	baseGHz  float64

	width, height int
	readme        string
	helpOn        bool
	helpScroll    int
	sortMode      int
	treeMode      bool
	coreZoom      bool
	scroll        int
	busy          bool
	status        string
	lastReport    string

	m metricsMsg

	// histories for the combined line graph and per-core zoom
	cpuHist   []float64
	memHist   []float64
	diskHist  []float64 // summed R+W bytes/s, scaled at render time
	netHist   []float64 // summed up+down bytes/s, scaled at render time
	gpuHist   []float64
	downHist  []float64
	upHist    []float64
	wifiHist  []float64 // signal %, 0 while disconnected
	linkHist  []float64 // link Mbps, scaled at render time
	wifiHistL []float64 // long window: worst signal per 30 s bucket, ~6 h
	linkHistL []float64 // long window: worst link rate per 30 s bucket
	wifiLong  bool      // 'w' toggles the wifi graph window
	todoOn    bool      // 'y' toggles the Microsoft To Do panel (opt-in)
	todoItems []todo.Item
	todoErr   string
	todoAge   int
	bucketN   int
	bucketSig float64
	bucketLnk float64
	coresHist [][]float64
}

type tickMsg struct{}

type analyzeDoneMsg struct {
	path  string
	score int
	n     int
	err   error
}

type exportDoneMsg struct {
	path string
	err  error
}

type todoMsg struct {
	items []todo.Item
	err   string
}

func fetchTodo() tea.Msg {
	items, err := todo.TodayTasks()
	if err != nil {
		return todoMsg{err: err.Error()}
	}
	return todoMsg{items: items}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tea.SetWindowTitle("perfadvisor"), sample)
}

func push(hist []float64, v float64) []float64 {
	hist = append(hist, v)
	if len(hist) > 720 {
		hist = hist[len(hist)-720:]
	}
	return hist
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		return m, sample
	case metricsMsg:
		m.m = msg
		m.cpuHist = push(m.cpuHist, msg.cpuPct)
		m.memHist = push(m.memHist, msg.memPct)
		var dsum float64
		for _, d := range msg.disks {
			dsum += d.readBps + d.writeBps
		}
		m.diskHist = push(m.diskHist, dsum)
		m.netHist = push(m.netHist, msg.net.downBps+msg.net.upBps)
		m.downHist = push(m.downHist, msg.net.downBps)
		m.upHist = push(m.upHist, msg.net.upBps)
		g := msg.pressure.gpuPct
		if g < 0 {
			g = 0
		}
		m.gpuHist = push(m.gpuHist, g)
		sig, link := 0.0, 0.0
		if msg.wifi.connected {
			sig = float64(msg.wifi.signalPct)
			link = msg.wifi.linkMbps
		}
		m.wifiHist = push(m.wifiHist, sig)
		m.linkHist = push(m.linkHist, link)
		// Long window: keep the worst value seen in each 30 s bucket so a
		// short drop still shows up in the 6 h view.
		if m.bucketN == 0 {
			m.bucketSig, m.bucketLnk = sig, link
		} else {
			m.bucketSig = min(m.bucketSig, sig)
			m.bucketLnk = min(m.bucketLnk, link)
		}
		m.bucketN++
		if m.bucketN >= 20 { // 20 ticks of 1.5 s = 30 s
			m.wifiHistL = push(m.wifiHistL, m.bucketSig)
			m.linkHistL = push(m.linkHistL, m.bucketLnk)
			m.bucketN = 0
		}
		if len(m.coresHist) != len(msg.cores) {
			m.coresHist = make([][]float64, len(msg.cores))
		}
		for i, v := range msg.cores {
			m.coresHist[i] = push(m.coresHist[i], v)
		}
		if m.scroll >= len(msg.rows) {
			m.scroll = 0
		}
		cmds := []tea.Cmd{tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })}
		if m.todoOn {
			m.todoAge++
			if m.todoAge >= 200 { // refresh tasks about every 5 minutes
				m.todoAge = 0
				cmds = append(cmds, fetchTodo)
			}
		}
		return m, tea.Batch(cmds...)
	case analyzeDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.status = "analysis failed: " + msg.err.Error()
		} else {
			m.lastReport = msg.path
			m.status = fmt.Sprintf("score %d/100, %d finding(s). Report: %s  (press o to open)", msg.score, msg.n, msg.path)
		}
		return m, nil
	case todoMsg:
		m.todoItems = msg.items
		m.todoErr = msg.err
		return m, nil
	case exportDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.status = "export failed: " + msg.err.Error()
		} else {
			m.lastReport = msg.path
			m.status = "bundle written: " + msg.path + "  (paste into Claude; press o to open)"
		}
		return m, nil
	case tea.KeyMsg:
		if m.helpOn {
			switch msg.String() {
			case "i", "esc":
				m.helpOn = false
			case "q", "ctrl+c":
				return m, tea.Quit
			case "down", "j":
				m.helpScroll++
			case "up", "k":
				m.helpScroll--
			case "pgdown":
				m.helpScroll += 10
			case "pgup":
				m.helpScroll -= 10
			case "g":
				m.helpScroll = 0
			}
			if m.helpScroll < 0 {
				m.helpScroll = 0
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "i":
			m.helpOn = true
			m.helpScroll = 0
		case "c":
			m.sortMode = sortCPU
		case "m":
			m.sortMode = sortMem
		case "d":
			m.sortMode = sortDisk
		case "t":
			m.treeMode = !m.treeMode
		case "p":
			m.coreZoom = !m.coreZoom
		case "w":
			m.wifiLong = !m.wifiLong
		case "y":
			m.todoOn = !m.todoOn
			if m.todoOn {
				m.todoErr = ""
				m.todoAge = 0
				return m, fetchTodo
			}
		case "down", "j":
			m.scroll++
		case "up", "k":
			m.scroll--
		case "pgdown":
			m.scroll += 10
		case "pgup":
			m.scroll -= 10
		case "a":
			if !m.busy {
				m.busy = true
				m.status = fmt.Sprintf("analyzing: sampling %d s, then mining Windows history...", analyzeSeconds)
				return m, runAnalysis
			}
		case "e":
			if !m.busy {
				m.busy = true
				m.status = fmt.Sprintf("exporting: sampling %d s, then writing the bundle...", analyzeSeconds)
				return m, runExport
			}
		case "o":
			if m.lastReport != "" && runtime.GOOS == "windows" {
				_ = exec.Command("cmd", "/c", "start", "", m.lastReport).Start()
			}
		}
		if m.scroll < 0 {
			m.scroll = 0
		}
		return m, nil
	}
	return m, nil
}

func runAnalysis() tea.Msg {
	snap := collect.Collect(collect.Options{SampleSeconds: analyzeSeconds})
	findings := rules.Evaluate(snap)
	dir, err := report.OutputDir("")
	if err != nil {
		return analyzeDoneMsg{err: err}
	}
	path, err := report.WriteHTML(snap, findings, dir, curVersion)
	if err != nil {
		return analyzeDoneMsg{err: err}
	}
	return analyzeDoneMsg{path: path, score: rules.Score(findings), n: len(findings)}
}

func runExport() tea.Msg {
	snap := collect.Collect(collect.Options{SampleSeconds: analyzeSeconds})
	findings := rules.Evaluate(snap)
	dir, err := report.OutputDir("")
	if err != nil {
		return exportDoneMsg{err: err}
	}
	path, err := export.WriteBundle(snap, findings, dir, curVersion)
	if err != nil {
		return exportDoneMsg{err: err}
	}
	return exportDoneMsg{path: path}
}
