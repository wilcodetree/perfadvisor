package collect

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

type Options struct {
	SampleSeconds int
	Progress      func(msg string)
}

// Collect samples the live system for SampleSeconds and mines what Windows
// already logged. Best-effort: anything unreadable lands in Snapshot.Unchecked.
func Collect(opts Options) *Snapshot {
	if opts.SampleSeconds <= 0 {
		opts.SampleSeconds = 60
	}
	progress := opts.Progress
	if progress == nil {
		progress = func(string) {}
	}
	s := &Snapshot{TakenAt: time.Now(), SampleSeconds: opts.SampleSeconds}

	if hi, err := host.Info(); err == nil {
		s.Hostname = hi.Hostname
		s.OSVersion = strings.TrimSpace(hi.Platform + " " + hi.PlatformVersion)
		s.Uptime = time.Duration(hi.Uptime) * time.Second
		s.BootTime = time.Unix(int64(hi.BootTime), 0)
	} else {
		s.Note("Host info", err.Error())
	}
	if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
		s.CPUModel = strings.TrimSpace(infos[0].ModelName)
	}
	if n, err := cpu.Counts(true); err == nil {
		s.LogicalCores = n
	}

	// Prime per-process CPU counters so the second read spans the window.
	procs, procErr := process.Processes()
	if procErr != nil {
		s.Note("Process list", procErr.Error())
	}
	for _, p := range procs {
		_, _ = p.Percent(0)
	}
	// Prime the per-core delta counter (interval 0) the same way; the first
	// real per-core read happens on tick 0 of the loop below. Total CPU% is
	// sampled separately with a blocking one-second call each tick, so the
	// two do not share state and do not interfere with each other.
	_, _ = cpu.Percent(0, true)

	pwrStart := readPowerTick()

	progress(fmt.Sprintf("Sampling the system for %d seconds...", opts.SampleSeconds))
	var sum, peak float64
	ticks := 0

	var coreSums []float64
	coreTicks := 0

	pTicks := 0
	var qSum, qPeak float64
	var perfSum, perfMin float64
	perfMin = -1
	var latSum, latPeak float64
	var dqSum, dqPeak float64
	var hfSum, hfPeak float64
	var gpuSum, gpuPeak float64

	wifiSamples := 0
	var wifiSigSum float64
	wifiSigMin := 0
	var wifiLinkSum float64
	wifiDrops := 0
	wifiConnected := false
	wifiSSID := ""
	prevWifiConnected := false
	sawWifi := false
	firstWifiTick := true

	for i := 0; i < opts.SampleSeconds; i++ {
		pcts, err := cpu.Percent(time.Second, false)
		if err == nil && len(pcts) > 0 {
			sum += pcts[0]
			if pcts[0] > peak {
				peak = pcts[0]
			}
			ticks++
		}
		if cores, err := cpu.Percent(0, true); err == nil && len(cores) > 0 {
			if len(coreSums) != len(cores) {
				coreSums = make([]float64, len(cores))
			}
			for ci, v := range cores {
				coreSums[ci] += v
			}
			coreTicks++
		}

		pt := readPressureTick()
		if pt.ok {
			pTicks++
			if pt.cpuQueue >= 0 {
				qSum += pt.cpuQueue
				if pt.cpuQueue > qPeak {
					qPeak = pt.cpuQueue
				}
			}
			if pt.cpuPerfPct >= 0 {
				perfSum += pt.cpuPerfPct
				if perfMin < 0 || pt.cpuPerfPct < perfMin {
					perfMin = pt.cpuPerfPct
				}
			}
			if pt.diskLatMs >= 0 {
				latSum += pt.diskLatMs
				if pt.diskLatMs > latPeak {
					latPeak = pt.diskLatMs
				}
			}
			if pt.diskQueue >= 0 {
				dqSum += pt.diskQueue
				if pt.diskQueue > dqPeak {
					dqPeak = pt.diskQueue
				}
			}
			if pt.hardFaults >= 0 {
				hfSum += pt.hardFaults
				if pt.hardFaults > hfPeak {
					hfPeak = pt.hardFaults
				}
			}
			if pt.gpuPct >= 0 {
				gpuSum += pt.gpuPct
				if pt.gpuPct > gpuPeak {
					gpuPeak = pt.gpuPct
				}
			}
		}

		// Wifi every 4th tick; netsh is comparatively slow and the signal
		// does not change meaningfully faster than that.
		if i%4 == 0 {
			wt := readWifiTick()
			if wt.ok {
				sawWifi = true
				if prevWifiConnected && !wt.connected && !firstWifiTick {
					wifiDrops++
				}
				prevWifiConnected = wt.connected
				firstWifiTick = false
				wifiConnected = wt.connected
				if wt.connected {
					wifiSSID = wt.ssid
					wifiSamples++
					wifiSigSum += float64(wt.signalPct)
					if wifiSamples == 1 || wt.signalPct < wifiSigMin {
						wifiSigMin = wt.signalPct
					}
					wifiLinkSum += wt.linkMbps
				}
			}
		}

		if (i+1)%15 == 0 && i+1 < opts.SampleSeconds {
			progress(fmt.Sprintf("  ...%d of %d seconds", i+1, opts.SampleSeconds))
		}
	}
	if ticks > 0 {
		s.CPUAvgPercent = sum / float64(ticks)
		s.CPUPeakPercent = peak
	}

	pwrEnd := readPowerTick()
	if pwrStart.ok || pwrEnd.ok {
		s.Power = PowerInfo{
			OK:              true,
			HasBattery:      pwrStart.hasBattery || pwrEnd.hasBattery,
			OnACStart:       pwrStart.onAC,
			OnACEnd:         pwrEnd.onAC,
			BatteryPctStart: pwrStart.percent,
			BatteryPctEnd:   pwrEnd.percent,
			SaverActive:     pwrEnd.saver,
		}
		if !pwrStart.ok {
			s.Power.BatteryPctStart = -1
		}
		if !pwrEnd.ok {
			s.Power.BatteryPctEnd = -1
		}
	}

	if coreTicks > 0 && len(coreSums) > 0 {
		ci := CoreInfo{NCores: len(coreSums)}
		maxAvg, minAvg := -1.0, -1.0
		maxIdx := 0
		for idx, tot := range coreSums {
			avg := tot / float64(coreTicks)
			if avg > maxAvg {
				maxAvg, maxIdx = avg, idx
			}
			if minAvg < 0 || avg < minAvg {
				minAvg = avg
			}
		}
		ci.MaxCoreAvg = maxAvg
		ci.MinCoreAvg = minAvg
		ci.MaxCoreName = fmt.Sprintf("C%d", maxIdx)
		s.Cores = ci
	}

	if pTicks > 0 {
		s.Pressure = PressureInfo{
			OK: true, Ticks: pTicks,
			CPUQueueAvg: qSum / float64(pTicks), CPUQueuePeak: qPeak,
			DiskLatMsAvg: latSum / float64(pTicks), DiskLatMsPeak: latPeak,
			DiskQueueAvg: dqSum / float64(pTicks), DiskQueuePeak: dqPeak,
			HardFaultsAvg: hfSum / float64(pTicks), HardFaultsPeak: hfPeak,
		}
		if perfSum > 0 {
			s.Pressure.CPUPerfPctAvg = perfSum / float64(pTicks)
			s.Pressure.CPUPerfPctMin = perfMin
		}
		if gpuSum > 0 {
			s.Pressure.GPUPctAvg = gpuSum / float64(pTicks)
			s.Pressure.GPUPctPeak = gpuPeak
		}
	} else {
		s.Note("Pressure counters", "PDH counters unavailable on this system")
	}

	if sawWifi {
		s.Wifi = WifiInfo{OK: true, Connected: wifiConnected, SSID: wifiSSID, Drops: wifiDrops, Samples: wifiSamples}
		if wifiSamples > 0 {
			s.Wifi.SignalPctAvg = wifiSigSum / float64(wifiSamples)
			s.Wifi.LinkMbpsAvg = wifiLinkSum / float64(wifiSamples)
			s.Wifi.SignalPctMin = wifiSigMin
		}
	}

	var samples []ProcSample
	for _, p := range procs {
		pct, err := p.Percent(0)
		if err != nil {
			continue
		}
		name, err := p.Name()
		if err != nil || name == "" || p.Pid == 0 {
			continue
		}
		ps := ProcSample{PID: p.Pid, Name: name, CPUPercent: pct}
		if mi, err := p.MemoryInfo(); err == nil && mi != nil {
			ps.MemoryMB = float64(mi.RSS) / (1024 * 1024)
		}
		samples = append(samples, ps)
	}
	s.TopCPU = topBy(samples, 8, func(a, b ProcSample) bool { return a.CPUPercent > b.CPUPercent })
	s.TopMem = topBy(samples, 8, func(a, b ProcSample) bool { return a.MemoryMB > b.MemoryMB })

	if vm, err := mem.VirtualMemory(); err == nil {
		s.Memory = MemInfo{
			TotalMB:     float64(vm.Total) / (1024 * 1024),
			UsedMB:      float64(vm.Used) / (1024 * 1024),
			UsedPercent: vm.UsedPercent,
		}
	} else {
		s.Note("Memory", err.Error())
	}

	sysDrive := strings.ToUpper(os.Getenv("SystemDrive"))
	if parts, err := disk.Partitions(false); err == nil {
		for _, part := range parts {
			u, err := disk.Usage(part.Mountpoint)
			if err != nil || u.Total == 0 {
				continue
			}
			d := DiskInfo{
				Mount:       part.Mountpoint,
				TotalGB:     float64(u.Total) / (1024 * 1024 * 1024),
				FreeGB:      float64(u.Free) / (1024 * 1024 * 1024),
				UsedPercent: u.UsedPercent,
			}
			m := strings.ToUpper(strings.TrimRight(part.Mountpoint, `\/`))
			d.System = (sysDrive != "" && m == sysDrive) || (runtime.GOOS != "windows" && part.Mountpoint == "/")
			s.Disks = append(s.Disks, d)
		}
	} else {
		s.Note("Disks", err.Error())
	}

	progress("Reading startup and event history...")
	collectPlatform(s)
	return s
}

func topBy(in []ProcSample, n int, more func(a, b ProcSample) bool) []ProcSample {
	out := make([]ProcSample, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool { return more(out[i], out[j]) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}
