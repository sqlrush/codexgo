package features

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// marshalEvent encodes a protocol.Event to JSON using the protocol package's
// own (Un)MarshalJSON machinery.
func marshalEvent(e *protocol.Event) ([]byte, error) {
	return json.Marshal(e)
}

func TestUnstableWarningEventOnlyMentionsEnabledUnderDevelopment(t *testing.T) {
	configured := map[string]any{
		"child_agents_md": true,
		"personality":     true,
		"unknown":         true,
	}

	f := NewFeaturesWithDefaults()
	f.Enable(FeatureChildAgentsMd)

	event, ok := UnstableFeaturesWarningEvent(configured, false, &f, "/tmp/config.toml")
	if !ok || event == nil {
		t.Fatal("expected warning event")
	}
	if event.Msg.Type != protocol.EventMsgKindWarning {
		t.Fatalf("event type = %v", event.Msg.Type)
	}
	if event.Msg.Warning == nil {
		t.Fatal("warning payload missing")
	}
	msg := event.Msg.Warning.Message
	if !strings.Contains(msg, "child_agents_md") {
		t.Errorf("message should mention child_agents_md: %q", msg)
	}
	if strings.Contains(msg, "personality") {
		t.Errorf("message should not mention personality: %q", msg)
	}
	if !strings.Contains(msg, "/tmp/config.toml") {
		t.Errorf("message should mention config path: %q", msg)
	}
}

func TestUnstableWarningSuppressed(t *testing.T) {
	configured := map[string]any{"child_agents_md": true}
	f := NewFeaturesWithDefaults()
	f.Enable(FeatureChildAgentsMd)
	if _, ok := UnstableFeaturesWarningEvent(configured, true, &f, "/x"); ok {
		t.Error("suppressed warning should return nothing")
	}
}

func TestUnstableWarningNoneWhenNothingEnabled(t *testing.T) {
	f := NewFeaturesWithDefaults()
	if _, ok := UnstableFeaturesWarningEvent(nil, false, &f, "/x"); ok {
		t.Error("nil table should produce no warning")
	}
	// A stable feature toggled true must not warn.
	configured := map[string]any{"personality": true}
	if _, ok := UnstableFeaturesWarningEvent(configured, false, &f, "/x"); ok {
		t.Error("stable feature should not warn")
	}
}

func TestUnstableWarningSortedOrder(t *testing.T) {
	configured := map[string]any{
		"chronicle":       true,
		"child_agents_md": true,
	}
	f := NewFeaturesWithDefaults()
	f.Enable(FeatureChronicle)
	f.Enable(FeatureChildAgentsMd)
	event, ok := UnstableFeaturesWarningEvent(configured, false, &f, "/x")
	if !ok {
		t.Fatal("expected warning")
	}
	msg := event.Msg.Warning.Message
	// Sorted: child_agents_md before chronicle.
	if !strings.Contains(msg, "child_agents_md, chronicle") {
		t.Errorf("expected sorted join, got %q", msg)
	}
}

// roundTrip ensures the warning event serializes the way the protocol package
// emits warning events, locking in wire compatibility.
func TestWarningEventSerialization(t *testing.T) {
	configured := map[string]any{"child_agents_md": true}
	f := NewFeaturesWithDefaults()
	f.Enable(FeatureChildAgentsMd)
	event, ok := UnstableFeaturesWarningEvent(configured, false, &f, "/cfg")
	if !ok {
		t.Fatal("expected warning")
	}
	data, err := marshalEvent(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"type":"warning"`) {
		t.Errorf("serialized event missing warning type: %s", data)
	}
}
