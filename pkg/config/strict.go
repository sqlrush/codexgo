package config

import (
	"fmt"
	"sort"

	"github.com/sqlrush/codexgo/pkg/features"
)

// knownConfigTomlKeys is the set of top-level keys accepted by ConfigToml. It is
// derived from the Rust struct (schemars(deny_unknown_fields)).
var knownConfigTomlKeys = map[string]struct{}{
	"model": {}, "review_model": {}, "model_provider": {}, "model_context_window": {},
	"model_auto_compact_token_limit": {}, "model_auto_compact_token_limit_scope": {},
	"approval_policy": {}, "approvals_reviewer": {}, "auto_review": {},
	"shell_environment_policy": {}, "allow_login_shell": {}, "sandbox_mode": {},
	"sandbox_workspace_write": {}, "default_permissions": {}, "permissions": {},
	"notify": {}, "instructions": {}, "developer_instructions": {},
	"include_permissions_instructions": {}, "include_apps_instructions": {},
	"include_collaboration_mode_instructions": {}, "include_environment_context": {},
	"model_instructions_file": {}, "compact_prompt": {}, "forced_chatgpt_workspace_id": {},
	"forced_login_method": {}, "cli_auth_credentials_store": {}, "mcp_servers": {},
	"mcp_oauth_credentials_store": {}, "mcp_oauth_callback_port": {}, "mcp_oauth_callback_url": {},
	"model_providers": {}, "project_doc_max_bytes": {}, "project_doc_fallback_filenames": {},
	"tool_output_token_limit": {}, "background_terminal_max_timeout": {},
	"js_repl_node_path": {}, "js_repl_node_module_dirs": {}, "profile": {}, "profiles": {},
	"history": {}, "sqlite_home": {}, "log_dir": {}, "debug": {}, "file_opener": {},
	"tui": {}, "hide_agent_reasoning": {}, "show_raw_agent_reasoning": {},
	"model_reasoning_effort": {}, "plan_mode_reasoning_effort": {}, "model_reasoning_summary": {},
	"model_verbosity": {}, "model_supports_reasoning_summaries": {}, "model_catalog_json": {},
	"personality": {}, "service_tier": {}, "chatgpt_base_url": {}, "apps_mcp_product_sku": {},
	"openai_base_url": {}, "audio": {}, "experimental_realtime_ws_base_url": {},
	"experimental_realtime_ws_model": {}, "realtime": {},
	"experimental_realtime_ws_backend_prompt": {}, "experimental_realtime_ws_startup_context": {},
	"experimental_realtime_start_instructions": {}, "experimental_thread_config_endpoint": {},
	"experimental_thread_store_endpoint": {}, "experimental_thread_store": {}, "projects": {},
	"web_search": {}, "tools": {}, "tool_suggest": {}, "agents": {}, "memories": {},
	"skills": {}, "hooks": {}, "plugins": {}, "marketplaces": {}, "features": {},
	"suppress_unstable_features_warning": {}, "ghost_snapshot": {}, "project_root_markers": {},
	"check_for_update_on_startup": {}, "disable_paste_burst": {}, "analytics": {}, "feedback": {},
	"apps": {}, "desktop": {}, "otel": {}, "windows": {}, "notice": {},
	"experimental_compact_prompt_file": {}, "experimental_use_unified_exec_tool": {},
	"oss_provider": {},
}

// UnknownConfigField returns the first unknown top-level configuration field in
// the TOML value tree, joined as a dotted path, or "" if none. It also reports
// unknown feature keys in [features] and [profiles.*.features], mirroring the
// Rust strict-config field-tracking behavior for those tables.
func UnknownConfigField(value TomlValue) string {
	table, ok := value.(map[string]any)
	if !ok {
		return ""
	}

	for _, key := range sortedKeys(table) {
		if _, known := knownConfigTomlKeys[key]; !known {
			return key
		}
	}

	if path := unknownFeaturePath(table); path != "" {
		return path
	}
	return ""
}

// StrictConfigError returns a non-nil error describing the first unknown field
// when strict is true, otherwise nil. When strict is false the caller should
// treat unknown fields as warnings.
func StrictConfigError(value TomlValue, strict bool) error {
	field := UnknownConfigField(value)
	if field == "" {
		return nil
	}
	if !strict {
		return nil
	}
	return fmt.Errorf("unknown configuration field `%s`", field)
}

func unknownFeaturePath(root map[string]any) string {
	if path := firstUnknownFeatureKey([]string{"features"}, root["features"]); path != "" {
		return path
	}
	profiles, ok := root["profiles"].(map[string]any)
	if !ok {
		return ""
	}
	for _, name := range sortedKeys(profiles) {
		profile, ok := profiles[name].(map[string]any)
		if !ok {
			continue
		}
		prefix := []string{"profiles", name, "features"}
		if path := firstUnknownFeatureKey(prefix, profile["features"]); path != "" {
			return path
		}
	}
	return ""
}

func firstUnknownFeatureKey(prefix []string, value any) string {
	featuresTable, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	keys := make([]string, 0, len(featuresTable))
	for k := range featuresTable {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if features.IsKnownFeatureKey(key) {
			continue
		}
		return joinPath(append(append([]string(nil), prefix...), key))
	}
	return ""
}

func joinPath(segments []string) string {
	out := ""
	for i, s := range segments {
		if i > 0 {
			out += "."
		}
		out += s
	}
	return out
}
