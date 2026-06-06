package appserverproto

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// This file ports the v2 config, model, and plugin protocol surfaces:
//   - config/read, config/batchWrite
//   - model/list
//   - plugin/* (list, installed, read, skill/read, share/*, install, uninstall)
//   - app/list, skills/list, hooks/list
//   - marketplace/* (add, remove, upgrade)
//   - collaborationMode/list, experimentalFeature/list
//
// Wire fidelity: method-name strings, serde rename_all, rename, default, and
// skip_serializing_if are reproduced byte-for-byte (after key-order
// canonicalization) against codex 0.136.0.
//
// Reference: app-server-protocol/src/protocol/v2/{config,model,plugin,apps,
// collaboration_mode,experimental_feature,hook}.rs and common.rs registration.

// -----------------------------------------------------------------------------
// config.rs: ConfigLayerSource (internally-tagged enum, tag = "type")
// -----------------------------------------------------------------------------

// ConfigLayerSourceKind discriminates ConfigLayerSource variants. Variant tag
// values are camelCase (serde rename_all = "camelCase" on the enum).
//
// Rust: v2::config::ConfigLayerSource, #[serde(tag = "type", rename_all = "camelCase")].
type ConfigLayerSourceKind string

const (
	// ConfigLayerSourceMdm is a managed preferences layer delivered by MDM.
	ConfigLayerSourceMdm ConfigLayerSourceKind = "mdm"
	// ConfigLayerSourceSystem is a managed config layer from a file.
	ConfigLayerSourceSystem ConfigLayerSourceKind = "system"
	// ConfigLayerSourceUser is the user config layer from $CODEXGO_HOME/config.toml.
	ConfigLayerSourceUser ConfigLayerSourceKind = "user"
	// ConfigLayerSourceProject is a .codex/ folder within a project.
	ConfigLayerSourceProject ConfigLayerSourceKind = "project"
	// ConfigLayerSourceSessionFlags are session overrides via -c/--config.
	ConfigLayerSourceSessionFlags ConfigLayerSourceKind = "sessionFlags"
	// ConfigLayerSourceLegacyManagedConfigTomlFromFile is the legacy file layer.
	ConfigLayerSourceLegacyManagedConfigTomlFromFile ConfigLayerSourceKind = "legacyManagedConfigTomlFromFile"
	// ConfigLayerSourceLegacyManagedConfigTomlFromMdm is the legacy MDM layer.
	ConfigLayerSourceLegacyManagedConfigTomlFromMdm ConfigLayerSourceKind = "legacyManagedConfigTomlFromMdm"
)

// ConfigLayerSource identifies one configuration layer. It is an internally
// tagged enum: the "type" key selects the variant, and the remaining keys carry
// that variant's payload (camelCase per the per-variant rename_all).
//
// Only the active variant's pointer fields are populated. The Profile field is
// part of the User variant and is always emitted (null when absent).
type ConfigLayerSource struct {
	Kind ConfigLayerSourceKind

	// Domain and Key apply to the Mdm variant.
	Domain *string
	Key    *string
	// File applies to System, LegacyManagedConfigTomlFromFile, and User variants.
	File *protocol.AbsolutePath
	// Profile applies to the User variant (always emitted as a key, null when absent).
	Profile *string
	// DotCodexFolder applies to the Project variant.
	DotCodexFolder *protocol.AbsolutePath
}

// configLayerSourceWire is the flattened wire shape used for (de)serialization.
type configLayerSourceWire struct {
	Type           ConfigLayerSourceKind  `json:"type"`
	Domain         *string                `json:"domain,omitempty"`
	Key            *string                `json:"key,omitempty"`
	File           *protocol.AbsolutePath `json:"file,omitempty"`
	DotCodexFolder *protocol.AbsolutePath `json:"dotCodexFolder,omitempty"`
}

