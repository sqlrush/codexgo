package rollout

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestSessionSourceRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		json string
		want SessionSourceKind
	}{
		{"cli", `"cli"`, SessionSourceKindCli},
		{"vscode", `"vscode"`, SessionSourceKindVSCode},
		{"exec", `"exec"`, SessionSourceKindExec},
		{"mcp", `"mcp"`, SessionSourceKindMcp},
		{"unknown", `"unknown"`, SessionSourceKindUnknown},
		{"custom", `{"custom":"atlas"}`, SessionSourceKindCustom},
		{"internal", `{"internal":"memory_consolidation"}`, SessionSourceKindInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var s SessionSource
			if err := json.Unmarshal([]byte(tc.json), &s); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if s.Kind != tc.want {
				t.Fatalf("kind = %q, want %q", s.Kind, tc.want)
			}
			out, err := json.Marshal(s)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !jsonEqual(t, []byte(tc.json), out) {
				t.Fatalf("round-trip mismatch:\n want %s\n got  %s", tc.json, out)
			}
		})
	}
}

func TestSessionSourceUnknownFallback(t *testing.T) {
	var s SessionSource
	if err := json.Unmarshal([]byte(`"totally-unrecognized"`), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Kind != SessionSourceKindUnknown {
		t.Fatalf("expected unknown fallback, got %q", s.Kind)
	}
}

func TestSessionSourceSubAgentThreadSpawnRoundTrip(t *testing.T) {
	in := `{"subagent":{"thread_spawn":{"parent_thread_id":"5973b6c0-94b8-487b-a530-2aeb6098ae0e","depth":2,"agent_nickname":"nick","agent_role":"reviewer"}}}`
	var s SessionSource
	if err := json.Unmarshal([]byte(in), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Kind != SessionSourceKindSubAgent || s.SubAgent == nil {
		t.Fatalf("expected subagent source")
	}
	if s.SubAgent.Kind != SubAgentSourceKindThreadSpawn || s.SubAgent.ThreadSpawn == nil {
		t.Fatalf("expected thread_spawn sub-agent")
	}
	if s.Nickname() == nil || *s.Nickname() != "nick" {
		t.Fatalf("nickname = %v", s.Nickname())
	}
	if s.AgentRole() == nil || *s.AgentRole() != "reviewer" {
		t.Fatalf("agent role = %v", s.AgentRole())
	}
	out, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !jsonEqual(t, []byte(in), out) {
		t.Fatalf("round-trip mismatch:\n want %s\n got  %s", in, out)
	}
}

func TestSessionSourceDisplay(t *testing.T) {
	parent := protocol.NewThreadID("5973b6c0-94b8-487b-a530-2aeb6098ae0e")
	tests := []struct {
		src  SessionSource
		want string
	}{
		{NewCliSource(), "cli"},
		{NewVSCodeSource(), "vscode"},
		{NewExecSource(), "exec"},
		{NewMcpSource(), "mcp"},
		{NewCustomSource("atlas"), "atlas"},
		{SessionSource{Kind: SessionSourceKindUnknown}, "unknown"},
		{
			SessionSource{
				Kind: SessionSourceKindSubAgent,
				SubAgent: &SubAgentSource{
					Kind:        SubAgentSourceKindThreadSpawn,
					ThreadSpawn: &ThreadSpawnSource{ParentThreadID: parent, Depth: 3},
				},
			},
			"subagent_thread_spawn_5973b6c0-94b8-487b-a530-2aeb6098ae0e_d3",
		},
	}
	for _, tc := range tests {
		if got := tc.src.String(); got != tc.want {
			t.Errorf("Display(%v) = %q, want %q", tc.src.Kind, got, tc.want)
		}
	}
}

func TestSessionSourceFromStartupArg(t *testing.T) {
	tests := []struct {
		in      string
		want    SessionSourceKind
		wantErr bool
	}{
		{"cli", SessionSourceKindCli, false},
		{"VSCode", SessionSourceKindVSCode, false},
		{"app-server", SessionSourceKindMcp, false},
		{"appserver", SessionSourceKindMcp, false},
		{"unknown", SessionSourceKindUnknown, false},
		{"weird", SessionSourceKindCustom, false},
		{"   ", "", true},
	}
	for _, tc := range tests {
		got, err := SessionSourceFromStartupArg(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("SessionSourceFromStartupArg(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("SessionSourceFromStartupArg(%q): %v", tc.in, err)
			continue
		}
		if got.Kind != tc.want {
			t.Errorf("SessionSourceFromStartupArg(%q) = %q, want %q", tc.in, got.Kind, tc.want)
		}
	}
}
