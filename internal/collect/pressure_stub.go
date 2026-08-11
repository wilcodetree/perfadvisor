//go:build !windows

package collect

// Non-Windows stubs; the tool targets Windows, these keep the package
// portable. Mirrors internal/tui/sysmetrics_stub.go.

type pressureTick struct {
	ok         bool
	cpuQueue   float64
	cpuPerfPct float64
	diskQueue  float64
	diskLatMs  float64
	hardFaults float64
	gpuPct     float64
}

type powerTick struct {
	ok         bool
	onAC       bool
	hasBattery bool
	percent    int
	saver      bool
}

type wifiTick struct {
	ok        bool
	connected bool
	ssid      string
	signalPct int
	linkMbps  float64
}

func readPressureTick() pressureTick { return pressureTick{} }
func readPowerTick() powerTick       { return powerTick{} }
func readWifiTick() wifiTick         { return wifiTick{} }
