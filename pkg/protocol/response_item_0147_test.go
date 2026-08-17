package protocol

import (
	"encoding/json"
	"testing"
)

// The two ResponseItem variants added by upstream 0.147 (spec 50 D0.6).
func TestResponseItemAgentMessageRoundTrip(t *testing.T) {
	id := "msg_01"
	item := ResponseItem{
		Type:      ResponseItemKindAgentMessage,
		ItemID:    &id,
		Author:    "/root",
		Recipient: "/root/researcher",
		AgentMessageContent: []AgentMessageInputContent{
			{Type: AgentMessageInputContentKindInputText, Text: "hi"},
			{Type: AgentMessageInputContentKindEncryptedContent, EncryptedContent: "enc"},
		},
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"author":"/root","content":[{"text":"hi","type":"input_text"},{"encrypted_content":"enc","type":"encrypted_content"}],"id":"msg_01","recipient":"/root/researcher","type":"agent_message"}`
	if string(raw) != want {
		t.Fatalf("agent_message json:\n got %s\nwant %s", raw, want)
	}
	var back ResponseItem
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Type != ResponseItemKindAgentMessage || back.ItemID == nil || *back.ItemID != id || back.Author != "/root" || back.Recipient != "/root/researcher" || len(back.AgentMessageContent) != 2 {
		t.Fatalf("round trip = %+v", back)
	}
	if _, ok := PlaintextAgentMessageContent(back.AgentMessageContent); ok {
		t.Fatalf("encrypted part should make plaintext unavailable")
	}
	text, ok := PlaintextAgentMessageContent([]AgentMessageInputContent{{Type: AgentMessageInputContentKindInputText, Text: "a"}, {Type: AgentMessageInputContentKindInputText, Text: "b"}})
	if !ok || text != "a\nb" {
		t.Fatalf("plaintext = %q,%v want a\\nb,true", text, ok)
	}
	if _, ok := PlaintextAgentMessageContent([]AgentMessageInputContent{{Type: AgentMessageInputContentKindInputText, Text: "  "}}); ok {
		t.Fatalf("blank text should be reported as absent")
	}
	// Without an id the key is omitted (skip_serializing_if = Option::is_none).
	item.ItemID = nil
	raw, _ = json.Marshal(item)
	var probe map[string]json.RawMessage
	_ = json.Unmarshal(raw, &probe)
	if _, ok := probe["id"]; ok {
		t.Fatalf("nil id should be omitted: %s", raw)
	}
}

func TestResponseItemAdditionalToolsRoundTrip(t *testing.T) {
	item := ResponseItem{
		Type:  ResponseItemKindAdditionalTools,
		Role:  "developer",
		Tools: []json.RawMessage{json.RawMessage(`{"type":"function","name":"x"}`)},
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"role":"developer","tools":[{"type":"function","name":"x"}],"type":"additional_tools"}`
	if string(raw) != want {
		t.Fatalf("additional_tools json:\n got %s\nwant %s", raw, want)
	}
	var back ResponseItem
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Type != ResponseItemKindAdditionalTools || back.Role != "developer" || len(back.Tools) != 1 {
		t.Fatalf("round trip = %+v", back)
	}
}