// MarshalJSON encodes the internally-tagged ConfigLayerSource.
func (c ConfigLayerSource) MarshalJSON() ([]byte, error) {
	switch c.Kind {
	case ConfigLayerSourceMdm:
		return json.Marshal(configLayerSourceWire{Type: c.Kind, Domain: c.Domain, Key: c.Key})
	case ConfigLayerSourceSystem, ConfigLayerSourceLegacyManagedConfigTomlFromFile:
		return json.Marshal(configLayerSourceWire{Type: c.Kind, File: c.File})
	case ConfigLayerSourceUser:
		// The User variant always emits "profile" (Option<String> with no
		// skip_serializing_if), so it is rendered with a dedicated shape.
		return json.Marshal(struct {
			Type    ConfigLayerSourceKind  `json:"type"`
			File    *protocol.AbsolutePath `json:"file"`
			Profile *string                `json:"profile"`
		}{Type: c.Kind, File: c.File, Profile: c.Profile})
	case ConfigLayerSourceProject:
		return json.Marshal(configLayerSourceWire{Type: c.Kind, DotCodexFolder: c.DotCodexFolder})
	case ConfigLayerSourceSessionFlags, ConfigLayerSourceLegacyManagedConfigTomlFromMdm:
		return json.Marshal(struct {
			Type ConfigLayerSourceKind `json:"type"`
		}{Type: c.Kind})
	case "":
		return nil, fmt.Errorf("appserverproto: ConfigLayerSource has empty kind")
	default:
		return nil, fmt.Errorf("appserverproto: unknown ConfigLayerSource kind: %q", c.Kind)
	}
}

// UnmarshalJSON decodes the internally-tagged ConfigLayerSource.
func (c *ConfigLayerSource) UnmarshalJSON(data []byte) error {
	var w struct {
		Type           ConfigLayerSourceKind  `json:"type"`
		Domain         *string                `json:"domain"`
		Key            *string                `json:"key"`
		File           *protocol.AbsolutePath `json:"file"`
		Profile        *string                `json:"profile"`
		DotCodexFolder *protocol.AbsolutePath `json:"dotCodexFolder"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("appserverproto: decode ConfigLayerSource: %w", err)
	}
	switch w.Type {
	case ConfigLayerSourceMdm:
		*c = ConfigLayerSource{Kind: w.Type, Domain: w.Domain, Key: w.Key}
	case ConfigLayerSourceSystem, ConfigLayerSourceLegacyManagedConfigTomlFromFile:
		*c = ConfigLayerSource{Kind: w.Type, File: w.File}
	case ConfigLayerSourceUser:
		*c = ConfigLayerSource{Kind: w.Type, File: w.File, Profile: w.Profile}
	case ConfigLayerSourceProject:
		*c = ConfigLayerSource{Kind: w.Type, DotCodexFolder: w.DotCodexFolder}
	case ConfigLayerSourceSessionFlags, ConfigLayerSourceLegacyManagedConfigTomlFromMdm:
		*c = ConfigLayerSource{Kind: w.Type}
	case "":
		return fmt.Errorf("appserverproto: ConfigLayerSource missing type")
	default:
		return fmt.Errorf("appserverproto: unknown ConfigLayerSource type: %q", w.Type)
	}
	return nil
}

// SandboxWorkspaceWrite mirrors the persisted workspace-write sandbox config.
//
// Rust: snake_case; every field is `#[serde(default)]` with no
// skip_serializing_if, so all are always emitted (writableRoots as an array).
type SandboxWorkspaceWrite struct {
	WritableRoots       []string `json:"writable_roots"`
	NetworkAccess       bool     `json:"network_access"`
	ExcludeTmpdirEnvVar bool     `json:"exclude_tmpdir_env_var"`
	ExcludeSlashTmp     bool     `json:"exclude_slash_tmp"`
}

// MarshalJSON ensures writable_roots always emits a JSON array, never null.
func (s SandboxWorkspaceWrite) MarshalJSON() ([]byte, error) {
	type alias SandboxWorkspaceWrite
	out := alias(s)
	if out.WritableRoots == nil {
		out.WritableRoots = []string{}
	}
	return json.Marshal(out)
}

// ToolsV2 toggles optional tool surfaces in the v2 config. Rust: snake_case,
// single Option field always emitted (null when absent).
type ToolsV2 struct {
	WebSearch *protocol.WebSearchToolConfig `json:"web_search"`
}

// AnalyticsConfig carries analytics opt-in plus arbitrary extra keys. Rust:
// snake_case with `#[serde(flatten)]` additional map.
type AnalyticsConfig struct {
	Enabled    *bool                      `json:"enabled"`
	Additional map[string]json.RawMessage `json:"-"`
}

// MarshalJSON flattens Additional alongside the enabled field.
func (a AnalyticsConfig) MarshalJSON() ([]byte, error) {
	out := make(map[string]json.RawMessage, len(a.Additional)+1)
	for k, v := range a.Additional {
		out[k] = v
	}
	enabled, err := json.Marshal(a.Enabled)
	if err != nil {
		return nil, err
	}
	out["enabled"] = enabled
	return json.Marshal(out)
}

// UnmarshalJSON splits the enabled field from the flattened extras.
func (a *AnalyticsConfig) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("appserverproto: decode AnalyticsConfig: %w", err)
	}
	a.Additional = make(map[string]json.RawMessage)
	for k, v := range raw {
		if k == "enabled" {
			if err := json.Unmarshal(v, &a.Enabled); err != nil {
				return fmt.Errorf("appserverproto: decode AnalyticsConfig.enabled: %w", err)
			}
			continue
		}
		a.Additional[k] = v
	}
	return nil
}

