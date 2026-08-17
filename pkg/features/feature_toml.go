package features

import "fmt"

// FeatureToml is the untagged TOML/JSON representation used for features that
// need more than a boolean toggle. It is either a bare boolean (Enabled) or a
// config table (Config). Mirrors the Rust `FeatureToml<T>` enum
// (`#[serde(untagged)]`).
//
// The type parameter T is the feature-specific config type; it must be a
// pointer type that satisfies FeatureConfig (e.g. *MultiAgentV2ConfigToml).
type FeatureToml[T FeatureConfig] struct {
	// isConfig discriminates between the bool form (false) and the config form
	// (true).
	isConfig bool
	// boolValue holds the bare boolean when isConfig is false.
	boolValue bool
	// config holds the config table when isConfig is true.
	config T
}

// NewFeatureTomlEnabled builds the boolean form of FeatureToml. Mirrors the Rust
// `FeatureToml::Enabled(value)`.
func NewFeatureTomlEnabled[T FeatureConfig](value bool) FeatureToml[T] {
	return FeatureToml[T]{isConfig: false, boolValue: value}
}

// NewFeatureTomlConfig builds the config-table form of FeatureToml. Mirrors the
// Rust `FeatureToml::Config(config)`.
func NewFeatureTomlConfig[T FeatureConfig](config T) FeatureToml[T] {
	return FeatureToml[T]{isConfig: true, config: config}
}

// IsConfig reports whether this value holds a config table rather than a bare
// boolean.
func (f FeatureToml[T]) IsConfig() bool { return f.isConfig }

// BoolValue returns the bare boolean and ok=true when this value is the boolean
// form.
func (f FeatureToml[T]) BoolValue() (bool, bool) {
	if f.isConfig {
		return false, false
	}
	return f.boolValue, true
}

// Config returns the config table and ok=true when this value is the config
// form.
func (f FeatureToml[T]) Config() (T, bool) {
	if !f.isConfig {
		var zero T
		return zero, false
	}
	return f.config, true
}

// Enabled returns the effective enabled flag, or nil when unspecified. Mirrors
// `FeatureToml::enabled`.
func (f FeatureToml[T]) Enabled() *bool {
	if !f.isConfig {
		v := f.boolValue
		return &v
	}
	return f.config.Enabled()
}

// SetEnabled forces the enabled state in place. Mirrors `FeatureToml::set_enabled`.
func (f *FeatureToml[T]) SetEnabled(enabled bool) {
	if !f.isConfig {
		f.boolValue = enabled
		return
	}
	f.config.SetEnabled(enabled)
}

// decodeFeatureToml interprets a decoded TOML value (bool or table) into a
// FeatureToml[T]. The newConfig factory produces a fresh, mutable config to
// decode a table into. Mirrors serde's untagged enum behavior: a boolean
// matches Enabled, otherwise the value is decoded as the config struct.
func decodeFeatureToml[T FeatureConfig](
	value any,
	newConfig func() T,
	decodeConfig func(value any, into T) error,
) (FeatureToml[T], error) {
	if b, ok := value.(bool); ok {
		return NewFeatureTomlEnabled[T](b), nil
	}
	cfg := newConfig()
	if err := decodeConfig(value, cfg); err != nil {
		return FeatureToml[T]{}, fmt.Errorf("decode feature config: %w", err)
	}
	return NewFeatureTomlConfig(cfg), nil
}
