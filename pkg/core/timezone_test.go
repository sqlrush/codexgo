package core

import "testing"

// TestResolveIanaTimezone pins the CoreFoundation-mirroring resolution rule of
// resolveIanaTimezone (the testable core of ianaTimezone). It injects a fixed
// validity predicate and system-zone so the canonicalization branch is exercised
// independent of the host zoneinfo database. The expected values are the exact
// outputs captured from the codex 0.136.0 binary on macOS (see timezone.go).
func TestResolveIanaTimezone(t *testing.T) {
	const systemZone = "Asia/Shanghai"
	// validNames models CoreFoundation's case-sensitive timezone database: only
	// the exactly-cased canonical/alias names are accepted.
	validNames := map[string]bool{
		"UTC":              true,
		"GMT":              true,
		"Etc/UTC":          true,
		"America/New_York": true,
		"Europe/London":    true,
		"Greenwich":        true,
		"Universal":        true,
		"UCT":              true,
		"US/Eastern":       true,
	}
	valid := func(name string) bool { return validNames[name] }
	system := func() string { return systemZone }

	tests := []struct {
		name string
		tz   string
		want string
	}{
		{"utc alias canonicalizes to gmt", "UTC", "GMT"},
		{"gmt already canonical", "GMT", "GMT"},
		{"etc/utc passthrough", "Etc/UTC", "Etc/UTC"},
		{"region passthrough", "America/New_York", "America/New_York"},
		{"london passthrough", "Europe/London", "Europe/London"},
		{"greenwich passthrough", "Greenwich", "Greenwich"},
		{"universal passthrough", "Universal", "Universal"},
		{"uct passthrough", "UCT", "UCT"},
		{"legacy region passthrough", "US/Eastern", "US/Eastern"},
		{"empty falls back to system zone", "", systemZone},
		{"lowercase invalid falls back to system zone", "gmt", systemZone},
		{"unknown name falls back to system zone", "Not/AZone", systemZone},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := resolveIanaTimezone(tt.tz, valid, system)
			if got != tt.want {
				t.Errorf("resolveIanaTimezone(%q) = %q, want %q", tt.tz, got, tt.want)
			}
		})
	}
}

// TestCanonicalizeTimezone pins the single CoreFoundation canonicalization that
// diverges from raw passthrough: the UTC -> GMT alias link.
func TestCanonicalizeTimezone(t *testing.T) {
	tests := []struct{ in, want string }{
		{"UTC", "GMT"},
		{"GMT", "GMT"},
		{"Etc/UTC", "Etc/UTC"},
		{"America/New_York", "America/New_York"},
		{"UCT", "UCT"},
		{"Zulu", "Zulu"},
	}
	for _, tt := range tests {
		if got := canonicalizeTimezone(tt.in); got != tt.want {
			t.Errorf("canonicalizeTimezone(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestTzNameIsValidCaseSensitive verifies the host-backed validity predicate
// rejects mis-cased names the way CoreFoundation does, even though the macOS
// filesystem is case-insensitive. It is skipped if the host has no zoneinfo
// database (so the package still builds/tests on minimal environments).
func TestTzNameIsValidCaseSensitive(t *testing.T) {
	if zoneinfoRoot() == "" {
		t.Skip("no zoneinfo database on host")
	}
	if !tzNameIsValid("UTC") {
		t.Errorf("tzNameIsValid(%q) = false, want true", "UTC")
	}
	if !tzNameIsValid("America/New_York") {
		t.Errorf("tzNameIsValid(%q) = false, want true", "America/New_York")
	}
	if tzNameIsValid("gmt") {
		t.Errorf("tzNameIsValid(%q) = true, want false (mis-cased)", "gmt")
	}
	if tzNameIsValid("Not/AZone") {
		t.Errorf("tzNameIsValid(%q) = true, want false (unknown)", "Not/AZone")
	}
	if tzNameIsValid("") {
		t.Errorf("tzNameIsValid(%q) = true, want false (empty)", "")
	}
}
