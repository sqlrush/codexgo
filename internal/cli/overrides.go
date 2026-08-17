package cli

import (
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/pkg/config"
)

// ConfigOverrides is the accumulated `-c key=value` override stream, preserving
// insertion order so root-level overrides can be prepended ahead of
// subcommand-specific ones (matching prepend_config_flags in main.rs: root
// overrides take lower precedence than later, subcommand-scoped ones).
type ConfigOverrides struct {
	raw []string
}

// Raw returns the override strings in precedence order (earliest = lowest).
func (o ConfigOverrides) Raw() []string {
	out := make([]string, len(o.raw))
	copy(out, o.raw)
	return out
}

// Append returns a new ConfigOverrides with the given `key=value` strings added
// at the end (highest precedence). The receiver is not mutated.
func (o ConfigOverrides) Append(raw ...string) ConfigOverrides {
	next := make([]string, 0, len(o.raw)+len(raw))
	next = append(next, o.raw...)
	next = append(next, raw...)
	return ConfigOverrides{raw: next}
}

// Prepend returns a new ConfigOverrides with the given root overrides placed
// ahead of the receiver's, mirroring prepend_root_overrides (root overrides have
// lower precedence than CLI-specific ones specified after the subcommand). The
// receiver is not mutated.
func (o ConfigOverrides) Prepend(root ConfigOverrides) ConfigOverrides {
	next := make([]string, 0, len(root.raw)+len(o.raw))
	next = append(next, root.raw...)
	next = append(next, o.raw...)
	return ConfigOverrides{raw: next}
}

// Parse converts the raw `key=value` overrides into typed config.CliOverride
// values, decoding each value as a TOML expression and falling back to a bare
// string. It mirrors CliConfigOverrides::parse_overrides.
func (o ConfigOverrides) Parse() ([]config.CliOverride, error) {
	out := make([]config.CliOverride, 0, len(o.raw))
	for _, entry := range o.raw {
		key, value, err := parseOverrideEntry(entry)
		if err != nil {
			return nil, err
		}
		out = append(out, config.CliOverride{Path: key, Value: value})
	}
	return out, nil
}

// parseOverrideEntry splits a `key=value` override and decodes the value. The key
// is everything before the first `=`; the value is parsed as a TOML scalar/array,
// falling back to the literal string when it is not valid TOML, matching the Rust
// behavior of trying toml first then treating the remainder as a string.
func parseOverrideEntry(entry string) (string, config.TomlValue, error) {
	idx := strings.Index(entry, "=")
	if idx < 0 {
		return "", nil, fmt.Errorf("invalid override (missing '='): %q; expected key=value", entry)
	}
	key := strings.TrimSpace(entry[:idx])
	if key == "" {
		return "", nil, fmt.Errorf("invalid override (empty key): %q; expected key=value", entry)
	}
	rawValue := strings.TrimSpace(entry[idx+1:])
	return key, decodeOverrideValue(rawValue), nil
}

// decodeOverrideValue parses a TOML expression. It wraps the expression in a
// throwaway assignment (`__codex_value__ = <expr>`) and parses it; on failure it
// returns the literal string, matching how Rust's parse_overrides accepts both
// typed values (`true`, `5`, `[1,2]`) and bare strings (`gpt-5`).
func decodeOverrideValue(raw string) config.TomlValue {
	if raw == "" {
		return ""
	}
	parsed, err := config.ParseTomlValue([]byte("__codex_value__ = " + raw))
	if err != nil {
		return raw
	}
	table, ok := parsed.(map[string]any)
	if !ok {
		return raw
	}
	value, ok := table["__codex_value__"]
	if !ok {
		return raw
	}
	return value
}
