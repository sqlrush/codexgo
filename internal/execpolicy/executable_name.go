package execpolicy

import (
	"path/filepath"
	"runtime"
	"strings"
)

// windowsExecutableSuffixes are the executable extensions stripped from lookup
// keys on Windows, mirroring the Rust `WINDOWS_EXECUTABLE_SUFFIXES` constant.
var windowsExecutableSuffixes = [...]string{".exe", ".cmd", ".bat", ".com"}

// executableLookupKey normalizes a bare executable name into the key used to
// index rules and host-executable allowlists. It mirrors Rust's
// `executable_lookup_key`: on Windows the name is lowercased and a trailing
// executable suffix is stripped; on other platforms the name is returned
// unchanged.
func executableLookupKey(raw string) string {
	if runtime.GOOS != "windows" {
		return raw
	}
	lower := strings.ToLower(raw)
	for _, suffix := range windowsExecutableSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return lower[:len(lower)-len(suffix)]
		}
	}
	return lower
}

// executablePathLookupKey extracts the basename of path and normalizes it with
// [executableLookupKey]. It mirrors Rust's `executable_path_lookup_key`,
// returning ok=false when the path has no file name component.
func executablePathLookupKey(path string) (string, bool) {
	name := filepath.Base(path)
	// filepath.Base returns "." for an empty path and the separator for a
	// root-only path; treat those as having no usable basename, matching
	// Rust's `Path::file_name` returning `None`.
	if name == "." || name == string(filepath.Separator) || name == "" {
		return "", false
	}
	// On Windows, filepath.Base uses the OS separator; the normalized key also
	// handles forward slashes via the platform-aware base above.
	return executableLookupKey(name), true
}
