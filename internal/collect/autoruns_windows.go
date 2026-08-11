//go:build windows

package collect

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const cv = `Software\Microsoft\Windows\CurrentVersion`

func collectAutoruns(s *Snapshot) {
	type regSrc struct {
		root     registry.Key
		path     string
		label    string
		approved string
	}
	sources := []regSrc{
		{registry.CURRENT_USER, cv + `\Run`, "HKCU Run", cv + `\Explorer\StartupApproved\Run`},
		{registry.CURRENT_USER, cv + `\RunOnce`, "HKCU RunOnce", ""},
		{registry.LOCAL_MACHINE, cv + `\Run`, "HKLM Run", cv + `\Explorer\StartupApproved\Run`},
		{registry.LOCAL_MACHINE, cv + `\RunOnce`, "HKLM RunOnce", ""},
		{registry.LOCAL_MACHINE, `Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Run`, "HKLM Run (32-bit)", cv + `\Explorer\StartupApproved\Run32`},
	}
	opened := false
	for _, src := range sources {
		k, err := registry.OpenKey(src.root, src.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		opened = true
		names, err := k.ReadValueNames(-1)
		if err != nil {
			k.Close()
			continue
		}
		approved := readApproved(src.root, src.approved)
		for _, name := range names {
			cmd, _, err := k.GetStringValue(name)
			if err != nil {
				continue
			}
			enabled, known := approvedState(approved, name)
			s.Autoruns = append(s.Autoruns, Autorun{
				Name: name, Command: cmd, Source: src.label,
				Enabled: enabled, EnabledKnown: known,
			})
		}
		k.Close()
	}
	if !opened {
		s.Note("Autorun registry keys", "could not open any Run key")
	}

	folders := []struct {
		dir, label, approved string
		root                 registry.Key
	}{
		{filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs\Startup`),
			"Startup folder (user)", cv + `\Explorer\StartupApproved\StartupFolder`, registry.CURRENT_USER},
		{filepath.Join(os.Getenv("ProgramData"), `Microsoft\Windows\Start Menu\Programs\Startup`),
			"Startup folder (all users)", cv + `\Explorer\StartupApproved\StartupFolder`, registry.LOCAL_MACHINE},
	}
	for _, f := range folders {
		entries, err := os.ReadDir(f.dir)
		if err != nil {
			continue
		}
		approved := readApproved(f.root, f.approved)
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || strings.EqualFold(name, "desktop.ini") {
				continue
			}
			enabled, known := approvedState(approved, name)
			s.Autoruns = append(s.Autoruns, Autorun{
				Name: name, Command: filepath.Join(f.dir, name), Source: f.label,
				Enabled: enabled, EnabledKnown: known,
			})
		}
	}
}

// readApproved loads Windows' own enabled/disabled bookkeeping for autoruns,
// the same data Task Manager's Startup tab toggles.
func readApproved(root registry.Key, path string) map[string][]byte {
	out := map[string][]byte{}
	if path == "" {
		return out
	}
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return out
	}
	defer k.Close()
	names, err := k.ReadValueNames(-1)
	if err != nil {
		return out
	}
	for _, n := range names {
		if data, _, err := k.GetBinaryValue(n); err == nil {
			out[strings.ToLower(n)] = data
		}
	}
	return out
}

// approvedState: first byte 0x02 means enabled, 0x03 disabled.
// No entry means Windows never toggled it, which defaults to enabled.
func approvedState(approved map[string][]byte, name string) (enabled, known bool) {
	data, ok := approved[strings.ToLower(name)]
	if !ok || len(data) == 0 {
		return true, false
	}
	return data[0] == 2, true
}
