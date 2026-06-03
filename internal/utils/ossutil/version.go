package ossutil

import (
	"fmt"
	"strconv"
	"strings"
)

// minResponsesMajor, minResponsesMinor and minResponsesPatch define the minimum
// Ollama server version that supports the Responses API. They mirror
// codex_ollama::min_responses_version (Version::new(0, 13, 4)).
const (
	minResponsesMajor = 0
	minResponsesMinor = 13
	minResponsesPatch = 4
)

// SemVer is a minimal semantic version (major.minor.patch) sufficient for the
// Ollama Responses-API compatibility check. It stands in for the Rust crate's
// dependency on the third-party `semver` crate, supporting only the comparison
// semantics that codex-ollama actually relies on (ordering of the numeric
// core). Pre-release and build metadata are parsed away but not compared,
// matching how the reference code uses Version::new with bare numeric versions.
type SemVer struct {
	Major uint64
	Minor uint64
	Patch uint64
}

// MinResponsesVersion returns the minimum Ollama version that supports the
// Responses API. It mirrors codex_ollama::min_responses_version.
func MinResponsesVersion() SemVer {
	return SemVer{Major: minResponsesMajor, Minor: minResponsesMinor, Patch: minResponsesPatch}
}

// String renders the version as "major.minor.patch".
func (v SemVer) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// compare returns -1, 0, or 1 as v is less than, equal to, or greater than
// other, comparing the numeric core only.
func (v SemVer) compare(other SemVer) int {
	switch {
	case v.Major != other.Major:
		return cmpUint(v.Major, other.Major)
	case v.Minor != other.Minor:
		return cmpUint(v.Minor, other.Minor)
	case v.Patch != other.Patch:
		return cmpUint(v.Patch, other.Patch)
	default:
		return 0
	}
}

func cmpUint(a, b uint64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// SupportsResponses reports whether an Ollama server of the given version
// supports the Responses API.
//
// It mirrors codex_ollama::supports_responses: a development build reporting
// version 0.0.0 is always considered supported, and any version greater than or
// equal to the minimum (0.13.4) is supported.
func SupportsResponses(version SemVer) bool {
	zero := SemVer{}
	if version.compare(zero) == 0 {
		return true
	}
	return version.compare(MinResponsesVersion()) >= 0
}

// ParseSemVer parses a semantic version string into a SemVer.
//
// A single leading 'v' is stripped before parsing, matching the normalization
// applied by codex_ollama::OllamaClient::fetch_version. Pre-release and build
// metadata (the portions after '-' and '+') are accepted and discarded, since
// the comparison semantics used by the OSS readiness check only consider the
// numeric core. The major, minor and patch components are all required.
//
// An error is returned for input that does not contain a valid numeric core.
func ParseSemVer(s string) (SemVer, error) {
	raw := strings.TrimSpace(s)
	core := strings.TrimPrefix(raw, "v")

	// Drop build metadata (after '+') then pre-release (after '-').
	if i := strings.IndexByte(core, '+'); i >= 0 {
		core = core[:i]
	}
	if i := strings.IndexByte(core, '-'); i >= 0 {
		core = core[:i]
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return SemVer{}, fmt.Errorf("ossutil: invalid semantic version %q", s)
	}

	major, err := parseVersionComponent(parts[0])
	if err != nil {
		return SemVer{}, fmt.Errorf("ossutil: invalid major version in %q: %w", s, err)
	}
	minor, err := parseVersionComponent(parts[1])
	if err != nil {
		return SemVer{}, fmt.Errorf("ossutil: invalid minor version in %q: %w", s, err)
	}
	patch, err := parseVersionComponent(parts[2])
	if err != nil {
		return SemVer{}, fmt.Errorf("ossutil: invalid patch version in %q: %w", s, err)
	}

	return SemVer{Major: major, Minor: minor, Patch: patch}, nil
}

// parseVersionComponent parses one non-empty, all-digit numeric component.
func parseVersionComponent(s string) (uint64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty version component")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("non-numeric version component %q", s)
		}
	}
	return strconv.ParseUint(s, 10, 64)
}
