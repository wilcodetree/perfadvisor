package report

import (
	_ "embed"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"

	"perfadvisor/internal/collect"
	"perfadvisor/internal/rules"
)

//go:embed template.html
var tpl string

type viewData struct {
	Snap      *collect.Snapshot
	Findings  []rules.Finding
	Score     int
	Version   string
	Generated string
	High      int
	Medium    int
	InfoCount int
}

// WriteHTML renders the self-contained report and returns its path.
func WriteHTML(snap *collect.Snapshot, findings []rules.Finding, dir, version string) (string, error) {
	funcs := template.FuncMap{
		"f0":  func(v float64) string { return fmt.Sprintf("%.0f", v) },
		"f1":  func(v float64) string { return fmt.Sprintf("%.1f", v) },
		"gb":  func(mb float64) string { return fmt.Sprintf("%.1f", mb/1024) },
		"sec": func(ms int64) string { return fmt.Sprintf("%.1f", float64(ms)/1000) },
		"sev": func(s rules.Severity) string { return strings.ToLower(s.String()) },
		"dt": func(t time.Time) string {
			if t.IsZero() {
				return "unknown"
			}
			return t.Format("2006-01-02 15:04")
		},
		"days": func(d time.Duration) string { return fmt.Sprintf("%.1f", d.Hours()/24) },
		"clamp": func(v float64) float64 {
			if v < 0 {
				return 0
			}
			if v > 100 {
				return 100
			}
			return v
		},
	}
	t, err := template.New("report").Funcs(funcs).Parse(tpl)
	if err != nil {
		return "", err
	}
	data := viewData{
		Snap: snap, Findings: findings, Score: rules.Score(findings),
		Version: version, Generated: snap.TakenAt.Format("2006-01-02 15:04:05"),
	}
	for _, f := range findings {
		switch f.Severity {
		case rules.High:
			data.High++
		case rules.Medium:
			data.Medium++
		default:
			data.InfoCount++
		}
	}
	path := filepath.Join(dir, "perfadvisor-report-"+snap.TakenAt.Format("20060102-150405")+".html")
	fh, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	if err := t.Execute(fh, data); err != nil {
		return "", err
	}
	return path, nil
}

// OutputDir picks where output lands: an explicit dir, else a reports
// folder next to the exe if writable, else %LOCALAPPDATA%\perfadvisor\reports.
func OutputDir(override string) (string, error) {
	if override != "" {
		if err := os.MkdirAll(override, 0o755); err != nil {
			return "", err
		}
		return override, nil
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "reports")
		if err := os.MkdirAll(dir, 0o755); err == nil && writable(dir) {
			return dir, nil
		}
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = home
		}
	}
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "perfadvisor", "reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".perfadvisor-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}
