//go:build windows

package tui

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	pdhDLL                   = syscall.NewLazyDLL("pdh.dll")
	procPdhOpenQuery         = pdhDLL.NewProc("PdhOpenQueryW")
	procPdhAddEnglishCounter = pdhDLL.NewProc("PdhAddEnglishCounterW")
	procPdhCollectQueryData  = pdhDLL.NewProc("PdhCollectQueryData")
	procPdhGetFormattedValue = pdhDLL.NewProc("PdhGetFormattedCounterValue")
	procPdhGetFormattedArray = pdhDLL.NewProc("PdhGetFormattedCounterArrayW")
	kernel32DLL              = syscall.NewLazyDLL("kernel32.dll")
	procGetSystemPowerStatus = kernel32DLL.NewProc("GetSystemPowerStatus")
)

const (
	pdhFmtDouble = 0x00000200
	pdhMoreData  = 0x800007D2
)

type pdhFmtCounterValue struct {
	CStatus     uint32
	_           uint32
	DoubleValue float64
}

type pdhFmtCounterValueItem struct {
	SzName   *uint16
	FmtValue pdhFmtCounterValue
}

type pdhState struct {
	query    uintptr
	counters map[string]uintptr
	gpu      uintptr
	ok       bool
	inited   bool
}

// pdh is only touched from sample(), which already serializes on regMu.
var pdh pdhState

// init opens one PDH query with English counter names (locale-independent)
// so the tool works on Dutch, German, or any other Windows language.
func (s *pdhState) init() {
	s.inited = true
	s.counters = map[string]uintptr{}
	if r, _, _ := procPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&s.query))); r != 0 {
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
		if r, _, _ := procPdhAddEnglishCounter.Call(s.query, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&h))); r == 0 {
			s.counters[key] = h
		}
	}
	if p, err := windows.UTF16PtrFromString(`\GPU Engine(*)\Utilization Percentage`); err == nil {
		var h uintptr
		if r, _, _ := procPdhAddEnglishCounter.Call(s.query, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&h))); r == 0 {
			s.gpu = h
		}
	}
	// Prime rate counters; real values flow from the second collect on.
	procPdhCollectQueryData.Call(s.query)
	s.ok = true
}

func (s *pdhState) read(key string) float64 {
	h, found := s.counters[key]
	if !found {
		return -1
	}
	var v pdhFmtCounterValue
	if r, _, _ := procPdhGetFormattedValue.Call(h, pdhFmtDouble, 0, uintptr(unsafe.Pointer(&v))); r != 0 {
		return -1
	}
	return v.DoubleValue
}

// readGPU sums utilization per engine type across all GPU engine instances
// and returns the busiest type, which matches Task Manager's headline GPU%.
func (s *pdhState) readGPU() float64 {
	if s.gpu == 0 {
		return -1
	}
	var bufSize, count uint32
	r, _, _ := procPdhGetFormattedArray.Call(s.gpu, pdhFmtDouble,
		uintptr(unsafe.Pointer(&bufSize)), uintptr(unsafe.Pointer(&count)), 0)
	if (r != 0 && r != pdhMoreData) || bufSize == 0 {
		return -1
	}
	buf := make([]uint64, (bufSize+7)/8) // uint64 backing keeps the items 8-byte aligned
	r, _, _ = procPdhGetFormattedArray.Call(s.gpu, pdhFmtDouble,
		uintptr(unsafe.Pointer(&bufSize)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 || count == 0 {
		return -1
	}
	items := unsafe.Slice((*pdhFmtCounterValueItem)(unsafe.Pointer(&buf[0])), count)
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

func readPressure() pressureStat {
	if !pdh.inited {
		pdh.init()
	}
	if !pdh.ok {
		return pressureStat{}
	}
	if r, _, _ := procPdhCollectQueryData.Call(pdh.query); r != 0 {
		return pressureStat{}
	}
	p := pressureStat{ok: true}
	p.cpuQueue = pdh.read("cpuQueue")
	p.cpuPerfPct = pdh.read("cpuPerf")
	p.diskQueue = pdh.read("diskQueue")
	if v := pdh.read("diskLat"); v >= 0 {
		p.diskLatMs = v * 1000
	} else {
		p.diskLatMs = -1
	}
	p.hardFaults = pdh.read("hardFaults")
	p.gpuPct = pdh.readGPU()
	return p
}

type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

func readPower() powerStat {
	var st systemPowerStatus
	if r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&st))); r == 0 {
		return powerStat{}
	}
	p := powerStat{ok: true}
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

// readWifi shells out to netsh (hidden, short timeout). Label matching is
// deliberately loose so it survives localized Windows output.
func readWifi() wifiStat {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "netsh", "wlan", "show", "interfaces")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return wifiStat{}
	}
	w := wifiStat{ok: true}
	for _, line := range strings.Split(string(out), "\n") {
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
