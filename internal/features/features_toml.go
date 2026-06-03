package features

import (
	"bytes"
	"fmt"
	"sort"

	toml "github.com/pelletier/go-toml/v2"
)

// FeaturesToml is the deserializable `[features]` table. It mirrors the Rust
// `FeaturesToml` struct: three typed config fields plus a flattened map of
// boolean feature toggles keyed by canonical or legacy feature name.
type FeaturesToml struct {
	// MultiAgentV2 is the optional `[features.multi_agent_v2]` config.
	MultiAgentV2 *FeatureToml[*MultiAgentV2ConfigToml]
	// AppsMcpPathOverride is the optional `[features.apps_mcp_path_override]`
	// config.
	AppsMcpPathOverride *FeatureToml[*AppsMcpPathOverrideConfigToml]
	// NetworkProxy is the optional `[features.network_proxy]` config.
	NetworkProxy *FeatureToml[*NetworkProxyConfigToml]
	// entries are the boolean feature toggles keyed by canonical or legacy
	// feature name (the serde `#[serde(flatten)]` map).
	entries map[string]bool
}

// keys reserved for the typed config fields; everything else flattens into
// entries.
const (
	keyMultiAgentV2        = "multi_agent_v2"
	keyAppsMcpPathOverride = "apps_mcp_path_override"
	keyNetworkProxy        = "network_proxy"
)

// NewFeaturesTomlFromEntries builds a FeaturesToml from a boolean toggle map.
// Mirrors the Rust `From<BTreeMap<String, bool>>` impl.
func NewFeaturesTomlFromEntries(entries map[string]bool) FeaturesToml {
	cp := make(map[string]bool, len(entries))
	for k, v := range entries {
		cp[k] = v
	}
	return FeaturesToml{entries: cp}
}

// SetEntry returns a new FeaturesToml with the given boolean toggle set,
// preserving immutability of the receiver.
func (f FeaturesToml) SetEntry(key string, value bool) FeaturesToml {
	out := f.clone()
	if out.entries == nil {
		out.entries = map[string]bool{}
	}
	out.entries[key] = value
	return out
}

// clone returns a deep copy of the receiver so callers can mutate without
// affecting shared state.
func (f FeaturesToml) clone() FeaturesToml {
	out := FeaturesToml{}
	if f.MultiAgentV2 != nil {
		v := *f.MultiAgentV2
		out.MultiAgentV2 = &v
	}
	if f.AppsMcpPathOverride != nil {
		v := *f.AppsMcpPathOverride
		out.AppsMcpPathOverride = &v
	}
	if f.NetworkProxy != nil {
		v := *f.NetworkProxy
		out.NetworkProxy = &v
	}
	if f.entries != nil {
		out.entries = make(map[string]bool, len(f.entries))
		for k, v := range f.entries {
			out.entries[k] = v
		}
	}
	return out
}

// ParseFeaturesToml decodes a `[features]` table from raw TOML bytes. Mirrors
// `toml::from_str::<FeaturesToml>`.
func ParseFeaturesToml(data []byte) (FeaturesToml, error) {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return FeaturesToml{}, fmt.Errorf("parse features toml: %w", err)
	}
	return featuresTomlFromMap(raw)
}

// featuresTomlFromMap converts a decoded TOML map into a FeaturesToml,
// dispatching the three typed config fields and flattening the rest into the
// boolean entries map. Non-bool extra keys are an error, matching serde's
// `BTreeMap<String, bool>` flatten target.
func featuresTomlFromMap(raw map[string]any) (FeaturesToml, error) {
	out := FeaturesToml{entries: map[string]bool{}}

	for key, value := range raw {
		switch key {
		case keyMultiAgentV2:
			ft, err := decodeFeatureToml(value, func() *MultiAgentV2ConfigToml { return &MultiAgentV2ConfigToml{} }, decodeStrictConfig)
			if err != nil {
				return FeaturesToml{}, fmt.Errorf("features.%s: %w", key, err)
			}
			out.MultiAgentV2 = &ft
		case keyAppsMcpPathOverride:
			ft, err := decodeFeatureToml(value, func() *AppsMcpPathOverrideConfigToml { return &AppsMcpPathOverrideConfigToml{} }, decodeStrictConfig)
			if err != nil {
				return FeaturesToml{}, fmt.Errorf("features.%s: %w", key, err)
			}
			out.AppsMcpPathOverride = &ft
		case keyNetworkProxy:
			ft, err := decodeFeatureToml(value, func() *NetworkProxyConfigToml { return &NetworkProxyConfigToml{} }, decodeStrictConfig)
			if err != nil {
				return FeaturesToml{}, fmt.Errorf("features.%s: %w", key, err)
			}
			out.NetworkProxy = &ft
		default:
			b, ok := value.(bool)
			if !ok {
				return FeaturesToml{}, fmt.Errorf("features.%s: expected boolean toggle, got %T", key, value)
			}
			out.entries[key] = b
		}
	}

	return out, nil
}

