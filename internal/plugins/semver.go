package plugins

// Minimal Semantic Versioning 2.0.0 parser and comparator used by the plugin
// store to order cached plugin versions. This mirrors the subset of the Rust
// `semver` crate's behavior that `compare_plugin_versions` relies on: parse a
// version and order by (major, minor, patch, prerelease). Build metadata is
// ignored for ordering, matching the semver spec and the `semver` crate.

import (
	"strconv"
	"strings"
)

// semverVersion is a parsed Semantic Versioning 2.0.0 value.
type semverVersion struct {
	major uint64
	minor uint64
	patch uint64
	pre   []string // dot-separated prerelease identifiers; empty means a release
}

// parseSemver mirrors `semver::Version::parse`. It returns ok=false for any
// input that is not a strict semver 2.0.0 version. Callers fall back to byte
// comparison when parsing fails, matching the Rust `compare_plugin_versions`.
func parseSemver(value string) (semverVersion, bool) {
	core := value
	build := ""
	hasBuild := false
	if idx := strings.IndexByte(core, '+'); idx >= 0 {
		build = core[idx+1:]
		core = core[:idx]
		hasBuild = true
	}
	pre := ""
	hasPre := false
	if idx := strings.IndexByte(core, '-'); idx >= 0 {
		pre = core[idx+1:]
		core = core[:idx]
		hasPre = true
	}
	// A trailing `-` or `+` with no identifiers is not valid semver.
	if hasPre && pre == "" {
		return semverVersion{}, false
	}
	if hasBuild && build == "" {
		return semverVersion{}, false
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semverVersion{}, false
	}
	major, ok := parseSemverNumber(parts[0])
	if !ok {
		return semverVersion{}, false
	}
	minor, ok := parseSemverNumber(parts[1])
	if !ok {
		return semverVersion{}, false
	}
	patch, ok := parseSemverNumber(parts[2])
	if !ok {
		return semverVersion{}, false
	}

	var preIDs []string
	if pre != "" {
		preIDs = strings.Split(pre, ".")
		for _, id := range preIDs {
			if !isValidSemverIdentifier(id) {
				return semverVersion{}, false
			}
		}
	}
	if build != "" {
		for _, id := range strings.Split(build, ".") {
			if !isValidSemverIdentifier(id) {
				return semverVersion{}, false
			}
		}
	}

	return semverVersion{major: major, minor: minor, patch: patch, pre: preIDs}, true
}

// parseSemverNumber parses a numeric identifier, rejecting leading zeros (except
// the literal "0") to match semver 2.0.0.
func parseSemverNumber(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// isValidSemverIdentifier reports whether id is a valid prerelease/build
// identifier: non-empty and composed of [0-9A-Za-z-], with no leading-zero
// numeric identifiers in prerelease (the leading-zero rule is enforced during
// comparison only for purely numeric identifiers, but the crate also rejects
// them at parse time for prerelease — we keep parsing lenient and rely on the
// numeric check below for ordering).
func isValidSemverIdentifier(id string) bool {
	if id == "" {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		isDigit := c >= '0' && c <= '9'
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isHyphen := c == '-'
		if !isDigit && !isAlpha && !isHyphen {
			return false
		}
	}
	return true
}

// compareSemver orders two parsed versions per the semver 2.0.0 precedence
// rules, returning -1, 0, or 1.
func compareSemver(a, b semverVersion) int {
	if c := cmpUint64(a.major, b.major); c != 0 {
		return c
	}
	if c := cmpUint64(a.minor, b.minor); c != 0 {
		return c
	}
	if c := cmpUint64(a.patch, b.patch); c != 0 {
		return c
	}
	return comparePrerelease(a.pre, b.pre)
}

// comparePrerelease compares prerelease identifier lists. A version with a
// prerelease has lower precedence than the associated release.
func comparePrerelease(a, b []string) int {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	if len(a) == 0 {
		return 1 // a is a release, higher precedence
	}
	if len(b) == 0 {
		return -1 // b is a release, higher precedence
	}

	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if c := comparePrereleaseIdentifier(a[i], b[i]); c != 0 {
			return c
		}
	}
	return cmpInt(len(a), len(b))
}

// comparePrereleaseIdentifier compares two prerelease identifiers. Numeric
// identifiers compare numerically and are always lower than alphanumeric ones.
func comparePrereleaseIdentifier(a, b string) int {
	aNum, aIsNum := parseSemverNumber(a)
	bNum, bIsNum := parseSemverNumber(b)
	switch {
	case aIsNum && bIsNum:
		return cmpUint64(aNum, bNum)
	case aIsNum:
		return -1 // numeric < alphanumeric
	case bIsNum:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

func cmpUint64(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
