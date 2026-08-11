//go:build windows

package collect

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

func collectPowerPlan(s *Snapshot) {
	out, err := runHidden("powercfg", "/getactivescheme")
	if err != nil {
		s.Note("Power plan", shortErr(err, out))
		return
	}
	raw := strings.TrimSpace(out)
	s.PowerPlan.Raw = raw
	if i := strings.Index(raw, "("); i >= 0 {
		if j := strings.LastIndex(raw, ")"); j > i {
			s.PowerPlan.Name = raw[i+1 : j]
		}
	}
}

func collectPendingReboot(s *Snapshot) {
	checks := []struct{ path, why string }{
		{`SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending`,
			"Windows servicing has a pending reboot"},
		{`SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired`,
			"Windows Update requires a reboot"},
	}
	for _, c := range checks {
		if k, err := registry.OpenKey(registry.LOCAL_MACHINE, c.path, registry.QUERY_VALUE); err == nil {
			k.Close()
			s.PendingReboot = append(s.PendingReboot, c.why)
		}
	}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Session Manager`, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetStringsValue("PendingFileRenameOperations"); err == nil && len(v) > 0 {
			s.PendingReboot = append(s.PendingReboot, "pending file rename operations")
		}
		k.Close()
	}
}

// collectAutoServices lists non-Microsoft-looking services with automatic,
// non-delayed start. Informational: each one adds work to the boot path.
func collectAutoServices(s *Snapshot) {
	root, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Services`, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		s.Note("Automatic services", err.Error())
		return
	}
	defer root.Close()
	names, err := root.ReadSubKeyNames(-1)
	if err != nil {
		s.Note("Automatic services", err.Error())
		return
	}
	for _, name := range names {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE,
			`SYSTEM\CurrentControlSet\Services\`+name, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		start, _, err1 := k.GetIntegerValue("Start")
		typ, _, err2 := k.GetIntegerValue("Type")
		delayed, _, _ := k.GetIntegerValue("DelayedAutostart")
		img, _, _ := k.GetStringValue("ImagePath")
		k.Close()
		if err1 != nil || err2 != nil {
			continue
		}
		// Start 2 = automatic; Type 0x10/0x20 = win32 service; skip delayed.
		if start != 2 || typ&0x30 == 0 || delayed == 1 {
			continue
		}
		low := strings.ToLower(img)
		if strings.Contains(low, `\windows\`) || strings.Contains(low, `\system32\`) ||
			strings.Contains(low, "svchost.exe") {
			continue
		}
		s.AutoServices = append(s.AutoServices, name)
		if len(s.AutoServices) >= 40 {
			break
		}
	}
}
