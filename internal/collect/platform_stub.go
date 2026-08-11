//go:build !windows

package collect

// collectPlatform on non-Windows platforms records what is Windows-only.
// The tool targets Windows; this stub keeps the package portable for tests.
func collectPlatform(s *Snapshot) {
	for _, what := range []string{
		"Autorun inventory", "Boot degradation events", "Application hang events",
		"Power plan", "Pending reboot", "Automatic services",
	} {
		s.Note(what, "only available on Windows")
	}
}
