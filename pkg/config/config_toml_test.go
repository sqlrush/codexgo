package config

import (
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func parseConfig(t *testing.T, contents string) ConfigToml {
	t.Helper()
	value, err := ParseTomlValue([]byte(contents))
	if err != nil {
		t.Fatalf("parse toml: %v", err)
	}
	cfg, err := DecodeConfigToml(value)
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return cfg
}

func TestDecodeConfigTomlDefaults(t *testing.T) {
	cfg := parseConfig(t, "")
	if cfg.AllowLoginShell == nil || !*cfg.AllowLoginShell {
		t.Fatalf("allow_login_shell default = %v", cfg.AllowLoginShell)
	}
	if cfg.History == nil || cfg.History.Persistence != HistoryPersistenceSaveAll {
		t.Fatalf("history default = %#v", cfg.History)
	}
	if cfg.ProjectDocMaxBytes == nil || *cfg.ProjectDocMaxBytes != DefaultProjectDocMaxBytes {
		t.Fatalf("project_doc_max_bytes default = %v", cfg.ProjectDocMaxBytes)
	}
	if cfg.ProjectDocFallbackFilenames == nil || len(*cfg.ProjectDocFallbackFilenames) != 0 {
		t.Fatalf("project_doc_fallback_filenames default = %v", cfg.ProjectDocFallbackFilenames)
	}
	if cfg.HideAgentReasoning == nil || *cfg.HideAgentReasoning {
		t.Fatalf("hide_agent_reasoning default = %v", cfg.HideAgentReasoning)
	}
}

func TestDecodeConfigTomlBasicFields(t *testing.T) {
	cfg := parseConfig(t, `
model = "gpt-5-codex"
approval_policy = "on-request"
sandbox_mode = "workspace-write"
web_search = "live"

[sandbox_workspace_write]
network_access = true
writable_roots = ["/tmp/work"]
`)
	if cfg.Model == nil || *cfg.Model != "gpt-5-codex" {
		t.Fatalf("model = %v", cfg.Model)
	}
	if cfg.ApprovalPolicy == nil || cfg.ApprovalPolicy.Kind != protocol.AskForApprovalOnRequest {
		t.Fatalf("approval_policy = %#v", cfg.ApprovalPolicy)
	}
	if cfg.SandboxMode == nil || *cfg.SandboxMode != protocol.SandboxModeWorkspaceWrite {
		t.Fatalf("sandbox_mode = %v", cfg.SandboxMode)
	}
	if cfg.WebSearch == nil || *cfg.WebSearch != protocol.WebSearchModeLive {
		t.Fatalf("web_search = %v", cfg.WebSearch)
	}
	if cfg.SandboxWorkspaceWrite == nil || !cfg.SandboxWorkspaceWrite.NetworkAccess {
		t.Fatalf("sandbox_workspace_write = %#v", cfg.SandboxWorkspaceWrite)
	}
	if len(cfg.SandboxWorkspaceWrite.WritableRoots) != 1 || cfg.SandboxWorkspaceWrite.WritableRoots[0] != "/tmp/work" {
		t.Fatalf("writable_roots = %v", cfg.SandboxWorkspaceWrite.WritableRoots)
	}
}

func TestDecodeConfigTomlProfilesAndHistory(t *testing.T) {
	cfg := parseConfig(t, `
[history]
persistence = "none"
max_bytes = 1024

[profiles.work]
model = "gpt-5"
approval_policy = "never"
`)
	if cfg.History.Persistence != HistoryPersistenceNone {
		t.Fatalf("history persistence = %v", cfg.History.Persistence)
	}
	if cfg.History.MaxBytes == nil || *cfg.History.MaxBytes != 1024 {
		t.Fatalf("history max_bytes = %v", cfg.History.MaxBytes)
	}
	prof, ok := cfg.Profiles["work"]
	if !ok {
		t.Fatalf("missing profile work: %#v", cfg.Profiles)
	}
	if prof.Model == nil || *prof.Model != "gpt-5" {
		t.Fatalf("profile model = %v", prof.Model)
	}
	if prof.ApprovalPolicy == nil || prof.ApprovalPolicy.Kind != protocol.AskForApprovalNever {
		t.Fatalf("profile approval = %#v", prof.ApprovalPolicy)
	}
}

func TestDecodeConfigTomlFeaturesFlatten(t *testing.T) {
	cfg := parseConfig(t, `
[features]
some_feature_toggle = true
`)
	if cfg.Features == nil {
		t.Fatalf("features not decoded")
	}
	entries := cfg.Features.Entries()
	if !entries["some_feature_toggle"] {
		t.Fatalf("feature toggle missing: %#v", entries)
	}
}

func TestForcedChatgptWorkspaceID(t *testing.T) {
	const a = "123e4567-e89b-42d3-a456-426614174000"
	const b = "123e4567-e89b-42d3-a456-426614174001"

	cfg := parseConfig(t, `forced_chatgpt_workspace_id = "`+a+`"`)
	if got := cfg.ForcedChatgptWorkspaceID.IntoVec(); len(got) != 1 || got[0] != a {
		t.Fatalf("single = %v", got)
	}

	cfg = parseConfig(t, `forced_chatgpt_workspace_id = ["`+a+`", "`+b+`"]`)
	if got := cfg.ForcedChatgptWorkspaceID.IntoVec(); len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("multiple = %v", got)
	}

	value, err := ParseTomlValue([]byte(`forced_chatgpt_workspace_id = "` + a + `,` + b + `"`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = DecodeConfigToml(value)
	if err == nil || !strings.Contains(err.Error(), "comma-separated strings are not supported") {
		t.Fatalf("want comma-separated error, got %v", err)
	}
}
