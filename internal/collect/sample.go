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

	progress(fmt.Sprintf("Sampling the system for %d seconds...", opts.SampleSeconds))
	var sum, peak float64
	ticks := 0
	for i := 0; i < opts.SampleSeconds; i++ {
		pcts, err := cpu.Percent(time.Second, false)
		if err == nil && len(pcts) > 0 {
			sum += pcts[0]
			if pcts[0] > peak {
				peak = pcts[0]
			}
			ticks++
		}
		if (i+1)%15 == 0 && i+1 < opts.SampleSeconds {
			progress(fmt.Sprintf("  ...%d of %d seconds", i+1, opts.SampleSeconds))
		}
	}
	if ticks > 0 {
		s.CPUAvgPercent = sum / float64(ticks)
		s.CPUPeakPercent = peak
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
