//go:build windows

package collect

import (
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// This file gives analyze/export the same saturation signals the live TUI
// already reads in internal/tui/sysmetrics_windows.go: PDH performance
// counters, battery/AC state, and wifi via netsh. It is a deliberate, small
// duplication rather than a shared package: analyze/export run as a fresh
// process each invocation, so a fresh PDH query here is correct on its own,
// and it means extending the CLI path carries zero risk to the TUI code
// that is already proven working through v0.4.2. See
// docs\2026-08-11_handover-session-1.md for the history.

var (
	collectPdhDLL                   = syscall.NewLazyDLL("pdh.dll")
	collectProcPdhOpenQuery         = collectPdhDLL.NewProc("PdhOpenQueryW")
	collectProcPdhAddEnglishCounter = collectPdhDLL.NewProc("PdhAddEnglishCounterW")
	collectProcPdhCollectQueryData  = collectPdhDLL.NewProc("PdhCollectQueryData")
	collectProcPdhGetFormattedValue = collectPdhDLL.NewProc("PdhGetFormattedCounterValue")
	collectProcPdhGetFormattedArray = collectPdhDLL.NewProc("PdhGetFormattedCounterArrayW")
	collectKernel32DLL              = syscall.NewLazyDLL("kernel32.dll")
	collectProcGetSystemPowerStatus = collectKernel32DLL.NewProc("GetSystemPowerStatus")
)

const (
	collectPdhFmtDouble = 0x00000200
	collectPdhMoreData  = 0x800007D2
)

type collectPdhFmtCounterValue struct {
	CStatus     uint32
	_           uint32
	DoubleValue float64
}

type collectPdhFmtCounterValueItem struct {
	SzName   *uint16
	FmtValue collectPdhFmtCounterValue
}

type collectPdhState struct {
	query    uintptr
	counters map[string]uintptr
	gpu      uintptr
	ok       bool
	inited   bool
}

// pressurePdh is only touched from readPressureTick, which the sampling
// loop in sample.go calls once per second, in series, never concurrently.
var pressurePdh collectPdhState

// init opens one PDH query with English counter names (locale-independent)
// so this works on Dutch, German, or any other Windows language.
func (s *collectPdhState) init() {
	s.inited = true
	s.counters = map[string]uintptr{}
	if r, _, _ := collectProcPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&s.query))); r != 0 {
		return
	}
	paths := map[string]string{
		"cpuQueue":   `\System\Processor Queue Length`,
		"cpuPerf":    `\Processor Information(_Total)\% Processor Performance`,
		"diskQueue":  `\PhysicalDisk(_Total)\Avg. Disk Queue Length`,
		"diskLat":    `\PhysicalDisk(_Total)\Avg. Disk sec/Transfer`,
		"hardFaults": `\Memory\Pages Input/sec`,
	}
	for key, path := range paths {
		p, err := windows.UTF16PtrFromString(path)
		if err != nil {
			continue
		}
		var h uintptr
		if r, _, _ := collectProcPdhAddEnglishCounter.Call(s.query, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&h))); r == 0 {
			s.counters[key] = h
		}
	}
	if p, err := windows.UTF16PtrFromString(`\GPU Engine(*)\Utilization Percentage`); err == nil {
		var h uintptr
		if r, _, _ := collectProcPdhAddEnglishCounter.Call(s.query, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&h))); r == 0 {
			s.gpu = h
		}
	}
	// Prime rate counters; real values flow from the second collect on.
	collectProcPdhCollectQueryData.Call(s.query)
	s.ok = true
}

func (s *collectPdhState) read(key string) float64 {
	h, found := s.counters[key]
	if !found {
		return -1
	}
	var v collectPdhFmtCounterValue
	if r, _, _ := collectProcPdhGetFormattedValue.Call(h, collectPdhFmtDouble, 0, uintptr(unsafe.Pointer(&v))); r != 0 {
		return -1
	}
	return v.DoubleValue
}

