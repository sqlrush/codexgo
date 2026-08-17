package config

import (
	"encoding/json"
	"fmt"
	"time"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/sqlrush/codexgo/pkg/features"
)

// ParseTomlValue parses a TOML document into a value tree (map[string]any tables,
// []any arrays, and scalar leaves), mirroring Rust's toml::from_str::<Value>.
func ParseTomlValue(contents []byte) (TomlValue, error) {
	var root map[string]any
	if err := toml.Unmarshal(contents, &root); err != nil {
		return nil, fmt.Errorf("parse toml: %w", err)
	}
	if root == nil {
		root = map[string]any{}
	}
	return normalizeTomlValue(root), nil
}

// normalizeTomlValue converts go-toml decoded scalars into the JSON-compatible
// shapes used throughout this package. TOML datetimes become RFC3339 strings so
// they round-trip through the JSON-based typed decode.
func normalizeTomlValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, child := range v {
			out[k] = normalizeTomlValue(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = normalizeTomlValue(child)
		}
		return out
	case time.Time:
		return v.Format(time.RFC3339Nano)
	default:
		return v
	}
}

// DecodeConfigToml decodes a TOML value tree into a typed ConfigToml, applying
// the serde defaults that the Rust crate uses. Unknown fields are ignored
// (matching serde deserialization, which does not deny unknown fields at load
// time; strict validation is a separate pass).
func DecodeConfigToml(value TomlValue) (ConfigToml, error) {
	cfg := DefaultConfigToml()
	data, err := json.Marshal(value)
	if err != nil {
		return ConfigToml{}, fmt.Errorf("encode config value: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ConfigToml{}, fmt.Errorf("decode config: %w", err)
	}
	if err := decodeFlattenedTables(value, &cfg); err != nil {
		return ConfigToml{}, err
	}
	if err := ValidateModelProviders(cfg.ModelProviders); err != nil {
		return ConfigToml{}, err
	}
	return cfg, nil
}

// decodeFlattenedTables handles the serde(flatten) members that plain JSON
// decoding cannot reconstruct: [features], [hooks] events, [agents] roles, and
// [tui] flattened notification/nux settings.
func decodeFlattenedTables(value TomlValue, cfg *ConfigToml) error {
	table, ok := value.(map[string]any)
	if !ok {
		return nil
	}

	if raw, ok := table["features"]; ok {
		parsed, err := decodeFeatures(raw)
		if err != nil {
			return err
		}
		cfg.Features = parsed
	}

	for name, profileRaw := range mapTable(table["profiles"]) {
		profileTable, ok := profileRaw.(map[string]any)
		if !ok {
			continue
		}
		if raw, ok := profileTable["features"]; ok {
			parsed, err := decodeFeatures(raw)
			if err != nil {
				return err
			}
			profile := cfg.Profiles[name]
			profile.Features = parsed
			cfg.Profiles[name] = profile
		}
	}

	if hooksRaw, ok := table["hooks"]; ok && cfg.Hooks != nil {
		if hooksTable, ok := hooksRaw.(map[string]any); ok {
			events, err := decodeHookEvents(hooksTable)
			if err != nil {
				return err
			}
			cfg.Hooks.Events = events
		}
	}

	if agentsRaw, ok := table["agents"]; ok && cfg.Agents != nil {
		if agentsTable, ok := agentsRaw.(map[string]any); ok {
			roles, err := decodeAgentRoles(agentsTable)
			if err != nil {
				return err
			}
			cfg.Agents.Roles = roles
		}
	}

	if tuiRaw, ok := table["tui"]; ok && cfg.Tui != nil {
		if tuiTable, ok := tuiRaw.(map[string]any); ok {
			if err := decodeTuiFlattened(tuiTable, cfg.Tui); err != nil {
				return err
			}
		}
	}

	return nil
}

func decodeFeatures(raw TomlValue) (*features.FeaturesToml, error) {
	table, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("features must be a table, got %T", raw)
	}
	data, err := toml.Marshal(table)
	if err != nil {
		return nil, fmt.Errorf("encode features: %w", err)
	}
	parsed, err := features.ParseFeaturesToml(data)
	if err != nil {
		return nil, fmt.Errorf("decode features: %w", err)
	}
	return &parsed, nil
}

var hookEventKeys = []string{
	"PreToolUse", "PermissionRequest", "PostToolUse", "PreCompact", "PostCompact",
	"SessionStart", "UserPromptSubmit", "SubagentStart", "SubagentStop", "Stop",
}

func decodeHookEvents(hooksTable map[string]any) (HookEventsToml, error) {
	subset := map[string]any{}
	for _, key := range hookEventKeys {
		if v, ok := hooksTable[key]; ok {
			subset[key] = v
		}
	}
	data, err := json.Marshal(subset)
	if err != nil {
		return HookEventsToml{}, fmt.Errorf("encode hooks: %w", err)
	}
	var events HookEventsToml
	if err := json.Unmarshal(data, &events); err != nil {
		return HookEventsToml{}, fmt.Errorf("decode hooks: %w", err)
	}
	return events, nil
}

var agentsScalarKeys = map[string]struct{}{
	"max_threads":             {},
	"max_depth":               {},
	"job_max_runtime_seconds": {},
	"interrupt_message":       {},
}

func decodeAgentRoles(agentsTable map[string]any) (map[string]AgentRoleToml, error) {
	roles := map[string]AgentRoleToml{}
	for key, raw := range agentsTable {
		if _, isScalar := agentsScalarKeys[key]; isScalar {
			continue
		}
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("encode agent role %q: %w", key, err)
		}
		var role AgentRoleToml
		if err := json.Unmarshal(data, &role); err != nil {
			return nil, fmt.Errorf("decode agent role %q: %w", key, err)
		}
		roles[key] = role
	}
	if len(roles) == 0 {
		return nil, nil
	}
	return roles, nil
}

func decodeTuiFlattened(tuiTable map[string]any, tui *Tui) error {
	notif := DefaultTuiNotificationSettings()
	if raw, ok := tuiTable["notifications"]; ok {
		data, _ := json.Marshal(raw)
		if err := json.Unmarshal(data, &notif.Notifications); err != nil {
			return fmt.Errorf("decode tui notifications: %w", err)
		}
	}
	if raw, ok := tuiTable["notification_method"].(string); ok {
		notif.Method = NotificationMethod(raw)
	}
	if raw, ok := tuiTable["notification_condition"].(string); ok {
		notif.Condition = NotificationCondition(raw)
	}
	tui.NotificationSettings = notif

	nux := ModelAvailabilityNuxConfig{ShownCount: map[string]uint32{}}
	if raw, ok := tuiTable["model_availability_nux"].(map[string]any); ok {
		for k, v := range raw {
			switch n := v.(type) {
			case int64:
				nux.ShownCount[k] = uint32(n)
			case float64:
				nux.ShownCount[k] = uint32(n)
			}
		}
	}
	tui.ModelAvailabilityNux = nux
	return nil
}

func mapTable(value TomlValue) map[string]any {
	if t, ok := value.(map[string]any); ok {
		return t
	}
	return map[string]any{}
}
