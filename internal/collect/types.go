package collect

import "time"

// ProcSample is one process measured over the sampling window.
// CPUPercent is relative to one core and can exceed 100 on multicore machines.
type ProcSample struct {
	PID        int32
	Name       string
	CPUPercent float64
	MemoryMB   float64
}

type MemInfo struct {
	TotalMB     float64
	UsedMB      float64
	UsedPercent float64
}

type DiskInfo struct {
	Mount       string
	TotalGB     float64
	FreeGB      float64
	UsedPercent float64
	System      bool
}

// Autorun is one thing that starts at logon.
type Autorun struct {
	Name         string
	Command      string
	Source       string
	Enabled      bool
	EnabledKnown bool // false when Windows has no StartupApproved entry for it
}

// BootEvent is one entry from the Diagnostics-Performance log.
// EventID 100 carries the total boot duration; 101 to 110 name a culprit.
type BootEvent struct {
	Time          time.Time
	EventID       int
	BootMs        int64
	App           string
	Path          string
	DegradationMs int64
}

type HangEvent struct {
	Time time.Time
	App  string
}

type PowerPlan struct {
	Name string
	Raw  string
}

// Unchecked records a data source this run could not read, and why.
type Unchecked struct {
	What string
	Why  string
}

// PressureInfo aggregates the saturation counters (the same PDH counters the
// live TUI reads) over the whole sampling window: the average and the worst
// single tick. A zero-value GPU/CPUPerf average means that counter never
// reported a usable value, not that it was zero.
type PressureInfo struct {
	OK bool

	CPUQueueAvg    float64 // runnable threads waiting, System\Processor Queue Length
	CPUQueuePeak   float64
	CPUPerfPctAvg  float64 // % of base clock; <100 = throttled, >100 = turbo
	CPUPerfPctMin  float64
	DiskLatMsAvg   float64 // avg disk sec/transfer, in ms
	DiskLatMsPeak  float64
	DiskQueueAvg   float64
	DiskQueuePeak  float64
	HardFaultsAvg  float64 // memory pages read from disk per second
	HardFaultsPeak float64
	GPUPctAvg      float64 // busiest GPU engine type, Task Manager style
	GPUPctPeak     float64
	Ticks          int // number of PDH samples this average is built from
}

// CoreInfo captures per-core load spread, to tell "everything is loaded"
// apart from "one thread is pegged and the rest idle."
type CoreInfo struct {
	NCores      int
	MaxCoreAvg  float64
	MinCoreAvg  float64
	MaxCoreName string // "C<n>" of the busiest core
}

// WifiInfo aggregates signal and link rate over the sampling window, plus
// disconnect events. OK is false when the machine has no wifi adapter or
// netsh could not be read at all; Samples is 0 when wifi was never
// connected during the window (ethernet, or disconnected throughout).
type WifiInfo struct {
	OK           bool
	Connected    bool // connected at the end of the sample
	SSID         string
	SignalPctAvg float64
	SignalPctMin int
	LinkMbpsAvg  float64
	Drops        int // times signal fell from connected to disconnected
	Samples      int
}

// PowerInfo is the battery/AC state, read at the start and end of the
// sample so a run started on battery and finished on AC (or the reverse)
// is still visible. BatteryPct fields are -1 when unknown.
type PowerInfo struct {
	OK              bool
	HasBattery      bool
	OnACStart       bool
	OnACEnd         bool
	BatteryPctStart int
	BatteryPctEnd   int
	SaverActive     bool
}

type Snapshot struct {
	TakenAt        time.Time
	Hostname       string
	OSVersion      string
	Elevated       bool
	Uptime         time.Duration
	BootTime       time.Time
	CPUModel       string
	LogicalCores   int
	SampleSeconds  int
	CPUAvgPercent  float64
	CPUPeakPercent float64
	Memory         MemInfo
	Disks          []DiskInfo
	TopCPU         []ProcSample
	TopMem         []ProcSample
	Autoruns       []Autorun
	AutoServices   []string    // non-Microsoft services set to automatic, non-delayed start
	BootEvents     []BootEvent // newest first
	HangEvents     []HangEvent // last 14 days, newest first
	PowerPlan      PowerPlan
	PendingReboot  []string
	Pressure       PressureInfo
	Cores          CoreInfo
	Wifi           WifiInfo
	Power          PowerInfo
	Unchecked      []Unchecked
}

func (s *Snapshot) Note(what, why string) {
	s.Unchecked = append(s.Unchecked, Unchecked{What: what, Why: why})
}

// EnabledAutoruns returns autoruns that will actually run at logon.
func (s *Snapshot) EnabledAutoruns() []Autorun {
	var out []Autorun
	for _, a := range s.Autoruns {
		if a.Enabled {
			out = append(out, a)
		}
	}
	return out
}

func (s *Snapshot) SystemDisk() *DiskInfo {
	for i := range s.Disks {
		if s.Disks[i].System {
			return &s.Disks[i]
		}
	}
	return nil
}

// LatestBootMs returns the most recent measured boot duration in ms, or 0.
func (s *Snapshot) LatestBootMs() int64 {
	for _, e := range s.BootEvents {
		if e.EventID == 100 && e.BootMs > 0 {
			return e.BootMs
		}
	}
	return 0
}
