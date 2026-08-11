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
	AutoServices   []string // non-Microsoft services set to automatic, non-delayed start
	BootEvents     []BootEvent // newest first
	HangEvents     []HangEvent // last 14 days, newest first
	PowerPlan      PowerPlan
	PendingReboot  []string
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
