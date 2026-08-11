//go:build windows

package collect

import (
	"encoding/xml"
	"strconv"
	"strings"
	"time"
)

type xmlEventList struct {
	Events []xmlEvent `xml:"Event"`
}

type xmlEvent struct {
	System struct {
		Provider struct {
			Name string `xml:"Name,attr"`
		} `xml:"Provider"`
		EventID     string `xml:"EventID"`
		TimeCreated struct {
			SystemTime string `xml:"SystemTime,attr"`
		} `xml:"TimeCreated"`
	} `xml:"System"`
	EventData struct {
		Data []struct {
			Name  string `xml:"Name,attr"`
			Value string `xml:",chardata"`
		} `xml:"Data"`
	} `xml:"EventData"`
}

// parseEvents wraps wevtutil's rootless XML output and unmarshals it.
func parseEvents(raw string) []xmlEvent {
	var list xmlEventList
	if err := xml.Unmarshal([]byte("<Events>"+raw+"</Events>"), &list); err != nil {
		return nil
	}
	return list.Events
}

func (e xmlEvent) id() int {
	n, _ := strconv.Atoi(strings.TrimSpace(e.System.EventID))
	return n
}

func (e xmlEvent) when() time.Time {
	t, err := time.Parse(time.RFC3339Nano, e.System.TimeCreated.SystemTime)
	if err != nil {
		return time.Time{}
	}
	return t.Local()
}

func (e xmlEvent) data(names ...string) string {
	for _, want := range names {
		for _, d := range e.EventData.Data {
			if strings.EqualFold(d.Name, want) && strings.TrimSpace(d.Value) != "" {
				return strings.TrimSpace(d.Value)
			}
		}
	}
	return ""
}

func (e xmlEvent) firstData() string {
	for _, d := range e.EventData.Data {
		if v := strings.TrimSpace(d.Value); v != "" {
			return v
		}
	}
	return ""
}

// collectBootEvents mines the Diagnostics-Performance log: event 100 carries
// the measured boot duration, 101 to 110 name what degraded the boot.
// Reading this log usually needs elevation; failure lands in Unchecked.
func collectBootEvents(s *Snapshot) {
	out, err := runHidden("wevtutil", "qe",
		"Microsoft-Windows-Diagnostics-Performance/Operational",
		"/q:*[System[(EventID >= 100) and (EventID <= 110)]]",
		"/c:60", "/rd:true", "/f:xml")
	if err != nil {
		s.Note("Boot degradation events (Diagnostics-Performance log)",
			shortErr(err, out)+". Running perfadvisor elevated usually unlocks this.")
		return
	}
	for _, e := range parseEvents(out) {
		be := BootEvent{Time: e.when(), EventID: e.id()}
		if be.EventID == 100 {
			be.BootMs = atoi64(e.data("BootTime"))
		} else {
			be.App = e.data("ApplicationName", "Name", "DriverFileName", "ServiceName", "File")
			be.Path = e.data("ApplicationPath", "Path")
			be.DegradationMs = atoi64(e.data("DegradationTime", "Degradation", "TotalTime"))
		}
		s.BootEvents = append(s.BootEvents, be)
	}
}

// collectHangEvents reads Application Hang (1002) events, readable without admin.
func collectHangEvents(s *Snapshot) {
	out, err := runHidden("wevtutil", "qe", "Application",
		"/q:*[System[Provider[@Name='Application Hang'] and (EventID=1002)]]",
		"/c:120", "/rd:true", "/f:xml")
	if err != nil {
		s.Note("Application hang events", shortErr(err, out))
		return
	}
	cutoff := time.Now().AddDate(0, 0, -14)
	for _, e := range parseEvents(out) {
		t := e.when()
		if t.Before(cutoff) {
			continue
		}
		if app := e.firstData(); app != "" {
			s.HangEvents = append(s.HangEvents, HangEvent{Time: t, App: app})
		}
	}
}

func atoi64(v string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	return n
}
