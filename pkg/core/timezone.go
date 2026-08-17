package core

import (
	"os"
	"path/filepath"
	"strings"
)

// ianaTimezone returns the host IANA timezone name, mirroring codex's
// `local_time_context()` in core/src/session/turn_context.rs, which calls
// `iana_time_zone::get_timezone()`. On macOS that crate (v0.1.65) resolves the
// system zone via CoreFoundation's `CFTimeZoneCopySystem()` +
// `CFTimeZoneGetName()`. CoreFoundation honors the `TZ` environment variable but
// canonicalizes it through its (case-sensitive) timezone database: the alias
// `UTC` resolves to its canonical link target `GMT`, while an unset / empty /
// invalid `TZ` falls back to the system zone (the `/etc/localtime` symlink
// target). When `get_timezone()` errors, codex substitutes "Etc/UTC".
//
// This resolver reproduces the OBSERVED CoreFoundation behavior captured from
// the codex 0.136.0 binary on macOS:
//
//	TZ=UTC              -> GMT          (the only canonicalizing alias)
//	TZ=GMT              -> GMT
//	TZ=America/New_York -> America/New_York
//	TZ=Etc/UTC          -> Etc/UTC
//	TZ=gmt   (bad case) -> <system zone>   (rejected by CF's case-sensitive db)
//	TZ=      (empty)    -> <system zone>
//	unset               -> <system zone>
func ianaTimezone() string {
	return resolveIanaTimezone(os.Getenv("TZ"), tzNameIsValid, systemTimezone)
}

// resolveIanaTimezone is the testable core of ianaTimezone. The validity and
// system-zone lookups are injected so the canonicalization rule can be exercised
// deterministically, independent of the host's zoneinfo database. It mirrors the
// CoreFoundation resolution order used by iana_time_zone on macOS.
func resolveIanaTimezone(tz string, valid func(string) bool, system func() string) string {
	if tz != "" && valid(tz) {
		return canonicalizeTimezone(tz)
	}
	return system()
}

// canonicalizeTimezone maps a valid TZ value to its CoreFoundation canonical
// name. CF stores "GMT" as the canonical zone and "UTC" as a link/alias to it,
// so `CFTimeZoneGetName` reports "GMT" for an input of "UTC". Every other zone
// name in CF's database is already canonical (empirically: among the 443 known
// identifiers plus the abbreviation aliases UCT/Zulu/Universal/Greenwich/GMT0,
// only UTC self-canonicalizes), so all other names pass through unchanged.
func canonicalizeTimezone(tz string) string {
	if tz == "UTC" {
		return "GMT"
	}
	return tz
}

// tzNameIsValid reports whether name is a valid IANA zone the way CoreFoundation
// validates it: a case-SENSITIVE lookup against the zoneinfo database. Go's
// time.LoadLocation and a plain os.Stat are unsuitable on macOS because the
// filesystem is case-insensitive (so "gmt" would spuriously resolve "GMT"),
// whereas CF rejects mis-cased names. We therefore walk each path segment
// against the directory listing, requiring an exact-case match at every level.
func tzNameIsValid(name string) bool {
	root := zoneinfoRoot()
	if root == "" {
		return false
	}
	dir := root
	for _, seg := range strings.Split(name, "/") {
		if seg == "" {
			return false
		}
		if !dirHasExactEntry(dir, seg) {
			return false
		}
		dir = filepath.Join(dir, seg)
	}
	return true
}

// dirHasExactEntry reports whether dir contains an entry whose name matches seg
// exactly (case-sensitive), avoiding the case-insensitive open that os.Stat
// would perform on macOS.
func dirHasExactEntry(dir, seg string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Name() == seg {
			return true
		}
	}
	return false
}

// zoneinfoRoot returns the zoneinfo database root used to validate zone names,
// preferring the directory that the /etc/localtime symlink points into (macOS
// uses /var/db/timezone/zoneinfo; Linux uses /usr/share/zoneinfo) and falling
// back to the conventional locations.
func zoneinfoRoot() string {
	if target, err := os.Readlink(localtimePath); err == nil {
		if idx := strings.LastIndex(target, zoneinfoMarker); idx >= 0 {
			root := target[:idx+len(zoneinfoMarker)-1] // drop the trailing slash
			if fi, err := os.Stat(root); err == nil && fi.IsDir() {
				return root
			}
		}
	}
	for _, candidate := range zoneinfoFallbackRoots {
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			return candidate
		}
	}
	return ""
}

// systemTimezone resolves the host's system zone from the /etc/localtime
// symlink target under a zoneinfo root, mirroring the system-zone branch of
// CoreFoundation's CFTimeZoneCopySystem. When it cannot be determined, codex's
// get_timezone() error path substitutes "Etc/UTC".
func systemTimezone() string {
	if target, err := os.Readlink(localtimePath); err == nil {
		if idx := strings.LastIndex(target, zoneinfoMarker); idx >= 0 {
			if name := target[idx+len(zoneinfoMarker):]; name != "" {
				return filepath.ToSlash(name)
			}
		}
	}
	return getTimezoneErrorFallback
}

const (
	// localtimePath is the symlink the OS points at the active zone's data file.
	localtimePath = "/etc/localtime"
	// zoneinfoMarker is the path component preceding the zone name in a localtime
	// symlink target (e.g. .../zoneinfo/Asia/Shanghai).
	zoneinfoMarker = "zoneinfo/"
	// getTimezoneErrorFallback mirrors the Err(_) arm of codex's
	// local_time_context(), which substitutes "Etc/UTC".
	getTimezoneErrorFallback = "Etc/UTC"
)

// zoneinfoFallbackRoots are the conventional zoneinfo database locations probed
// when the /etc/localtime symlink does not reveal one.
var zoneinfoFallbackRoots = []string{
	"/var/db/timezone/zoneinfo", // macOS
	"/usr/share/zoneinfo",       // Linux / BSD
}