// AppToolApproval is the per-tool approval mode. Rust: snake_case.
type AppToolApproval string

const (
	// AppToolApprovalAuto auto-approves the tool.
	AppToolApprovalAuto AppToolApproval = "auto"
	// AppToolApprovalPrompt prompts the user.
	AppToolApprovalPrompt AppToolApproval = "prompt"
	// AppToolApprovalApprove approves the tool.
	AppToolApprovalApprove AppToolApproval = "approve"
)

// AppsDefaultConfig holds the apps `_default` block. Rust: snake_case, each
// field defaults to true (default_enabled) and is always emitted.
type AppsDefaultConfig struct {
	Enabled            bool `json:"enabled"`
	DestructiveEnabled bool `json:"destructive_enabled"`
	OpenWorldEnabled   bool `json:"open_world_enabled"`
}

// AppToolConfig configures a single app tool. Rust: snake_case, both Option
// fields always emitted (null when absent).
type AppToolConfig struct {
	Enabled      *bool            `json:"enabled"`
	ApprovalMode *AppToolApproval `json:"approval_mode"`
}

// AppToolsConfig is a flattened map of tool name -> AppToolConfig. Rust:
// snake_case with a single `#[serde(flatten)]` map field.
type AppToolsConfig struct {
	Tools map[string]AppToolConfig `json:"-"`
}

// MarshalJSON emits the flattened tools map directly.
func (a AppToolsConfig) MarshalJSON() ([]byte, error) {
	if a.Tools == nil {
		return json.Marshal(map[string]AppToolConfig{})
	}
	return json.Marshal(a.Tools)
}

// UnmarshalJSON decodes the flattened tools map directly.
func (a *AppToolsConfig) UnmarshalJSON(data []byte) error {
	m := make(map[string]AppToolConfig)
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("appserverproto: decode AppToolsConfig: %w", err)
	}
	a.Tools = m
	return nil
}

// AppConfig configures a single app. Rust: snake_case; `enabled` defaults to
// true and is always emitted, the rest are Option (always emitted, null).
type AppConfig struct {
	Enabled                  bool             `json:"enabled"`
	DestructiveEnabled       *bool            `json:"destructive_enabled"`
	OpenWorldEnabled         *bool            `json:"open_world_enabled"`
	DefaultToolsApprovalMode *AppToolApproval `json:"default_tools_approval_mode"`
	DefaultToolsEnabled      *bool            `json:"default_tools_enabled"`
	Tools                    *AppToolsConfig  `json:"tools"`
}

// AppsConfig carries the `_default` block plus a flattened per-app map. Rust:
// snake_case; `default` is renamed "_default", `apps` is `#[serde(flatten)]`.
type AppsConfig struct {
	Default *AppsDefaultConfig
	Apps    map[string]AppConfig
}

// MarshalJSON emits _default alongside the flattened per-app entries.
func (a AppsConfig) MarshalJSON() ([]byte, error) {
	out := make(map[string]json.RawMessage, len(a.Apps)+1)
	for k, v := range a.Apps {
		enc, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		out[k] = enc
	}
	def, err := json.Marshal(a.Default)
	if err != nil {
		return nil, err
	}
	out["_default"] = def
	return json.Marshal(out)
}

