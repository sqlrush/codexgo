package plugins

// Stable plugin identifier parsing and validation shared with the plugin cache.
// Ports `codex-rs/plugin/src/plugin_id.rs`.

import (
	"fmt"
	"strings"
)

// PluginIDError mirrors the Rust `PluginIdError`. It wraps an invalid-id message.
//
// The zero value is not meaningful; construct values via the package functions
// which return *PluginIDError on failure.
type PluginIDError struct {
	// Message is the human-readable validation failure, matching the Rust
	// `PluginIdError::Invalid(String)` display output exactly.
	Message string
}

// Error implements the error interface, mirroring the Rust `#[error("{0}")]`.
func (e *PluginIDError) Error() string {
	return e.Message
}

func newPluginIDError(message string) *PluginIDError {
	return &PluginIDError{Message: message}
}

// PluginID mirrors the Rust `PluginId`: a plugin name paired with its
// marketplace name. It is a comparable value type and is safe to copy; the
// fields are never mutated after construction.
type PluginID struct {
	PluginName      string
	MarketplaceName string
}

// NewPluginID mirrors the Rust `PluginId::new`. It validates each segment and
// returns the constructed id, or a *PluginIDError when a segment is invalid.
func NewPluginID(pluginName, marketplaceName string) (PluginID, error) {
	if err := ValidatePluginSegment(pluginName, "plugin name"); err != nil {
		return PluginID{}, newPluginIDError(err.Error())
	}
	if err := ValidatePluginSegment(marketplaceName, "marketplace name"); err != nil {
		return PluginID{}, newPluginIDError(err.Error())
	}
	return PluginID{PluginName: pluginName, MarketplaceName: marketplaceName}, nil
}

// ParsePluginID mirrors the Rust `PluginId::parse`. It splits pluginKey at the
// last '@' into <plugin>@<marketplace>, validating both segments. On failure it
// returns a *PluginIDError whose message matches the Rust output, including the
// trailing " in `<key>`" context that `PluginId::new` errors gain on parse.
func ParsePluginID(pluginKey string) (PluginID, error) {
	idx := strings.LastIndex(pluginKey, "@")
	if idx < 0 {
		return PluginID{}, newPluginIDError(fmt.Sprintf(
			"invalid plugin key `%s`; expected <plugin>@<marketplace>", pluginKey))
	}
	pluginName := pluginKey[:idx]
	marketplaceName := pluginKey[idx+1:]
	if pluginName == "" || marketplaceName == "" {
		return PluginID{}, newPluginIDError(fmt.Sprintf(
			"invalid plugin key `%s`; expected <plugin>@<marketplace>", pluginKey))
	}

	id, err := NewPluginID(pluginName, marketplaceName)
	if err != nil {
		// `PluginId::new` only ever returns PluginIdError::Invalid; append the
		// originating key for context, matching the Rust mapping.
		return PluginID{}, newPluginIDError(fmt.Sprintf("%s in `%s`", err.Error(), pluginKey))
	}
	return id, nil
}

// AsKey mirrors the Rust `PluginId::as_key`: it renders the id as
// "<plugin>@<marketplace>".
func (p PluginID) AsKey() string {
	return fmt.Sprintf("%s@%s", p.PluginName, p.MarketplaceName)
}

// ValidatePluginSegment mirrors the Rust `validate_plugin_segment`.
//
// A segment must be non-empty and contain only ASCII letters, digits, '_' and
// '-'. kind names the segment for the error message (for example "plugin name").
// It returns a plain error whose message matches the Rust string exactly; the
// returned error is nil on success.
func ValidatePluginSegment(segment, kind string) error {
	if segment == "" {
		return fmt.Errorf("invalid %s: must not be empty", kind)
	}
	for _, ch := range segment {
		if !isASCIIPluginSegmentChar(ch) {
			return fmt.Errorf(
				"invalid %s: only ASCII letters, digits, `_`, and `-` are allowed", kind)
		}
	}
	return nil
}

func isASCIIPluginSegmentChar(ch rune) bool {
	switch {
	case ch >= 'a' && ch <= 'z':
		return true
	case ch >= 'A' && ch <= 'Z':
		return true
	case ch >= '0' && ch <= '9':
		return true
	case ch == '-' || ch == '_':
		return true
	default:
		return false
	}
}
