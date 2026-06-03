package cliutil

import (
	"fmt"
	"strings"
)

// CliConfigOverrides captures arbitrary configuration overrides specified as
// `-c key=value` on the command line.
//
// It mirrors codex_utils_cli::CliConfigOverrides. Both halves of each override
// are intentionally kept unparsed so calling code can decide how to interpret
// the right-hand side; [CliConfigOverrides.ParseOverrides] performs the TOML
// interpretation lazily.
//
// The zero value is a usable, empty set of overrides. Following the package's
// immutable style, the mutating helpers return new values instead of modifying
// the receiver in place.
type CliConfigOverrides struct {
	rawOverrides []string
}

// NewCliConfigOverrides constructs a CliConfigOverrides from the raw
// `key=value` strings collected from the command line. The input slice is
// copied so the returned value does not alias caller-owned storage.
func NewCliConfigOverrides(rawOverrides []string) CliConfigOverrides {
	cp := make([]string, len(rawOverrides))
	copy(cp, rawOverrides)
	return CliConfigOverrides{rawOverrides: cp}
}

// RawOverrides returns a copy of the captured raw override strings, in order.
func (c CliConfigOverrides) RawOverrides() []string {
	cp := make([]string, len(c.rawOverrides))
	copy(cp, c.rawOverrides)
	return cp
}

// PrependRootOverrides returns a new CliConfigOverrides with the root-level
// override strings placed before the receiver's own strings, so root flags have
// lower precedence than command-specific flags parsed after a subcommand.
//
// Mirroring the upstream prepend_root_overrides, the result preserves the
// relative order of both groups. Neither the receiver nor rootOverrides is
// mutated.
func (c CliConfigOverrides) PrependRootOverrides(rootOverrides CliConfigOverrides) CliConfigOverrides {
	merged := make([]string, 0, len(rootOverrides.rawOverrides)+len(c.rawOverrides))
	merged = append(merged, rootOverrides.rawOverrides...)
	merged = append(merged, c.rawOverrides...)
	return CliConfigOverrides{rawOverrides: merged}
}

// ConfigOverride is a single parsed override: a dotted configuration path and
// the TOML value to apply at that path.
type ConfigOverride struct {
	// Path is the dotted configuration path, for example "foo.bar.baz".
	Path string
	// Value is the value to apply at Path.
	Value TOMLValue
}

// ParseOverrides parses the captured raw strings into a list of
// [ConfigOverride] entries, mirroring the upstream parse_overrides.
//
// Each raw string is split on the first '=' only, so values may freely contain
// the character. The key is trimmed and the canonicalized key alias is applied
// (see canonicalizeOverrideKey). The value is parsed as a TOML value; if TOML
// parsing fails the trimmed value (with one optional layer of surrounding single
// or double quotes removed) is used as a literal string, allowing convenient
// usage such as `-c model=o3` without quotes.
//
// An error is returned when an override is missing the '=' separator or has an
// empty key.
func (c CliConfigOverrides) ParseOverrides() ([]ConfigOverride, error) {
	result := make([]ConfigOverride, 0, len(c.rawOverrides))
	for _, s := range c.rawOverrides {
		key, valueStr, ok := strings.Cut(s, "=")
		if !ok {
			return nil, fmt.Errorf("Invalid override (missing '='): %s", s)
		}
		key = strings.TrimSpace(key)
		valueStr = strings.TrimSpace(valueStr)

		if key == "" {
			return nil, fmt.Errorf("Empty key in override: %s", s)
		}

		value, err := parseTOMLValue(valueStr)
		if err != nil {
			value = TOMLStringValue(stripSurroundingQuotes(strings.TrimSpace(valueStr)))
		}

		result = append(result, ConfigOverride{
			Path:  canonicalizeOverrideKey(key),
			Value: value,
		})
	}
	return result, nil
}

// ApplyOnValue applies all parsed overrides onto target and returns the
// resulting value, mirroring the upstream apply_on_value. Intermediate tables
// are created as necessary and values at a destination path are replaced.
//
// The supplied target is not mutated; a new value reflecting the overrides is
// returned. An error is returned when override parsing fails.
func (c CliConfigOverrides) ApplyOnValue(target TOMLValue) (TOMLValue, error) {
	overrides, err := c.ParseOverrides()
	if err != nil {
		return TOMLValue{}, err
	}
	result := target
	for _, o := range overrides {
		result = applySingleOverride(result, o.Path, o.Value)
	}
	return result, nil
}

// canonicalizeOverrideKey applies the known key aliases, mirroring the upstream
// canonicalize_override_key. Currently only "use_legacy_landlock" is remapped to
// "features.use_legacy_landlock".
func canonicalizeOverrideKey(key string) string {
	if key == "use_legacy_landlock" {
		return "features.use_legacy_landlock"
	}
	return key
}

// stripSurroundingQuotes removes one optional layer of matching leading and
// trailing single or double quotes, mirroring the upstream
// trim_matches(|c| c == '"' || c == '\”). The Rust version strips any run of
// such quote characters from both ends; this reproduces that by trimming every
// leading and trailing quote character.
func stripSurroundingQuotes(s string) string {
	return strings.Trim(s, "\"'")
}

// applySingleOverride returns a new value with value applied at the dotted path
// within root, creating intermediate tables as necessary. The root value is not
// mutated.
//
// It mirrors the upstream apply_single_override: traversing a non-table along
// the path replaces that node with a fresh table, and the final segment is
// inserted (or replaced) in the deepest table.
func applySingleOverride(root TOMLValue, path string, value TOMLValue) TOMLValue {
	parts := strings.Split(path, ".")
	return setPath(root, parts, value)
}

// setPath recursively sets value at the head-relative path within current,
// returning a new value. When current is not a table it is replaced by a fresh
// table, matching the upstream behavior.
func setPath(current TOMLValue, parts []string, value TOMLValue) TOMLValue {
	part := parts[0]
	if len(parts) == 1 {
		if current.Kind() != TOMLTable {
			return EmptyTOMLTable().withTableEntry(part, value)
		}
		return current.withTableEntry(part, value)
	}

	base := current
	if base.Kind() != TOMLTable {
		base = EmptyTOMLTable()
	}
	child, ok := base.Get(part)
	if !ok {
		child = EmptyTOMLTable()
	}
	updatedChild := setPath(child, parts[1:], value)
	return base.withTableEntry(part, updatedChild)
}
