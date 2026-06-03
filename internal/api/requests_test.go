package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/sqlrush/codexgo/internal/client"
	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestBuildSessionHeaders(t *testing.T) {
	sid := "s1"
	tid := "t1"
	headers := BuildSessionHeaders(&sid, &tid)
	if headers.Get("session-id") != "s1" || headers.Get("thread-id") != "t1" {
		t.Fatalf("unexpected headers: %v", headers)
	}
	empty := BuildSessionHeaders(nil, nil)
	if len(empty) != 0 {
		t.Fatalf("expected empty headers")
	}
}

func TestSubagentHeader(t *testing.T) {
	tests := []struct {
		name   string
		source *SessionSource
		want   string
		ok     bool
	}{
		{"nil", nil, "", false},
		{"review", ptrSource(SubAgentSource{Kind: SubAgentReview}), "review", true},
		{"compact", ptrSource(SubAgentSource{Kind: SubAgentCompact}), "compact", true},
		{"memory", ptrSource(SubAgentSource{Kind: SubAgentMemoryConsolidation}), "memory_consolidation", true},
		{"spawn", ptrSource(SubAgentSource{Kind: SubAgentThreadSpawn}), "collab_spawn", true},
		{"other", ptrSource(SubAgentSource{Kind: SubAgentOther, Label: "custom"}), "custom", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := subagentHeader(tt.source)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("got (%q,%v) want (%q,%v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func ptrSource(sub SubAgentSource) *SessionSource {
	s := NewSubAgentSession(sub)
	return &s
}

func TestAttachItemIDs(t *testing.T) {
	rid := "reasoning-1"
	items := []protocol.ResponseItem{
		{Type: protocol.ResponseItemKindReasoning, ReasoningID: rid},
	}
	body := `{"model":"m","input":[{"type":"reasoning","summary":[],"encrypted_content":null}]}`
	out, err := attachItemIDs([]byte(body), items)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var input []map[string]json.RawMessage
	if err := json.Unmarshal(parsed["input"], &input); err != nil {
		t.Fatalf("unmarshal input: %v", err)
	}
	var gotID string
	if err := json.Unmarshal(input[0]["id"], &gotID); err != nil || gotID != rid {
		t.Fatalf("expected id %q, got %q", rid, gotID)
	}
}

func TestAttachItemIDsSkipsEmptyID(t *testing.T) {
	mid := ""
	items := []protocol.ResponseItem{
		{Type: protocol.ResponseItemKindMessage, MessageID: &mid},
	}
	body := `{"model":"m","input":[{"type":"message","role":"assistant","content":[]}]}`
	out, err := attachItemIDs([]byte(body), items)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if string(out) == "" {
		t.Fatalf("expected body")
	}
	var parsed map[string]json.RawMessage
	_ = json.Unmarshal(out, &parsed)
	var input []map[string]json.RawMessage
	_ = json.Unmarshal(parsed["input"], &input)
	if _, ok := input[0]["id"]; ok {
		t.Fatalf("did not expect id for empty value")
	}
}

func TestAttachItemIDsNoInputIsNoop(t *testing.T) {
	body := `{"model":"m"}`
	out, err := attachItemIDs([]byte(body), nil)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if string(out) != body {
		t.Fatalf("expected unchanged body, got %s", out)
	}
}

func TestBearerAuthAddsHeader(t *testing.T) {
	auth := NewBearerAuth("secret")
	headers := http.Header{}
	auth.AddAuthHeaders(headers)
	if headers.Get("Authorization") != "Bearer secret" {
		t.Fatalf("unexpected auth header: %q", headers.Get("Authorization"))
	}
}

func TestBearerAuthApplyDoesNotMutateInput(t *testing.T) {
	auth := NewBearerAuth("secret")
	req := client.NewRequest("POST", "https://x")
	out, aerr := auth.ApplyAuth(context.Background(), req)
	if aerr != nil {
		t.Fatalf("apply: %v", aerr)
	}
	if out.Headers.Get("Authorization") != "Bearer secret" {
		t.Fatalf("expected auth on output")
	}
	if req.Headers.Get("Authorization") != "" {
		t.Fatalf("input request was mutated")
	}
}

func TestNoOpAuthAddsNothing(t *testing.T) {
	headers := http.Header{}
	NoOpAuth{}.AddAuthHeaders(headers)
	if len(headers) != 0 {
		t.Fatalf("expected no headers")
	}
}

func TestAuthHeaderTelemetry(t *testing.T) {
	tel := AuthHeaderTelemetryFor(NewBearerAuth("x"))
	if !tel.Attached || tel.Name != "authorization" {
		t.Fatalf("unexpected telemetry: %+v", tel)
	}
	tel = AuthHeaderTelemetryFor(NoOpAuth{})
	if tel.Attached {
		t.Fatalf("expected not attached for noop")
	}
}

func TestAuthErrorToTransportError(t *testing.T) {
	build := &AuthError{Kind: AuthErrorBuild, Message: "boom"}
	te := build.ToTransportError()
	if te.Kind != client.TransportErrorBuild {
		t.Fatalf("expected build transport error, got %+v", te)
	}
	transient := &AuthError{Kind: AuthErrorTransient, Message: "later"}
	if transient.ToTransportError().Kind != client.TransportErrorNetwork {
		t.Fatalf("expected network transport error")
	}
}
