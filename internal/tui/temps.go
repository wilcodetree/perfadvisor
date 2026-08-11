package tui

import (
	"strings"

	"github.com/shirou/gopsutil/v4/sensors"
)

type tempStat struct {
	label string
	c     float64
}

// readTemps is best-effort: on many Windows machines the ACPI thermal zone
// is unreadable without elevation, in which case we simply show nothing.
func readTemps() []tempStat {
	ts, err := sensors.SensorsTemperatures()
	if err != nil || len(ts) == 0 {
		return nil
	}
	var out []tempStat
	for _, t := range ts {
		if t.Temperature <= 0 || t.Temperature > 120 {
			continue
		}
		label := t.SensorKey
		if i := strings.LastIndexAny(label, "\\/"); i >= 0 {
			label = label[i+1:]
		}
		out = append(out, tempStat{label: label, c: t.Temperature})
		if len(out) >= 6 {
			break
		}
	}
	return out
}
