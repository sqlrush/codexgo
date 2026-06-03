package appserverproto

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestInitializeParamsRoundTrip(t *testing.T) {
	params := InitializeParams{
		ClientInfo: ClientInfo{
			Name:    "test-client",
			Title:   nil,
			Version: "0.1.0",
		},
	}
	// capabilities omitted (skip_serializing_if), title emitted as null.
	assertJSON(t, params, `{"clientInfo":{"name":"test-client","title":null,"version":"0.1.0"}}`)

	withCaps := InitializeParams{
		ClientInfo: ClientInfo{Name: "c", Title: strPtr("My IDE"), Version: "1"},
		Capabilities: &InitializeCapabilities{
			ExperimentalApi:    true,
			RequestAttestation: false,
		},
	}
	assertJSON(t, withCaps, `{"clientInfo":{"name":"c","title":"My IDE","version":"1"},"capabilities":{"experimentalApi":true,"requestAttestation":false,"optOutNotificationMethods":null}}`)

	var back InitializeParams
	b, _ := json.Marshal(withCaps)
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Capabilities == nil || !back.Capabilities.ExperimentalApi {
		t.Fatalf("capabilities round-trip lost: %+v", back.Capabilities)
	}
}

func TestInitializeResponseRoundTrip(t *testing.T) {
	resp := InitializeResponse{
		UserAgent:      "codex/0.136.0",
		CodexHome:      protocol.AbsolutePath("/home/user/.codex"),
		PlatformFamily: "unix",
		PlatformOs:     "macos",
	}
	assertJSON(t, resp, `{"userAgent":"codex/0.136.0","codexHome":"/home/user/.codex","platformFamily":"unix","platformOs":"macos"}`)
}

func TestGetConversationSummaryParamsUntagged(t *testing.T) {
	rollout := GetConversationSummaryParams{RolloutPath: strPtr("/tmp/r.jsonl")}
	assertJSON(t, rollout, `{"rolloutPath":"/tmp/r.jsonl"}`)

	tid := protocol.NewThreadID("11111111-1111-1111-1111-111111111111")
	byID := GetConversationSummaryParams{ConversationID: &tid}
	assertJSON(t, byID, `{"conversationId":"11111111-1111-1111-1111-111111111111"}`)

	// Decode each variant.
	var back GetConversationSummaryParams
	if err := json.Unmarshal([]byte(`{"rolloutPath":"/tmp/r.jsonl"}`), &back); err != nil {
		t.Fatalf("unmarshal rollout: %v", err)
	}
	if back.RolloutPath == nil || *back.RolloutPath != "/tmp/r.jsonl" {
		t.Fatalf("rollout decode = %+v", back)
	}

	var back2 GetConversationSummaryParams
	if err := json.Unmarshal([]byte(`{"conversationId":"11111111-1111-1111-1111-111111111111"}`), &back2); err != nil {
		t.Fatalf("unmarshal threadId: %v", err)
	}
	if back2.ConversationID == nil {
		t.Fatalf("threadId decode = %+v", back2)
	}
}

func TestGitDiffToRemoteResponseRoundTrip(t *testing.T) {
	resp := GitDiffToRemoteResponse{Sha: GitSha("abc123"), Diff: "diff --git ..."}
	assertJSON(t, resp, `{"sha":"abc123","diff":"diff --git ..."}`)
}

func TestGetAuthStatusResponseRoundTrip(t *testing.T) {
	mode := AuthModeChatgpt
	resp := GetAuthStatusResponse{AuthMethod: &mode}
	// All Option fields are emitted; authToken/requiresOpenaiAuth as null.
	assertJSON(t, resp, `{"authMethod":"chatgpt","authToken":null,"requiresOpenaiAuth":null}`)
}

func TestAuthModeValues(t *testing.T) {
	cases := map[AuthMode]string{
		AuthModeApiKey:            `"apikey"`,
		AuthModeChatgpt:           `"chatgpt"`,
		AuthModeChatgptAuthTokens: `"chatgptAuthTokens"`,
		AuthModeAgentIdentity:     `"agentIdentity"`,
	}
	for v, want := range cases {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(b) != want {
			t.Fatalf("marshal = %s, want %s", b, want)
		}
	}
}

func TestLoginApiKeyParamsRoundTrip(t *testing.T) {
	p := LoginApiKeyParams{ApiKey: "sk-test"}
	assertJSON(t, p, `{"apiKey":"sk-test"}`)
}

func TestSandboxSettingsAlwaysEmitsWritableRootsArray(t *testing.T) {
	// nil slice must serialize as [], matching Rust's always-serialized Vec.
	empty := SandboxSettings{}
	assertJSON(t, empty, `{"writableRoots":[],"networkAccess":null,"excludeTmpdirEnvVar":null,"excludeSlashTmp":null}`)

	net := true
	populated := SandboxSettings{
		WritableRoots: []protocol.AbsolutePath{"/work"},
		NetworkAccess: &net,
	}
	assertJSON(t, populated, `{"writableRoots":["/work"],"networkAccess":true,"excludeTmpdirEnvVar":null,"excludeSlashTmp":null}`)
}
