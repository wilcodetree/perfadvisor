//go:build !windows

package tui

// Non-Windows stubs; the tool targets Windows, these keep the package portable.

func readPressure() pressureStat { return pressureStat{} }
func readPower() powerStat       { return powerStat{} }
func readWifi() wifiStat         { return wifiStat{} }
