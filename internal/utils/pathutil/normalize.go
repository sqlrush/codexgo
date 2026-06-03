package pathutil

import (
	"runtime"
	"strings"
)

// NormalizeForNativeWorkdir returns a path suitable for use as a native working
// directory. On Windows it strips verbatim/device prefixes (the dunce::simplified
// behavior); on every other platform the path is returned unchanged.
func NormalizeForNativeWorkdir(path string) string {
	return normalizeForNativeWorkdirWithFlag(path, runtime.GOOS == "windows")
}

// normalizeForNativeWorkdirWithFlag mirrors
// normalize_for_native_workdir_with_flag. The isWindows flag is threaded
// explicitly so the behavior is testable on any host.
func normalizeForNativeWorkdirWithFlag(path string, isWindows bool) string {
	if isWindows {
		return simplifyWindowsVerbatim(path)
	}
	return path
}

// simplifyWindowsVerbatim reproduces dunce::simplified: it removes a leading
// verbatim ("\\?\") prefix when the result is a plain drive path or UNC share,
// returning otherwise-unsupported device paths unchanged.
func simplifyWindowsVerbatim(path string) string {
	if rest, ok := strings.CutPrefix(path, `\\?\UNC\`); ok {
		return `\\` + rest
	}
	if rest, ok := strings.CutPrefix(path, `\\?\`); ok && isWindowsDriveAbsolutePath(rest) {
		return rest
	}
	return path
}

// normalizeForWSLWithFlag mirrors normalize_for_wsl_with_flag.
//
// When running under WSL, paths under /mnt/<drive> map onto case-insensitive
// Windows volumes, so they are lowercased (ASCII only). Paths that are not WSL
// drive mounts, and all paths when not under WSL, are returned unchanged.
func normalizeForWSLWithFlag(path string, isWSL bool) string {
	if !isWSL {
		return path
	}
	if !isWSLCaseInsensitivePath(path) {
		return path
	}
	return lowerASCIIPath(path)
}

// normalizeForWSL applies WSL normalization using live environment detection.
func normalizeForWSL(path string) string {
	return normalizeForWSLWithFlag(path, IsWSL())
}

// isWSLCaseInsensitivePath mirrors is_wsl_case_insensitive_path. It is only
// meaningful on Linux; elsewhere it always reports false.
//
// A matching path looks like /mnt/<drive>/... where <drive> is a single ASCII
// letter and "mnt" matches case-insensitively.
func isWSLCaseInsensitivePath(path string) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if !strings.HasPrefix(path, "/") {
		return false
	}
	comps := splitUnixComponents(path)
	if len(comps) < 2 {
		return false
	}
	if !strings.EqualFold(comps[0], "mnt") {
		return false
	}
	drive := comps[1]
	return len(drive) == 1 && isASCIIAlpha(drive[0])
}

// splitUnixComponents returns the non-empty path components of an absolute
// POSIX path (the leading RootDir component is dropped, matching Rust's
// Components iteration after RootDir).
func splitUnixComponents(path string) []string {
	var comps []string
	for _, c := range strings.Split(path, "/") {
		if c != "" {
			comps = append(comps, c)
		}
	}
	return comps
}

// lowerASCIIPath mirrors lower_ascii_path on Linux: it lowercases ASCII bytes of
// the entire path. On other platforms the path is returned unchanged.
func lowerASCIIPath(path string) string {
	if runtime.GOOS != "linux" {
		return path
	}
	b := []byte(path)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