// UnmarshalJSON splits _default from the flattened per-app entries.
func (a *AppsConfig) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("appserverproto: decode AppsConfig: %w", err)
	}
	a.Apps = make(map[string]AppConfig)
	for k, v := range raw {
		if k == "_default" {
			if err := json.Unmarshal(v, &a.Default); err != nil {
				return fmt.Errorf("appserverproto: decode AppsConfig._default: %w", err)
			}
			continue
		}
		var ac AppConfig
		if err := json.Unmarshal(v, &ac); err != nil {
			return fmt.Errorf("appserverproto: decode AppsConfig[%q]: %w", k, err)
		}
		a.Apps[k] = ac
	}
	return nil
}

// ForcedChatgptWorkspaceIds is the backward-compatible login-restriction shape:
// either a single workspace id string, or a list of ids. Rust: untagged enum.
type ForcedChatgptWorkspaceIds struct {
	// Single selects the string variant when non-nil.
	Single *string
	// Multiple selects the []string variant when non-nil.
	Multiple []string
}

// MarshalJSON emits the active untagged variant.
func (f ForcedChatgptWorkspaceIds) MarshalJSON() ([]byte, error) {
	switch {
	case f.Single != nil:
		return json.Marshal(*f.Single)
	case f.Multiple != nil:
		return json.Marshal(f.Multiple)
	default:
		return []byte("null"), nil
	}
}

// UnmarshalJSON decodes whichever untagged variant is present, preferring the
// single string (serde tries variants in declaration order).
func (f *ForcedChatgptWorkspaceIds) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = ForcedChatgptWorkspaceIds{Single: &s}
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		*f = ForcedChatgptWorkspaceIds{Multiple: list}
		return nil
	}
	return fmt.Errorf("appserverproto: ForcedChatgptWorkspaceIds matched no variant: %s", string(data))
}

// IntoVec normalizes both shapes into a slice, mirroring Rust's into_vec.
func (f ForcedChatgptWorkspaceIds) IntoVec() []string {
	switch {
	case f.Single != nil:
		return []string{*f.Single}
	default:
		return f.Multiple
	}
}

// Config is the effective v2 configuration surface. Rust: snake_case with a
// flattened `additional` map for forward compatibility. All Option fields are
// always emitted (null when absent); `apps` is `#[serde(default)]` Option.
type Config struct {
	Model                           *string                              `json:"model"`
	ReviewModel                     *string                              `json:"review_model"`
	ModelContextWindow              *int64                               `json:"model_context_window"`
	ModelAutoCompactTokenLimit      *int64                               `json:"model_auto_compact_token_limit"`
	ModelAutoCompactTokenLimitScope *protocol.AutoCompactTokenLimitScope `json:"model_auto_compact_token_limit_scope"`
	ModelProvider                   *string                              `json:"model_provider"`
	ApprovalPolicy                  *AskForApprovalV2                    `json:"approval_policy"`
	ApprovalsReviewer               *ApprovalsReviewerV2                 `json:"approvals_reviewer"`
	SandboxMode                     *SandboxModeV2                       `json:"sandbox_mode"`
	SandboxWorkspaceWrite           *SandboxWorkspaceWrite               `json:"sandbox_workspace_write"`
	ForcedChatgptWorkspaceID        *ForcedChatgptWorkspaceIds           `json:"forced_chatgpt_workspace_id"`
	ForcedLoginMethod               *protocol.ForcedLoginMethod          `json:"forced_login_method"`
	WebSearch                       *protocol.WebSearchMode              `json:"web_search"`
	Tools                           *ToolsV2                             `json:"tools"`
	Instructions                    *string                              `json:"instructions"`
	DeveloperInstructions           *string                              `json:"developer_instructions"`
	CompactPrompt                   *string                              `json:"compact_prompt"`
	ModelReasoningEffort            *protocol.ReasoningEffort            `json:"model_reasoning_effort"`
	ModelReasoningSummary           *protocol.ReasoningSummary           `json:"model_reasoning_summary"`
	ModelVerbosity                  *protocol.Verbosity                  `json:"model_verbosity"`
	ServiceTier                     *string                              `json:"service_tier"`
	Analytics                       *AnalyticsConfig                     `json:"analytics"`
	Apps                            *AppsConfig                          `json:"apps"`
	Desktop                         map[string]json.RawMessage           `json:"desktop"`
	Additional                      map[string]json.RawMessage           `json:"-"`
}

