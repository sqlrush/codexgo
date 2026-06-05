package cli

import (
	"os/exec"
	"runtime"
	"strings"
)

// doctorOSInfo is a best-effort projection of the os_info crate output used by
// system.environment in doctor.rs. The Rust doctor renders three labels:
//   - os: the full descriptive string (e.g. "Mac OS 26.4.1 [64-bit]")
//   - os type: the OS family (e.g. "Mac OS")
//   - os version: the product/release version (e.g. "26.4.1")
//
// codexgo has no os_info equivalent, so this resolves the closest faithful values
// per platform (sw_vers on macOS, /etc/os-release on Linux, ver/runtime on
// Windows). The label set and value types match codex; exact version strings on
// non-macOS hosts are best-effort. See DEVIATIONS.md (doctor).
type doctorOSInfo struct {
	// OS is the full descriptive OS string.
	OS string
	// OSType is the OS family name.
	OSType string
	// OSVersion is the product/release version, or "Unknown".
	OSVersion string
}

// detectOSInfo resolves the OS family, version, and descriptive string for the
// running platform.
func detectOSInfo() doctorOSInfo {
	bitness := osBitness()
	switch runtime.GOOS {
	case "darwin":
		version := macProductVersion()
		return doctorOSInfo{
			OS:        joinDescription("Mac OS", version, bitness),
			OSType:    "Mac OS",
			OSVersion: version,
		}
	case "windows":
		version := commandFirstWord("cmd", "/c", "ver")
		return doctorOSInfo{
			OS:        joinDescription("Windows", version, bitness),
			OSType:    "Windows",
			OSVersion: version,
		}
	case "linux":
		name, version := linuxRelease()
		return doctorOSInfo{
			OS:        joinDescription(name, version, bitness),
			OSType:    name,
			OSVersion: version,
		}
	default:
		return doctorOSInfo{OS: runtime.GOOS, OSType: runtime.GOOS, OSVersion: "Unknown"}
	}
}

// joinDescription renders the descriptive "<type> <version> [<bitness>]" string,
// omitting an unknown version, mirroring the os_info Display format.
func joinDescription(osType, version, bitness string) string {
	out := osType
	if version != "" && version != "Unknown" {
		out += " " + version
	}
	if bitness != "" {
		out += " [" + bitness + "]"
	}
	return out
}

// osBitness reports the pointer-width label os_info uses ("64-bit"/"32-bit").
func osBitness() string {
	if strings.HasSuffix(runtime.GOARCH, "64") || runtime.GOARCH == "arm64" {
		return "64-bit"
	}
	return "32-bit"
}

// macProductVersion reads the macOS product version via sw_vers.
func macProductVersion() string {
	return commandFirstWord("sw_vers", "-productVersion")
}

// linuxRelease reads the distribution name and version from /etc/os-release,
// falling back to a generic "Linux" name.
func linuxRelease() (name, version string) {
	name, version = "Linux", "Unknown"
	out, err := exec.Command("sh", "-c", ". /etc/os-release 2>/dev/null && printf '%s\\n%s' \"$NAME\" \"$VERSION_ID\"").Output()
	if err != nil {
		return name, version
	}
	lines := strings.SplitN(strings.TrimRight(string(out), "\n"), "\n", 2)
	if len(lines) >= 1 && strings.TrimSpace(lines[0]) != "" {
		name = strings.TrimSpace(lines[0])
	}
	if len(lines) >= 2 && strings.TrimSpace(lines[1]) != "" {
		version = strings.TrimSpace(lines[1])
	}
	return name, version
}

// commandFirstWord runs program with args and returns the trimmed first line, or
// "Unknown" when the command fails or produces no output.
func commandFirstWord(program string, args ...string) string {
	out, err := exec.Command(program, args...).Output()
	if err != nil {
		return "Unknown"
	}
	if line := firstLine(string(out)); line != "" {
		return line
	}
	return "Unknown"
}