// decodeStrictConfig re-marshals a decoded TOML sub-value to TOML and decodes it
// into the typed config with unknown-field rejection, replicating serde
// `deny_unknown_fields`.
func decodeStrictConfig[T any](value any, into T) error {
	encoded, err := toml.Marshal(value)
	if err != nil {
		return fmt.Errorf("re-encode config table: %w", err)
	}
	dec := toml.NewDecoder(bytes.NewReader(encoded))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("decode config table: %w", err)
	}
	return nil
}

// Entries returns the effective boolean toggle map, augmented with the resolved
// enabled flag from each typed config field. Mirrors `FeaturesToml::entries`.
func (f FeaturesToml) Entries() map[string]bool {
	entries := make(map[string]bool, len(f.entries)+3)
	for k, v := range f.entries {
		entries[k] = v
	}
	if f.MultiAgentV2 != nil {
		if enabled := f.MultiAgentV2.Enabled(); enabled != nil {
			entries[FeatureMultiAgentV2.Key()] = *enabled
		}
	}
	if f.AppsMcpPathOverride != nil {
		if enabled := f.AppsMcpPathOverride.Enabled(); enabled != nil {
			entries[FeatureAppsMcpPathOverride.Key()] = *enabled
		}
	}
	if f.NetworkProxy != nil {
		if enabled := f.NetworkProxy.Enabled(); enabled != nil {
			entries[FeatureNetworkProxy.Key()] = *enabled
		}
	}
	return entries
}

// MaterializeResolvedEnabled rewrites the FeaturesToml so every feature reflects
// its resolved enabled state from the supplied feature set, dropping legacy
// alias keys and preserving any custom config in the typed fields. Mirrors
// `FeaturesToml::materialize_resolved_enabled`. The receiver is mutated in
// place, matching the Rust `&mut self` signature.
func (f *FeaturesToml) MaterializeResolvedEnabled(features *Features) {
	if f.entries == nil {
		f.entries = map[string]bool{}
	}
	for _, key := range LegacyFeatureKeys() {
		delete(f.entries, key)
	}
	for _, spec := range FEATURES {
		enabled := features.Enabled(spec.ID)
		switch spec.ID {
		case FeatureMultiAgentV2:
			materializeResolvedFeatureEnabled(&f.MultiAgentV2, enabled)
		case FeatureAppsMcpPathOverride:
			materializeResolvedFeatureEnabled(&f.AppsMcpPathOverride, enabled)
		case FeatureNetworkProxy:
			materializeResolvedFeatureEnabled(&f.NetworkProxy, enabled)
		default:
			f.entries[spec.Key] = enabled
		}
	}
}

// materializeResolvedFeatureEnabled sets the resolved enabled flag on a typed
// FeatureToml field, allocating a bool form when the field is unset. Mirrors the
// Rust generic helper of the same name.
func materializeResolvedFeatureEnabled[T FeatureConfig](
	field **FeatureToml[T],
	enabled bool,
) {
	if *field != nil {
		(*field).SetEnabled(enabled)
		return
	}
	ft := NewFeatureTomlEnabled[T](enabled)
	*field = &ft
}

// SortedEntryKeys returns the boolean entry keys in sorted (BTreeMap) order. It
// is a convenience for deterministic iteration in callers and tests.
func (f FeaturesToml) SortedEntryKeys() []string {
	keys := make([]string, 0, len(f.entries))
	for k := range f.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
