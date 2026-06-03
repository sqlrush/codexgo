package ollama

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a minimal semantic version (major.minor.patch) sufficient for the
// Ollama Responses-API compatibility check. It stands in for the Rust crate's
// dependency on the third-party `semver` crate, supporting only the comparison
// semantics that codex-ollama actually relies on: equality with Version::new
// (the bare numeric core) and ordering against the minimum supported version.
//
// Pre-release and build metadata are accepted by ParseVersion but discarded, as
// the reference code only ever compares against bare numeric versions built with
// Version::new.
type Version struct {
	Major uint64
	Minor uint64
	Patch uint64
}

// NewVersion builds a Version from its numeric components, mirroring the Rust
// semver::Version::new.
func NewVersion(major, minor, patch uint64) Version {
	return Version{Major: major, Minor: minor, Patch: patch}
}

// String renders the version as "major.minor.patch".
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare returns -1, 0, or 1 as v is less than, equal to, or greater than
// other, comparing the numeric core only.
func (v Version) Compare(other Version) int {
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
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// ParseVersion parses a semantic version string into a Version.
//
// A leading 'v' is stripped before parsing (matching the normalization applied
// by OllamaClient.FetchVersion, which uses trim_start_matches('v')). Pre-release
// and build metadata (the portions after '-' and '+') are accepted and
// discarded. All three numeric components are required.
//
// An error is returned for input that does not contain a valid numeric core.
func ParseVersion(s string) (Version, error) {
	core := strings.TrimLeft(strings.TrimSpace(s), "v")

	// Drop build metadata (after '+') then pre-release (after '-').
	if i := strings.IndexByte(core, '+'); i >= 0 {
		core = core[:i]
	}
	if i := strings.IndexByte(core, '-'); i >= 0 {
		core = core[:i]
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("ollama: invalid semantic version %q", s)
	}

	major, err := parseVersionComponent(parts[0])
	if err != nil {
		return Version{}, fmt.Errorf("ollama: invalid major version in %q: %w", s, err)
	}
	minor, err := parseVersionComponent(parts[1])
	if err != nil {
		return Version{}, fmt.Errorf("ollama: invalid minor version in %q: %w", s, err)
	}
	patch, err := parseVersionComponent(parts[2])
	if err != nil {
		return Version{}, fmt.Errorf("ollama: invalid patch version in %q: %w", s, err)
	}

	return Version{Major: major, Minor: minor, Patch: patch}, nil
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
