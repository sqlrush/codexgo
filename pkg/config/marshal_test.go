package config

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestHookHandlerCommandRoundTrip(t *testing.T) {
	input := `{"type":"command","command":"echo hi","commandWindows":"echo hi","timeout":30,"async":true,"statusMessage":"running"}`
	var h HookHandlerConfig
	if err := json.Unmarshal([]byte(input), &h); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if h.Kind != HookHandlerCommand || h.Command != "echo hi" || !h.Async {
		t.Fatalf("decoded = %#v", h)
	}
	if h.TimeoutSec == nil || *h.TimeoutSec != 30 {
		t.Fatalf("timeout = %v", h.TimeoutSec)
	}
	out, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatalf("round unmarshal: %v", err)
	}
	if round["type"] != "command" || round["timeout"].(float64) != 30 {
		t.Fatalf("round = %#v", round)
	}
}

func TestHookHandlerCommandWindowsAlias(t *testing.T) {
	var h HookHandlerConfig
	if err := json.Unmarshal([]byte(`{"type":"command","command":"x","command_windows":"y"}`), &h); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if h.CommandWindows == nil || *h.CommandWindows != "y" {
		t.Fatalf("command_windows alias = %v", h.CommandWindows)
	}
}

func TestHookEventsIntoMatcherGroups(t *testing.T) {
	events := HookEventsToml{
		PreToolUse: []MatcherGroup{{Hooks: []HookHandlerConfig{{Kind: HookHandlerPrompt}}}},
		Stop:       []MatcherGroup{{Hooks: []HookHandlerConfig{{Kind: HookHandlerAgent}, {Kind: HookHandlerPrompt}}}},
	}
	if events.IsEmpty() {
		t.Fatalf("should not be empty")
	}
	if events.HandlerCount() != 3 {
		t.Fatalf("handler count = %d", events.HandlerCount())
	}
	groups := events.IntoMatcherGroups()
	if len(groups) != 10 {
		t.Fatalf("groups len = %d", len(groups))
	}
	if groups[0].Event != protocol.HookEventNamePreToolUse {
		t.Fatalf("first event = %v", groups[0].Event)
	}
	if groups[9].Event != protocol.HookEventNameStop || len(groups[9].Groups) != 1 {
		t.Fatalf("last event = %#v", groups[9])
	}
}

func TestNotificationsMarshal(t *testing.T) {
	def := DefaultNotifications()
	out, _ := json.Marshal(def)
	if string(out) != "true" {
		t.Fatalf("default notifications marshal = %s", out)
	}

	var custom Notifications
	if err := json.Unmarshal([]byte(`["a","b"]`), &custom); err != nil {
		t.Fatalf("unmarshal custom: %v", err)
	}
	out, _ = json.Marshal(custom)
	if string(out) != `["a","b"]` {
		t.Fatalf("custom marshal = %s", out)
	}
}

func TestForcedWorkspaceMarshal(t *testing.T) {
	single := "ws-1"
	f := ForcedChatgptWorkspaceIds{Single: &single}
	out, _ := json.Marshal(f)
	if string(out) != `"ws-1"` {
		t.Fatalf("single marshal = %s", out)
	}

	multi := ForcedChatgptWorkspaceIds{Multiple: &[]string{"a", "b"}}
	out, _ = json.Marshal(multi)
	if string(out) != `["a","b"]` {
		t.Fatalf("multi marshal = %s", out)
	}
}

func TestUriBasedFileOpenerScheme(t *testing.T) {
	tests := map[UriBasedFileOpener]string{
		UriBasedFileOpenerVsCode:         "vscode",
		UriBasedFileOpenerVsCodeInsiders: "vscode-insiders",
		UriBasedFileOpenerWindsurf:       "windsurf",
		UriBasedFileOpenerCursor:         "cursor",
	}
	for opener, want := range tests {
		got := opener.Scheme()
		if got == nil || *got != want {
			t.Fatalf("%s scheme = %v, want %q", opener, got, want)
		}
	}
	if UriBasedFileOpenerNone.Scheme() != nil {
		t.Fatalf("none should have no scheme")
	}
}

func TestProjectConfigTrust(t *testing.T) {
	trusted := protocol.TrustLevelTrusted
	untrusted := protocol.TrustLevelUntrusted
	if !(ProjectConfig{TrustLevel: &trusted}).IsTrusted() {
		t.Fatalf("trusted not detected")
	}
	if !(ProjectConfig{TrustLevel: &untrusted}).IsUntrusted() {
		t.Fatalf("untrusted not detected")
	}
	if (ProjectConfig{}).IsTrusted() || (ProjectConfig{}).IsUntrusted() {
		t.Fatalf("empty project should be neither")
	}
}

func TestMcpServerConfigUnmarshalRejectsUnknownFields(t *testing.T) {
	var cfg McpServerConfig
	err := json.Unmarshal([]byte(`{"command":"x","bogus_field":1}`), &cfg)
	if err == nil {
		t.Fatalf("expected error for unknown field")
	}
}