// configAlias avoids recursion in Config (Un)MarshalJSON.
type configAlias Config

// MarshalJSON flattens Additional onto the named fields.
func (c Config) MarshalJSON() ([]byte, error) {
	base, err := json.Marshal(configAlias(c))
	if err != nil {
		return nil, err
	}
	if len(c.Additional) == 0 {
		return base, nil
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(base, &merged); err != nil {
		return nil, err
	}
	for k, v := range c.Additional {
		if _, taken := merged[k]; !taken {
			merged[k] = v
		}
	}
	return json.Marshal(merged)
}

// configKnownKeys lists the named (non-flattened) Config keys.
var configKnownKeys = map[string]struct{}{
	"model": {}, "review_model": {}, "model_context_window": {},
	"model_auto_compact_token_limit": {}, "model_auto_compact_token_limit_scope": {},
	"model_provider": {}, "approval_policy": {}, "approvals_reviewer": {},
	"sandbox_mode": {}, "sandbox_workspace_write": {}, "forced_chatgpt_workspace_id": {},
	"forced_login_method": {}, "web_search": {}, "tools": {}, "instructions": {},
	"developer_instructions": {}, "compact_prompt": {}, "model_reasoning_effort": {},
	"model_reasoning_summary": {}, "model_verbosity": {}, "service_tier": {},
	"analytics": {}, "apps": {}, "desktop": {},
}

// UnmarshalJSON decodes named fields and collects unknown keys into Additional.
func (c *Config) UnmarshalJSON(data []byte) error {
	var alias configAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return fmt.Errorf("appserverproto: decode Config: %w", err)
	}
	*c = Config(alias)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("appserverproto: decode Config extras: %w", err)
	}
	extras := make(map[string]json.RawMessage)
	for k, v := range raw {
		if _, known := configKnownKeys[k]; !known {
			extras[k] = v
		}
	}
	if len(extras) > 0 {
		c.Additional = extras
	}
	return nil
}

// ConfigLayerMetadata names a layer plus its version. Rust: camelCase.
type ConfigLayerMetadata struct {
	Name    ConfigLayerSource `json:"name"`
	Version string            `json:"version"`
}

// ConfigLayer is one resolved config layer. Rust: camelCase; disabledReason is
// omitted when absent (skip_serializing_if).
type ConfigLayer struct {
	Name           ConfigLayerSource `json:"name"`
	Version        string            `json:"version"`
	Config         json.RawMessage   `json:"config"`
	DisabledReason *string           `json:"disabledReason,omitempty"`
}

// MergeStrategy selects how a config write merges into existing values. Rust:
// camelCase.
type MergeStrategy string

const (
	// MergeStrategyReplace overwrites the existing value.
	MergeStrategyReplace MergeStrategy = "replace"
	// MergeStrategyUpsert inserts or merges the value.
	MergeStrategyUpsert MergeStrategy = "upsert"
)

// WriteStatus reports the result of a config write. Rust: camelCase.
type WriteStatus string

const (
	// WriteStatusOk indicates the write succeeded.
	WriteStatusOk WriteStatus = "ok"
	// WriteStatusOkOverridden indicates the write succeeded but is overridden by
	// a higher-precedence layer.
	WriteStatusOkOverridden WriteStatus = "okOverridden"
)

// OverriddenMetadata describes a value overridden by a higher layer. Rust:
// camelCase.
type OverriddenMetadata struct {
	Message         string              `json:"message"`
	OverridingLayer ConfigLayerMetadata `json:"overridingLayer"`
	EffectiveValue  json.RawMessage     `json:"effectiveValue"`
}

// ConfigWriteResponse is the response to config writes. Rust: camelCase;
// overriddenMetadata is always emitted (null when absent).
type ConfigWriteResponse struct {
	Status             WriteStatus           `json:"status"`
	Version            string                `json:"version"`
	FilePath           protocol.AbsolutePath `json:"filePath"`
	OverriddenMetadata *OverriddenMetadata   `json:"overriddenMetadata"`
}

