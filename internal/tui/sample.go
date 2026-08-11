package tui

import (
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

type procRow struct {
	pid  int32
	ppid int32
	name string
	cpu  float64
	mem  float64 // MB
	disk float64 // read+write bytes/s over the tick window
}

type diskStat struct {
	mount    string
	system   bool
	usedPct  float64
	freeGB   float64
	totalGB  float64
	readBps  float64
	writeBps float64
}

type netStat struct {
	downBps, upBps     float64
	downTotal, upTotal uint64
}

// pressureStat holds saturation signals from the PDH performance counters.
// Values are -1 when the counter is unavailable this tick.
type pressureStat struct {
	cpuQueue   float64 // runnable threads waiting (System\Processor Queue Length)
	cpuPerfPct float64 // % of base clock; <100 = throttled, >100 = turbo
	diskQueue  float64
	diskLatMs  float64 // avg disk sec/transfer, in ms
	hardFaults float64 // memory pages read from disk per second
	gpuPct     float64 // busiest GPU engine type, Task Manager style
	ok         bool
}

type powerStat struct {
	hasBattery bool
	onAC       bool
	percent    int // -1 unknown
	saver      bool
	ok         bool
}

type wifiStat struct {
	connected bool
	ssid      string
	signalPct int
	linkMbps  float64
	ok        bool
}

type metricsMsg struct {
	cpuPct     float64
	cores      []float64
	memPct     float64
	memUsedGB  float64
	memTotalGB float64
	memAvailGB float64
	swapPct    float64
	swapUsedGB float64
	swapTotGB  float64
	disks      []diskStat
	net        netStat
	temps      []tempStat
	pressure   pressureStat
	power      powerStat
	wifi       wifiStat
	rows       []procRow
	nProcs     int
}

type trackedProc struct {
	p         *process.Process
	name      string
	ppid      int32
	lastRead  uint64
	lastWrite uint64
	hasIO     bool
}

// Persistent state between ticks, all touched only from sample() under regMu.
var (
	regMu    sync.Mutex
	procReg  = map[int32]*trackedProc{}
	prevNet  *gnet.IOCountersStat
	prevIO   map[string]disk.IOCountersStat
	prevAt   time.Time
	tickN    int
	lastTemp []tempStat
	lastWifi wifiStat
)

func sample() tea.Msg {
	regMu.Lock()
	defer regMu.Unlock()

	var msg metricsMsg
	now := time.Now()
	elapsed := now.Sub(prevAt).Seconds()
	if elapsed <= 0 || prevAt.IsZero() {
		elapsed = 0
	}

	if v, err := cpu.Percent(0, false); err == nil && len(v) > 0 {
		msg.cpuPct = v[0]
	}
	if v, err := cpu.Percent(0, true); err == nil {
		msg.cores = v
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		msg.memPct = vm.UsedPercent
		msg.memUsedGB = float64(vm.Used) / (1 << 30)
		msg.memTotalGB = float64(vm.Total) / (1 << 30)
		msg.memAvailGB = float64(vm.Available) / (1 << 30)
	}
	if sw, err := mem.SwapMemory(); err == nil && sw.Total > 0 {
		msg.swapPct = sw.UsedPercent
		msg.swapUsedGB = float64(sw.Used) / (1 << 30)
		msg.swapTotGB = float64(sw.Total) / (1 << 30)
	}

	// Disk usage plus IO rates (delta against the previous tick).
	io, _ := disk.IOCounters()
	sysDrive := strings.ToUpper(os.Getenv("SystemDrive"))
	if parts, err := disk.Partitions(false); err == nil {
		for _, part := range parts {
			u, err := disk.Usage(part.Mountpoint)
			if err != nil || u.Total == 0 {
				continue
			}
			key := strings.ToUpper(strings.TrimRight(part.Mountpoint, `\/`))
			d := diskStat{
				mount:   key,
				system:  key == sysDrive,
				usedPct: u.UsedPercent,
				freeGB:  float64(u.Free) / (1 << 30),
				totalGB: float64(u.Total) / (1 << 30),
			}
			if elapsed > 0 && prevIO != nil {
				for name, cur := range io {
					if !strings.EqualFold(strings.TrimRight(name, `\/`), key) {
						continue
					}
					if old, ok := prevIO[name]; ok {
						d.readBps = float64(cur.ReadBytes-old.ReadBytes) / elapsed
						d.writeBps = float64(cur.WriteBytes-old.WriteBytes) / elapsed
					}
				}
			}
			msg.disks = append(msg.disks, d)
		}
	}
	prevIO = io

	if nics, err := gnet.IOCounters(false); err == nil && len(nics) > 0 {
		cur := nics[0]
		msg.net.downTotal = cur.BytesRecv
		msg.net.upTotal = cur.BytesSent
		if prevNet != nil && elapsed > 0 {
			msg.net.downBps = float64(cur.BytesRecv-prevNet.BytesRecv) / elapsed
			msg.net.upBps = float64(cur.BytesSent-prevNet.BytesSent) / elapsed
		}
		prevNet = &cur
	}
	prevAt = now

	// Saturation counters and power state, every tick (cheap, in-process).
	msg.pressure = readPressure()
	msg.power = readPower()

	// Temperatures (WMI) and WiFi (netsh) every 4th tick, about every 6 s.
	if tickN%4 == 0 {
		lastTemp = readTemps()
		lastWifi = readWifi()
	}
	tickN++
	msg.temps = lastTemp
	msg.wifi = lastWifi

	// Processes: reuse handles so CPU% and IO deltas span the tick window.
	if procs, err := process.Processes(); err == nil {
		seen := map[int32]bool{}
		for _, p := range procs {
			seen[p.Pid] = true
			if _, ok := procReg[p.Pid]; !ok {
				name, err := p.Name()
				if err != nil || name == "" {
					continue
				}
				tp := &trackedProc{p: p, name: name}
				if pp, err := p.Ppid(); err == nil {
					tp.ppid = pp
				}
				_, _ = p.Percent(0) // prime the CPU delta
				if ioc, err := p.IOCounters(); err == nil && ioc != nil {
					tp.lastRead, tp.lastWrite, tp.hasIO = ioc.ReadBytes, ioc.WriteBytes, true
				}
				procReg[p.Pid] = tp
			}
		}
		for pid := range procReg {
			if !seen[pid] {
				delete(procReg, pid)
			}
		}
		for pid, tp := range procReg {
			if pid == 0 {
				continue
			}
			pct, err := tp.p.Percent(0)
			if err != nil {
				continue
			}
			r := procRow{pid: pid, ppid: tp.ppid, name: tp.name, cpu: pct}
			if mi, err := tp.p.MemoryInfo(); err == nil && mi != nil {
				r.mem = float64(mi.RSS) / (1 << 20)
			}
			if tp.hasIO && elapsed > 0 {
				if ioc, err := tp.p.IOCounters(); err == nil && ioc != nil {
					r.disk = (float64(ioc.ReadBytes-tp.lastRead) + float64(ioc.WriteBytes-tp.lastWrite)) / elapsed
					tp.lastRead, tp.lastWrite = ioc.ReadBytes, ioc.WriteBytes
				}
			}
			msg.rows = append(msg.rows, r)
		}
		msg.nProcs = len(msg.rows)
	}
	return msg
}
