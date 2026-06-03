package analytics

import (
	"os/exec"
	"strings"
)

// osVersion returns a best-effort OS version string, mirroring the
// runtime_os_version that codex's os_info crate reports. The exact format is not
// part of any on-disk/wire contract beyond being a free-form string, so a
// best-effort uname-based value is acceptable.
func osVersion() string {
	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return "Unknown"
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "Unknown"
	}
	return v
}