// ConfigWriteErrorCode enumerates config write failure causes. Rust: camelCase.
type ConfigWriteErrorCode string

const (
	// ConfigWriteErrorConfigLayerReadonly indicates the target layer is read-only.
	ConfigWriteErrorConfigLayerReadonly ConfigWriteErrorCode = "configLayerReadonly"
	// ConfigWriteErrorConfigVersionConflict indicates an expected-version mismatch.
	ConfigWriteErrorConfigVersionConflict ConfigWriteErrorCode = "configVersionConflict"
	// ConfigWriteErrorConfigValidationError indicates schema validation failed.
	ConfigWriteErrorConfigValidationError ConfigWriteErrorCode = "configValidationError"
	// ConfigWriteErrorConfigPathNotFound indicates the key path was not found.
	ConfigWriteErrorConfigPathNotFound ConfigWriteErrorCode = "configPathNotFound"
	// ConfigWriteErrorConfigSchemaUnknownKey indicates an unknown schema key.
	ConfigWriteErrorConfigSchemaUnknownKey ConfigWriteErrorCode = "configSchemaUnknownKey"
	// ConfigWriteErrorUserLayerNotFound indicates the user layer is missing.
	ConfigWriteErrorUserLayerNotFound ConfigWriteErrorCode = "userLayerNotFound"
)

// ConfigReadParams requests the effective config. Rust: camelCase;
// includeLayers defaults to false and is omitted when false (skip via Not::not).
type ConfigReadParams struct {
	IncludeLayers bool    `json:"includeLayers,omitempty"`
	Cwd           *string `json:"cwd,omitempty"`
}

// ConfigReadResponse returns the effective config plus per-key origins. Rust:
// camelCase; layers is omitted when absent (skip_serializing_if).
type ConfigReadResponse struct {
	Config  Config                         `json:"config"`
	Origins map[string]ConfigLayerMetadata `json:"origins"`
	Layers  *[]ConfigLayer                 `json:"layers,omitempty"`
}

// ConfigEdit is a single batched config edit. Rust: camelCase.
type ConfigEdit struct {
	KeyPath       string          `json:"keyPath"`
	Value         json.RawMessage `json:"value"`
	MergeStrategy MergeStrategy   `json:"mergeStrategy"`
}

// ConfigBatchWriteParams writes multiple config edits atomically. Rust:
// camelCase; reloadUserConfig defaults to false and is omitted when false.
type ConfigBatchWriteParams struct {
	Edits            []ConfigEdit `json:"edits"`
	FilePath         *string      `json:"filePath,omitempty"`
	ExpectedVersion  *string      `json:"expectedVersion,omitempty"`
	ReloadUserConfig bool         `json:"reloadUserConfig,omitempty"`
}

// TextPosition is a 1-based line/column position. Rust: camelCase.
type TextPosition struct {
	Line   uint64 `json:"line"`
	Column uint64 `json:"column"`
}

// TextRange spans two positions within a config file. Rust: camelCase.
type TextRange struct {
	Start TextPosition `json:"start"`
	End   TextPosition `json:"end"`
}

// ConfigWarningNotification reports a non-fatal config warning. Rust: camelCase;
// path and range are omitted when absent (skip_serializing_if).
type ConfigWarningNotification struct {
	Summary string     `json:"summary"`
	Details *string    `json:"details"`
	Path    *string    `json:"path,omitempty"`
	Range   *TextRange `json:"range,omitempty"`
}

// -----------------------------------------------------------------------------
// model.rs: model/list
// -----------------------------------------------------------------------------

// ModelRerouteReason is the camelCase v2 reroute reason. Rust:
// v2_enum_from_core! over CoreModelRerouteReason (camelCase).
type ModelRerouteReason string

const (
	// ModelRerouteReasonHighRiskCyberActivity reroutes on cyber risk.
	ModelRerouteReasonHighRiskCyberActivity ModelRerouteReason = "highRiskCyberActivity"
)

// ModelVerification is the camelCase v2 verification kind. Rust:
// v2_enum_from_core! over CoreModelVerification (camelCase).
type ModelVerification string

