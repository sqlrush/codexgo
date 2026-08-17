package rollout

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func testConfig(home string) RolloutConfig {
	return RolloutConfig{
		CodexHomeDir:       home,
		CwdDir:             home,
		ModelProvider:      "test-provider",
		GenerateMemoriesOn: true,
	}
}

func newTestRecorder(t *testing.T, home string) *RolloutRecorder {
	t.Helper()
	rec, err := NewRecorderForCreate(context.Background(), testConfig(home), CreateParams{
		ConversationID:   protocol.NewThreadID("5973b6c0-94b8-487b-a530-2aeb6098ae0e"),
		Source:           NewExecSource(),
		BaseInstructions: BaseInstructions{Text: "base"},
		Originator:       "test_originator",
		CliVersion:       "test_version",
	})
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}
	return rec
}

func agentMessageItem(message string) RolloutItem {
	ev := protocol.EventMsg{
		Type:         protocol.EventMsgKindAgentMessage,
		AgentMessage: &protocol.AgentMessageEvent{Message: message},
	}
	return NewEventMsgItem(ev)
}

func userMessageItem(message string) RolloutItem {
	ev := protocol.EventMsg{
		Type:        protocol.EventMsgKindUserMessage,
		UserMessage: &protocol.UserMessageEvent{Message: message},
	}
	return NewEventMsgItem(ev)
}

func TestRecorderMaterializesOnFlushWithPendingItems(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	rec := newTestRecorder(t, home)
	path := rec.RolloutPath()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rollout file should not exist before first recordable item")
	}

	if err := rec.RecordCanonicalItems(ctx, []RolloutItem{agentMessageItem("buffered-event")}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := rec.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("flush with pending items should materialize the rollout: %v", err)
	}

	if err := rec.RecordCanonicalItems(ctx, []RolloutItem{userMessageItem("first-user-message")}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := rec.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}

	if err := rec.Persist(ctx); err != nil {
		t.Fatalf("persist: %v", err)
	}
	// Idempotent second persist.
	if err := rec.Persist(ctx); err != nil {
		t.Fatalf("persist (second): %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"type":"session_meta"`) {
		t.Fatalf("expected session metadata in rollout:\n%s", text)
	}
	bufferedIdx := strings.Index(text, "buffered-event")
	userIdx := strings.Index(text, "first-user-message")
	if bufferedIdx < 0 || userIdx < 0 {
		t.Fatalf("expected both events in rollout:\n%s", text)
	}
	if bufferedIdx >= userIdx {
		t.Fatalf("buffered items should preserve ordering")
	}

	textAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(textAfter) != text {
		t.Fatalf("second persist should be idempotent")
	}

	if err := rec.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestRecorderSessionMetaIsFirstLine(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	rec := newTestRecorder(t, home)

	if err := rec.RecordCanonicalItems(ctx, []RolloutItem{agentMessageItem("hello")}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := rec.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := rec.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	data, err := os.ReadFile(rec.RolloutPath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d:\n%s", len(lines), data)
	}
	var first RolloutLine
	if err := first.UnmarshalJSON([]byte(lines[0])); err != nil {
		t.Fatalf("parse first line: %v", err)
	}
	if first.Item.Kind != RolloutItemKindSessionMeta {
		t.Fatalf("first line should be session_meta, got %q", first.Item.Kind)
	}
	if first.Item.SessionMeta.Meta.Originator != "test_originator" {
		t.Fatalf("originator mismatch: %q", first.Item.SessionMeta.Meta.Originator)
	}
}

func TestRecorderResumeAppends(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	rec := newTestRecorder(t, home)
	if err := rec.RecordCanonicalItems(ctx, []RolloutItem{agentMessageItem("first")}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := rec.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	path := rec.RolloutPath()
	if err := rec.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	resumed, err := NewRecorderForResume(ctx, path)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if err := resumed.RecordCanonicalItems(ctx, []RolloutItem{agentMessageItem("second")}); err != nil {
		t.Fatalf("record on resumed: %v", err)
	}
	if err := resumed.Flush(ctx); err != nil {
		t.Fatalf("flush resumed: %v", err)
	}
	if err := resumed.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown resumed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "first") || !strings.Contains(text, "second") {
		t.Fatalf("resume should append, content:\n%s", text)
	}
}

func TestRecorderFilenameLayout(t *testing.T) {
	// Pin the local clock so the filename is deterministic.
	prev := nowLocal
	nowLocal = func() time.Time {
		return time.Date(2025, 5, 7, 17, 24, 21, 0, time.UTC)
	}
	defer func() { nowLocal = prev }()

	home := t.TempDir()
	rec := newTestRecorder(t, home)
	want := filepath.Join(home, "sessions", "2025", "05", "07",
		"rollout-2025-05-07T17-24-21-5973b6c0-94b8-487b-a530-2aeb6098ae0e.jsonl")
	if rec.RolloutPath() != want {
		t.Fatalf("rollout path = %q, want %q", rec.RolloutPath(), want)
	}
	_ = rec.Shutdown(context.Background())
}

func TestRecorderMemoryModeDisabledWhenMemoriesOff(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	config := testConfig(home)
	config.GenerateMemoriesOn = false
	rec, err := NewRecorderForCreate(ctx, config, CreateParams{
		ConversationID:   protocol.NewThreadID("5973b6c0-94b8-487b-a530-2aeb6098ae0e"),
		Source:           NewExecSource(),
		BaseInstructions: BaseInstructions{Text: "base"},
		Originator:       "o",
		CliVersion:       "v",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := rec.RecordCanonicalItems(ctx, []RolloutItem{agentMessageItem("x")}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := rec.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := rec.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	meta, err := ReadSessionMetaLine(rec.RolloutPath())
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if meta.Meta.MemoryMode == nil || *meta.Meta.MemoryMode != "disabled" {
		t.Fatalf("memory_mode should be disabled, got %v", meta.Meta.MemoryMode)
	}
}
