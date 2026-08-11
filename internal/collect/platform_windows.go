//go:build windows

package collect

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func collectPlatform(s *Snapshot) {
	s.Elevated = windows.GetCurrentProcessToken().IsElevated()
	collectAutoruns(s)
	collectBootEvents(s)
	collectHangEvents(s)
	collectPowerPlan(s)
	collectPendingReboot(s)
	collectAutoServices(s)
}

// runHidden runs a console tool without flashing a window, with a timeout.
func runHidden(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func shortErr(err error, out string) string {
	msg := strings.TrimSpace(out)
	if msg == "" && err != nil {
		msg = err.Error()
	}
	if len(msg) > 160 {
		msg = msg[:160] + "..."
	}
	return strings.ReplaceAll(strings.ReplaceAll(msg, "\r\n", " "), "\n", " ")
}