const (
	// ModelVerificationTrustedAccessForCyber grants trusted cyber access.
	ModelVerificationTrustedAccessForCyber ModelVerification = "trustedAccessForCyber"
)

// ModelListParams requests a page of models. Rust: camelCase; all Option fields
// always emitted (null when absent).
type ModelListParams struct {
	Cursor        *string `json:"cursor"`
	Limit         *uint32 `json:"limit"`
	IncludeHidden *bool   `json:"includeHidden"`
}

// ModelAvailabilityNux carries an availability NUX message. Rust: camelCase.
type ModelAvailabilityNux struct {
	Message string `json:"message"`
}

// ModelServiceTier describes a model service tier. Rust: camelCase.
type ModelServiceTier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ModelUpgradeInfo carries upgrade guidance for a model. Rust: camelCase.
type ModelUpgradeInfo struct {
	Model             string  `json:"model"`
	UpgradeCopy       *string `json:"upgradeCopy"`
	ModelLink         *string `json:"modelLink"`
	MigrationMarkdown *string `json:"migrationMarkdown"`
}

// ReasoningEffortOption pairs a reasoning effort with its description. Rust:
// camelCase.
type ReasoningEffortOption struct {
	ReasoningEffort protocol.ReasoningEffort `json:"reasoningEffort"`
	Description     string                   `json:"description"`
}

// Model describes one selectable model. Rust: camelCase; several `#[serde(default)]`
// fields (inputModalities defaults via default_input_modalities, the vec/bool
// fields default to empty/false) are always emitted.
type Model struct {
	ID                        string                   `json:"id"`
	Model                     string                   `json:"model"`
	Upgrade                   *string                  `json:"upgrade"`
	UpgradeInfo               *ModelUpgradeInfo        `json:"upgradeInfo"`
	AvailabilityNux           *ModelAvailabilityNux    `json:"availabilityNux"`
	DisplayName               string                   `json:"displayName"`
	Description               string                   `json:"description"`
	Hidden                    bool                     `json:"hidden"`
	SupportedReasoningEfforts []ReasoningEffortOption  `json:"supportedReasoningEfforts"`
	DefaultReasoningEffort    protocol.ReasoningEffort `json:"defaultReasoningEffort"`
	InputModalities           []protocol.InputModality `json:"inputModalities"`
	SupportsPersonality       bool                     `json:"supportsPersonality"`
	AdditionalSpeedTiers      []string                 `json:"additionalSpeedTiers"`
	ServiceTiers              []ModelServiceTier       `json:"serviceTiers"`
	DefaultServiceTier        *string                  `json:"defaultServiceTier"`
	IsDefault                 bool                     `json:"isDefault"`
}

// MarshalJSON ensures the always-serialized slices emit arrays, never null,
// matching the Rust `#[serde(default)]` Vec fields.
func (m Model) MarshalJSON() ([]byte, error) {
	type alias Model
	out := alias(m)
	if out.SupportedReasoningEfforts == nil {
		out.SupportedReasoningEfforts = []ReasoningEffortOption{}
	}
	if out.InputModalities == nil {
		out.InputModalities = protocol.DefaultInputModalities()
	}
	if out.AdditionalSpeedTiers == nil {
		out.AdditionalSpeedTiers = []string{}
	}
	if out.ServiceTiers == nil {
		out.ServiceTiers = []ModelServiceTier{}
	}
	return json.Marshal(out)
}

// ModelListResponse is a page of models plus a next-page cursor. Rust: camelCase.
type ModelListResponse struct {
	Data       []Model `json:"data"`
	NextCursor *string `json:"nextCursor"`
}

// ModelReroutedNotification reports a mid-turn model reroute. Rust: camelCase.
type ModelReroutedNotification struct {
	ThreadID  string             `json:"threadId"`
	TurnID    string             `json:"turnId"`
	FromModel string             `json:"fromModel"`
	ToModel   string             `json:"toModel"`
	Reason    ModelRerouteReason `json:"reason"`
}

// ModelVerificationNotification reports verification states for a turn. Rust:
// camelCase.
type ModelVerificationNotification struct {
	ThreadID      string              `json:"threadId"`
	TurnID        string              `json:"turnId"`
	Verifications []ModelVerification `json:"verifications"`
}
