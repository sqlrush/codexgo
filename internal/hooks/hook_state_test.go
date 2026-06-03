package hooks

import (
	"testing"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/protocol"
)

func boolPtr(b bool) *bool { return &b }

func TestHookKey(t *testing.T) {
	got := HookKey("file:/tmp/hooks.json", protocol.HookEventNamePreToolUse, 0, 0)
	want := "file:/tmp/hooks.json:pre_tool_use:0:0"
	if got != want {
		t.Errorf("HookKey = %q, want %q", got, want)
	}

	got = HookKey("plugin:demo", protocol.HookEventNameStop, 2, 5)
	want = "plugin:demo:stop:2:5"
	if got != want {
		t.Errorf("HookKey = %q, want %q", got, want)
	}
}

func TestHookEventKeyLabel(t *testing.T) {
	tests := []struct {
		event protocol.HookEventName
		want  string
	}{
		{protocol.HookEventNamePreToolUse, "pre_tool_use"},
		{protocol.HookEventNamePermissionRequest, "permission_request"},
		{protocol.HookEventNamePostToolUse, "post_tool_use"},
		{protocol.HookEventNamePreCompact, "pre_compact"},
		{protocol.HookEventNamePostCompact, "post_compact"},
		{protocol.HookEventNameSessionStart, "session_start"},
		{protocol.HookEventNameUserPromptSubmit, "user_prompt_submit"},
		{protocol.HookEventNameSubagentStart, "subagent_start"},
		{protocol.HookEventNameSubagentStop, "subagent_stop"},
		{protocol.HookEventNameStop, "stop"},
	}
	for _, tt := range tests {
		if got := HookEventKeyLabel(tt.event); got != tt.want {
			t.Errorf("HookEventKeyLabel(%s) = %q, want %q", tt.event, got, tt.want)
		}
	}
}

// TestMergeHookStatesRespectsLayerPrecedence mirrors
// hook_states_from_stack_respects_layer_precedence: later layers win for a field
// they set.
func TestMergeHookStatesRespectsLayerPrecedence(t *testing.T) {
	key := "file:/tmp/hooks.json:pre_tool_use:0:0"
	layers := []map[string]config.HookStateToml{
		{key: {Enabled: boolPtr(false)}},
		{key: {Enabled: boolPtr(true)}},
	}
	got := MergeHookStates(layers)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	state := got[key]
	if state.Enabled == nil || *state.Enabled != true {
		t.Errorf("enabled = %v, want true", state.Enabled)
	}
	if state.TrustedHash != nil {
		t.Errorf("trustedHash = %v, want nil", state.TrustedHash)
	}
}

// TestMergeHookStatesMergesFieldsAcrossLayers mirrors
// hook_states_from_stack_merges_fields_across_layers: a later partial write does
// not erase an unrelated earlier field.
func TestMergeHookStatesMergesFieldsAcrossLayers(t *testing.T) {
	key := "file:/tmp/hooks.json:pre_tool_use:0:0"
	hash := "sha256:trusted"
	layers := []map[string]config.HookStateToml{
		{key: {Enabled: boolPtr(false)}},
		{key: {TrustedHash: &hash}},
	}
	got := MergeHookStates(layers)
	state := got[key]
	if state.Enabled == nil || *state.Enabled != false {
		t.Errorf("enabled = %v, want false", state.Enabled)
	}
	if state.TrustedHash == nil || *state.TrustedHash != hash {
		t.Errorf("trustedHash = %v, want %q", state.TrustedHash, hash)
	}
}

func TestMergeHookStatesTrimsAndDropsEmptyKeys(t *testing.T) {
	layers := []map[string]config.HookStateToml{
		{
			"  file:/tmp/hooks.json:stop:0:0  ": {Enabled: boolPtr(true)},
			"":                                  {Enabled: boolPtr(false)},
			"   ":                               {Enabled: boolPtr(false)},
		},
	}
	got := MergeHookStates(layers)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (empty keys dropped)", len(got))
	}
	if _, ok := got["file:/tmp/hooks.json:stop:0:0"]; !ok {
		t.Errorf("trimmed key missing: %v", got)
	}
}

func TestHookEnabled(t *testing.T) {
	tests := []struct {
		name      string
		isManaged bool
		state     *config.HookStateToml
		want      bool
	}{
		{"managed always enabled", true, &config.HookStateToml{Enabled: boolPtr(false)}, true},
		{"nil state enabled", false, nil, true},
		{"no enabled field enabled", false, &config.HookStateToml{}, true},
		{"explicit false disabled", false, &config.HookStateToml{Enabled: boolPtr(false)}, false},
		{"explicit true enabled", false, &config.HookStateToml{Enabled: boolPtr(true)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HookEnabled(tt.isManaged, tt.state); got != tt.want {
				t.Errorf("HookEnabled = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHookTrustedHash(t *testing.T) {
	hash := "sha256:abc"
	if got := HookTrustedHash(true, &config.HookStateToml{TrustedHash: &hash}); got != nil {
		t.Errorf("managed hook trusted hash = %v, want nil", got)
	}
	if got := HookTrustedHash(false, nil); got != nil {
		t.Errorf("nil state trusted hash = %v, want nil", got)
	}
	got := HookTrustedHash(false, &config.HookStateToml{TrustedHash: &hash})
	if got == nil || *got != hash {
		t.Errorf("trusted hash = %v, want %q", got, hash)
	}
}

func TestHookTrustStatus(t *testing.T) {
	hash := "sha256:current"
	other := "sha256:stale"
	tests := []struct {
		name        string
		isManaged   bool
		currentHash string
		trustedHash *string
		want        protocol.HookTrustStatus
	}{
		{"managed", true, hash, &hash, protocol.HookTrustStatusManaged},
		{"untrusted when no stored hash", false, hash, nil, protocol.HookTrustStatusUntrusted},
		{"trusted on match", false, hash, &hash, protocol.HookTrustStatusTrusted},
		{"modified on mismatch", false, hash, &other, protocol.HookTrustStatusModified},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HookTrustStatus(tt.isManaged, tt.currentHash, tt.trustedHash); got != tt.want {
				t.Errorf("HookTrustStatus = %s, want %s", got, tt.want)
			}
		})
	}
}