// readGPU sums utilization per engine type across all GPU engine instances
// and returns the busiest type, which matches Task Manager's headline GPU%.
func (s *collectPdhState) readGPU() float64 {
	if s.gpu == 0 {
		return -1
	}
	var bufSize, count uint32
	r, _, _ := collectProcPdhGetFormattedArray.Call(s.gpu, collectPdhFmtDouble,
		uintptr(unsafe.Pointer(&bufSize)), uintptr(unsafe.Pointer(&count)), 0)
	if (r != 0 && r != collectPdhMoreData) || bufSize == 0 {
		return -1
	}
	buf := make([]uint64, (bufSize+7)/8) // uint64 backing keeps the items 8-byte aligned
	r, _, _ = collectProcPdhGetFormattedArray.Call(s.gpu, collectPdhFmtDouble,
		uintptr(unsafe.Pointer(&bufSize)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 || count == 0 {
		return -1
	}
	items := unsafe.Slice((*collectPdhFmtCounterValueItem)(unsafe.Pointer(&buf[0])), count)
	sums := map[string]float64{}
	for _, it := range items {
		if it.FmtValue.CStatus > 1 { // 0 valid, 1 new data
			continue
		}
		name := windows.UTF16PtrToString(it.SzName)
		typ := name
		if i := strings.LastIndex(name, "engtype_"); i >= 0 {
			typ = name[i+8:]
		}
		sums[typ] += it.FmtValue.DoubleValue
	}
	var top float64
	for _, v := range sums {
		if v > top {
			top = v
		}
	}
	if top > 100 {
		top = 100
	}
	return top
}

// pressureTick is one instantaneous read of every saturation counter.
// Fields are -1 when that counter was unavailable this tick.
type pressureTick struct {
	ok         bool
	cpuQueue   float64
	cpuPerfPct float64
	diskQueue  float64
	diskLatMs  float64
	hardFaults float64
	gpuPct     float64
}

func readPressureTick() pressureTick {
	if !pressurePdh.inited {
		pressurePdh.init()
	}
	if !pressurePdh.ok {
		return pressureTick{}
	}
	if r, _, _ := collectProcPdhCollectQueryData.Call(pressurePdh.query); r != 0 {
		return pressureTick{}
	}
	t := pressureTick{ok: true}
	t.cpuQueue = pressurePdh.read("cpuQueue")
	t.cpuPerfPct = pressurePdh.read("cpuPerf")
	t.diskQueue = pressurePdh.read("diskQueue")
	if v := pressurePdh.read("diskLat"); v >= 0 {
		t.diskLatMs = v * 1000
	} else {
		t.diskLatMs = -1
	}
	t.hardFaults = pressurePdh.read("hardFaults")
	t.gpuPct = pressurePdh.readGPU()
	return t
}

type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

type powerTick struct {
	ok         bool
	onAC       bool
	hasBattery bool
	percent    int // -1 unknown
	saver      bool
}

func readPowerTick() powerTick {
	var st systemPowerStatus
	if r, _, _ := collectProcGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&st))); r == 0 {
		return powerTick{}
	}
	p := powerTick{ok: true}
	p.onAC = st.ACLineStatus == 1
	p.hasBattery = st.BatteryFlag != 128 && st.BatteryFlag != 255
	if st.BatteryLifePercent <= 100 {
		p.percent = int(st.BatteryLifePercent)
	} else {
		p.percent = -1
	}
	p.saver = st.SystemStatusFlag == 1
	return p
}

type wifiTick struct {
	ok        bool
	connected bool
	ssid      string
	signalPct int
	linkMbps  float64
}

// readWifiTick shells out to netsh (hidden, timed out via the shared
// runHidden helper in misc_windows.go). Label matching is deliberately
// loose so it survives localized Windows output.
func readWifiTick() wifiTick {
	out, err := runHidden("netsh", "wlan", "show", "interfaces")
	if err != nil {
		return wifiTick{}
	}
	w := wifiTick{ok: true}
	for _, line := range strings.Split(out, "\n") {
		i := strings.Index(line, ":")
		if i < 0 {
			continue
		}
		label := strings.ToLower(strings.TrimSpace(line[:i]))
		value := strings.TrimSpace(line[i+1:])
		switch {
		case strings.Contains(label, "ssid") && !strings.Contains(label, "bssid") && w.ssid == "":
			w.ssid = value
		case strings.Contains(label, "sig") && strings.HasSuffix(value, "%"):
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(value, "%"))); err == nil {
				w.signalPct = n
			}
		case w.linkMbps == 0 && (strings.Contains(label, "receive") || strings.Contains(label, "ontvang") ||
			strings.Contains(label, "empfang") || strings.Contains(label, "mbps")):
			if f, err := strconv.ParseFloat(strings.ReplaceAll(value, ",", "."), 64); err == nil {
				w.linkMbps = f
			}
		}
	}
	w.connected = w.ssid != "" && w.signalPct > 0
	return w
}
