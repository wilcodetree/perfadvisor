// perfadvisor: a small, portable Windows diagnostic advisor.
// Run without arguments for the live TUI, or use the analyze/export commands.
// All data stays on this device. No network calls, no central collection.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"perfadvisor/internal/collect"
	"perfadvisor/internal/export"
	"perfadvisor/internal/report"
	"perfadvisor/internal/rules"
	"perfadvisor/internal/todo"
	"perfadvisor/internal/tui"
)

const version = "0.6.0"

//go:embed README.md
var readmeMD string

func main() {
	if len(os.Args) < 2 {
		if err := tui.Run(version, readmeMD); err != nil {
			fatal(err)
		}
		return
	}
	switch os.Args[1] {
	case "analyze":
		fs := flag.NewFlagSet("analyze", flag.ExitOnError)
		seconds := fs.Int("seconds", 60, "live sampling duration in seconds")
		out := fs.String("out", "", "output directory (default: the reports folder next to the exe)")
		open := fs.Bool("open", false, "open the HTML report when done")
		_ = fs.Parse(os.Args[2:])
		runAnalyze(*seconds, *out, *open)
	case "export":
		fs := flag.NewFlagSet("export", flag.ExitOnError)
		seconds := fs.Int("seconds", 60, "live sampling duration in seconds")
		out := fs.String("out", "", "output directory (default: the reports folder next to the exe)")
		_ = fs.Parse(os.Args[2:])
		runExport(*seconds, *out)
	case "todo":
		if len(os.Args) < 3 {
			fmt.Println("usage: perfadvisor todo login|logout")
			os.Exit(2)
		}
		switch os.Args[2] {
		case "login":
			if err := todo.Login(os.Stdout); err != nil {
				fatal(err)
			}
			fmt.Println("Signed in. Press 'y' in the dashboard to show today's tasks.")
		case "logout":
			if err := todo.Logout(); err != nil {
				fatal(err)
			}
			fmt.Println("Signed out; cached token removed.")
		default:
			fmt.Println("usage: perfadvisor todo login|logout")
			os.Exit(2)
		}
	case "version", "-v", "--version":
		fmt.Println("perfadvisor " + version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func runAnalyze(seconds int, out string, open bool) {
	snap := collect.Collect(collect.Options{
		SampleSeconds: seconds,
		Progress:      func(msg string) { fmt.Println(msg) },
	})
	findings := rules.Evaluate(snap)
	dir, err := report.OutputDir(out)
	if err != nil {
		fatal(err)
	}
	path, err := report.WriteHTML(snap, findings, dir, version)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("\nHealth score: %d/100, %d finding(s).\n", rules.Score(findings), len(findings))
	fmt.Println("Report written: " + path)
	if open {
		openFile(path)
	}
}

func runExport(seconds int, out string) {
	snap := collect.Collect(collect.Options{
		SampleSeconds: seconds,
		Progress:      func(msg string) { fmt.Println(msg) },
	})
	findings := rules.Evaluate(snap)
	dir, err := report.OutputDir(out)
	if err != nil {
		fatal(err)
	}
	path, err := export.WriteBundle(snap, findings, dir, version)
	if err != nil {
		fatal(err)
	}
	fmt.Println("Diagnostic bundle written: " + path)
	fmt.Println("Paste its contents into Claude for a tailored second opinion.")
}

func openFile(path string) {
	if runtime.GOOS == "windows" {
		_ = exec.Command("cmd", "/c", "start", "", path).Start()
	}
}

func usage() {
	fmt.Print(`perfadvisor ` + version + `, a small portable Windows performance advisor.

Usage:
  perfadvisor              start the live monitor (TUI)
  perfadvisor analyze      sample the system, mine Windows history, write an HTML report
      -seconds N           sampling duration (default 60)
      -out DIR             output directory (default: the reports folder next to the exe)
      -open                open the report when done
  perfadvisor export       write a markdown diagnostic bundle to paste into Claude
      -seconds N, -out DIR
  perfadvisor todo login   sign in to Microsoft To Do (device code, one-time);
                           'y' in the dashboard then shows tasks due today.
                           This is the only feature that touches the network.
  perfadvisor todo logout  remove the cached sign-in token
  perfadvisor version      print the version

Inside the TUI: q quit, i info screen, c/m/d sort by cpu/memory/disk, t process tree,
p per-core history, w wifi window (18 min / 6 h), j/k scroll, a run analysis,
e export bundle, o open last
report. For fullscreen use the terminal itself (F11 in Windows Terminal).

All data stays on this device. Not all sources are readable as a standard user;
the report lists anything that could not be checked. Running elevated unlocks
deeper boot analysis.
`)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "perfadvisor: "+err.Error())
	os.Exit(1)
}
