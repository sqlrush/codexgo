package core

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
)

// This file tests the inline auto-compaction port (event_compact.go +
// event_compact_helpers.go): the pure history-rebuild helpers and the end-to-end
// streaming compaction turn (history replacement + ContextCompacted/Warning
// events + the `compacted` rollout line).

// ---------------------------------------------------------------------------
// test helpers
// ---------------------------------------------------------------------------

// cUserMsg builds a user-role response item with a single input-text content
// item. Named to avoid colliding with the rollout-flavored userMsg helper in
// thread_manager_test.go.
func cUserMsg(text string) protocol.ResponseItem {
	return protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "user",
		Content: []protocol.ContentItem{{Type: protocol.ContentItemKindInputText, Text: text}},
	}
}

// cAsstMsg builds an assistant-role response item with output text.
func cAsstMsg(text string) protocol.ResponseItem {
	return protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "assistant",
		Content: []protocol.ContentItem{{Type: protocol.ContentItemKindOutputText, Text: text}},
	}
}

// itemUserText returns the input-text of a user-role response item, or "".
func itemUserText(item protocol.ResponseItem) string {
	if item.Type != protocol.ResponseItemKindMessage {
		return ""
	}
	var b strings.Builder
	for _, c := range item.Content {
		if c.Type == protocol.ContentItemKindInputText {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// newCompactSession builds a Session wired with the given model client and
// recorder, a threadID, and a buffered event queue, suitable for driving a full
// compaction turn.
func newCompactSession(t *testing.T, client ModelClient, rec RolloutRecorder, history ...protocol.ResponseItem) (*Session, <-chan protocol.Event) {
	t.Helper()
	events := make(chan protocol.Event, 64)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := &Session{
		threadID:    protocol.NewThreadID("00000000-0000-0000-0000-000000000001"),
		txEvent:     events,
		state:       NewSessionState(SessionConfiguration{}),
		agentStatus: protocol.AgentStatus{Kind: protocol.AgentStatusPendingInit},
		ctx:         ctx,
		cancel:      cancel,
		services: SessionServices{
			ModelClient:     client,
			RolloutRecorder: rec,
		},
	}
	if len(history) > 0 {
		s.RecordItems(history)
	}
	s.setActiveTurn(NewActiveTurn())
	return s, events
}

// eventKinds projects events to their EventMsg kinds, in order.
func eventKinds(events []protocol.Event) []protocol.EventMsgKind {
	out := make([]protocol.EventMsgKind, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Msg.Type)
	}
	return out
}

// completedTurn scripts a single model turn whose summary is an assistant message
// plus a Completed event carrying the usage.
func completedTurn(summary string, usage *protocol.TokenUsage) MockTurn {
	return MockTurn{Events: []api.ResponseEvent{
		{Kind: api.ResponseEventOutputItemDone, Item: ptrItem(cAsstMsg(summary))},
		{Kind: api.ResponseEventCompleted, TokenUsage: usage},
	}}
}

func ptrItem(item protocol.ResponseItem) *protocol.ResponseItem { return &item }

// ---------------------------------------------------------------------------
// pure helper tests
// ---------------------------------------------------------------------------

func TestIsSummaryMessage(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"summary", SummaryPrefix + "\nrecap", true},
		{"prefix only no newline", SummaryPrefix, false},
		{"plain", "hello", false},
		{"empty", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSummaryMessage(tc.in); got != tc.want {
				t.Fatalf("isSummaryMessage(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestCollectUserMessages(t *testing.T) {
	items := []protocol.ResponseItem{
		cUserMsg("first"),
		cAsstMsg("answer"),
		cUserMsg("second"),
		cUserMsg(SummaryPrefix + "\nold summary"), // excluded
		{Type: protocol.ResponseItemKindMessage, Role: "system",
			Content: []protocol.ContentItem{{Type: protocol.ContentItemKindInputText, Text: "sys"}}}, // excluded
	}
	got := collectUserMessages(items)
	want := []string{"first", "second"}
	if len(got) != len(want) {
		t.Fatalf("collectUserMessages len = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("collectUserMessages[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGetLastAssistantMessageFromTurn(t *testing.T) {
	tests := []struct {
		name  string
		items []protocol.ResponseItem
		want  string
	}{
		{
			name:  "last assistant wins",
			items: []protocol.ResponseItem{cAsstMsg("one"), cUserMsg("u"), cAsstMsg("two")},
			want:  "two",
		},
		{
			name:  "skip blank assistant",
			items: []protocol.ResponseItem{cAsstMsg("real"), cAsstMsg("   ")},
			want:  "real",
		},
		{
			name:  "none",
			items: []protocol.ResponseItem{cUserMsg("u")},
			want:  "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := getLastAssistantMessageFromTurn(tc.items); got != tc.want {
				t.Fatalf("getLastAssistantMessageFromTurn = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildCompactedHistoryWithLimit(t *testing.T) {
	t.Run("appends users then summary in order", func(t *testing.T) {
		got := buildCompactedHistoryWithLimit(nil, []string{"u1", "u2"}, "SUMMARY", 1000)
		// Expect: u1, u2, SUMMARY (each a user message).
		if len(got) != 3 {
			t.Fatalf("got %d items, want 3: %#v", len(got), got)
		}
		wants := []string{"u1", "u2", "SUMMARY"}
		for i, w := range wants {
			if got[i].Role != "user" {
				t.Fatalf("item %d role = %q, want user", i, got[i].Role)
			}
			if txt := itemUserText(got[i]); txt != w {
				t.Fatalf("item %d text = %q, want %q", i, txt, w)
			}
		}
	})

	t.Run("zero budget keeps only summary", func(t *testing.T) {
		got := buildCompactedHistoryWithLimit(nil, []string{"u1", "u2"}, "S", 0)
		if len(got) != 1 || itemUserText(got[0]) != "S" {
			t.Fatalf("got %#v, want single summary item", got)
		}
	})

	t.Run("empty summary becomes placeholder", func(t *testing.T) {
		got := buildCompactedHistoryWithLimit(nil, nil, "", 1000)
		if len(got) != 1 || itemUserText(got[0]) != noSummaryAvailablePlaceholder {
			t.Fatalf("got %#v, want placeholder", got)
		}
	})

	t.Run("budget keeps newest messages", func(t *testing.T) {
		// Each message ~ len/4 tokens. "aaaa" = 1 token. Budget of 1 keeps only the
		// newest whole message.
		got := buildCompactedHistoryWithLimit(nil, []string{"aaaa", "bbbb"}, "S", 1)
		// Newest ("bbbb") fits; "aaaa" is dropped (budget exhausted).
		var texts []string
		for _, it := range got {
			texts = append(texts, itemUserText(it))
		}
		want := []string{"bbbb", "S"}
		if len(texts) != len(want) {
			t.Fatalf("texts = %v, want %v", texts, want)
		}
		for i := range want {
			if texts[i] != want[i] {
				t.Fatalf("texts[%d] = %q, want %q", i, texts[i], want[i])
			}
		}
	})

	t.Run("prepends initial context", func(t *testing.T) {
		initial := []protocol.ResponseItem{developerTextMessage("dev")}
		got := buildCompactedHistoryWithLimit(initial, []string{"u"}, "S", 1000)
		if len(got) != 3 || got[0].Role != "developer" {
			t.Fatalf("got %#v, want [dev, u, S]", got)
		}
	})
}

func TestInsertInitialContextBeforeLastRealUserOrSummary(t *testing.T) {
	initial := []protocol.ResponseItem{developerTextMessage("CTX")}

	t.Run("before last real user", func(t *testing.T) {
		compacted := []protocol.ResponseItem{
			cUserMsg("u1"),
			cUserMsg("u2"),
			cUserMsg(SummaryPrefix + "\nsum"),
		}
		got := insertInitialContextBeforeLastRealUserOrSummary(compacted, initial)
		// CTX should be spliced before u2 (the last real user message).
		// Result: u1, CTX, u2, summary.
		if len(got) != 4 {
			t.Fatalf("got %d items, want 4", len(got))
		}
		if got[1].Role != "developer" || itemUserText(got[2]) != "u2" {
			t.Fatalf("CTX not before last real user: %#v", got)
		}
	})

	t.Run("before summary when no real user", func(t *testing.T) {
		compacted := []protocol.ResponseItem{
			cUserMsg(SummaryPrefix + "\nsum"),
		}
		got := insertInitialContextBeforeLastRealUserOrSummary(compacted, initial)
		// CTX before the summary so the summary stays last.
		if len(got) != 2 || got[0].Role != "developer" {
			t.Fatalf("CTX not before summary: %#v", got)
		}
		if !isSummaryMessage(itemUserText(got[1])) {
			t.Fatalf("summary not last: %#v", got)
		}
	})

	t.Run("append when no user or compaction items", func(t *testing.T) {
		compacted := []protocol.ResponseItem{cAsstMsg("a")}
		got := insertInitialContextBeforeLastRealUserOrSummary(compacted, initial)
		if len(got) != 2 || got[1].Role != "developer" {
			t.Fatalf("CTX not appended: %#v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// end-to-end compaction tests
// ---------------------------------------------------------------------------

func TestRunCompactTaskImpl_ReplacesHistoryAndEmitsEvents(t *testing.T) {
	usage := &protocol.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}
	client := NewMockModelClient("gpt-test", nil, completedTurn("the summary", usage))
	rec := &recordingRecorder{}
	sess, events := newCompactSession(t, client, rec,
		cUserMsg("hello"),
		cAsstMsg("hi there"),
		cUserMsg("do the thing"),
	)
	tc := &TurnContext{SubID: "turn-1", Cwd: "/work"}

	input := []protocol.UserInput{{Type: protocol.UserInputKindText, Text: "please summarize"}}
	suffix, err := runCompactTaskImpl(context.Background(), sess, tc, input, DoNotInjectInitialContext)
	if err != nil {
		t.Fatalf("runCompactTaskImpl error: %v", err)
	}
	if suffix != "the summary" {
		t.Fatalf("summary suffix = %q, want %q", suffix, "the summary")
	}

	// History is replaced with: user messages (hello, do the thing) + summary.
	hist := sess.HistoryItems()
	if len(hist) == 0 {
		t.Fatal("history is empty after compaction")
	}
	last := hist[len(hist)-1]
	wantSummary := SummaryPrefix + "\nthe summary"
	if itemUserText(last) != wantSummary {
		t.Fatalf("last history item = %q, want %q", itemUserText(last), wantSummary)
	}
	// The original assistant message must be gone (compacted away).
	for _, it := range hist {
		if it.Role == "assistant" {
			t.Fatalf("assistant message survived compaction: %#v", it)
		}
	}
	// Recent user messages preserved, oldest-first.
	if itemUserText(hist[0]) != "hello" || itemUserText(hist[1]) != "do the thing" {
		t.Fatalf("recent user messages not preserved in order: %#v", hist)
	}

	// Events: ItemStarted (compaction), ItemCompleted, ContextCompacted, Warning.
	kinds := eventKinds(drainEvents(events))
	wantKinds := []protocol.EventMsgKind{
		protocol.EventMsgKindItemStarted,
		protocol.EventMsgKindItemCompleted,
		protocol.EventMsgKindContextCompacted,
		protocol.EventMsgKindWarning,
	}
	if !equalKinds(kinds, wantKinds) {
		t.Fatalf("event kinds = %v, want %v", kinds, wantKinds)
	}

	// The `compacted` rollout line is persisted with the replacement history.
	var compactedItems []rollout.CompactedItem
	for _, it := range rec.items() {
		if it.Kind == rollout.RolloutItemKindCompacted && it.Compacted != nil {
			compactedItems = append(compactedItems, *it.Compacted)
		}
	}
	if len(compactedItems) != 1 {
		t.Fatalf("got %d compacted rollout lines, want 1", len(compactedItems))
	}
	if compactedItems[0].Message != wantSummary {
		t.Fatalf("compacted message = %q, want %q", compactedItems[0].Message, wantSummary)
	}
	if compactedItems[0].ReplacementHistory == nil {
		t.Fatal("compacted replacement history is nil")
	}
}

func TestRunCompactTaskImpl_MidTurnInjectsInitialContext(t *testing.T) {
	client := NewMockModelClient("gpt-test", nil, completedTurn("recap", nil))
	rec := &recordingRecorder{}
	sess, _ := newCompactSession(t, client, rec, cUserMsg("question"))
	dev := "developer instructions"
	tc := &TurnContext{
		SubID:                 "turn-1",
		Cwd:                   "/work",
		DeveloperInstructions: &dev,
		// ToTurnContextItem marshals the approval policy, which has no valid zero
		// value; set an explicit policy so the reference turn-context line builds.
		ApprovalPolicy: protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest},
	}

	_, err := runCompactTaskImpl(context.Background(), sess, tc, nil, InjectBeforeLastUserMessage)
	if err != nil {
		t.Fatalf("runCompactTaskImpl error: %v", err)
	}

	hist := sess.HistoryItems()
	// Initial context (developer message) must be spliced before the last real
	// user message ("question").
	foundDev := false
	for i, it := range hist {
		if it.Role == "developer" {
			foundDev = true
			// The next real user message should follow.
			if i+1 < len(hist) && itemUserText(hist[i+1]) != "question" {
				t.Fatalf("developer context not before last real user: %#v", hist)
			}
		}
	}
	if !foundDev {
		t.Fatalf("developer initial context not injected: %#v", hist)
	}

	// A turn_context rollout line is persisted alongside the compacted line.
	var sawTurnContext bool
	for _, it := range rec.items() {
		if it.Kind == rollout.RolloutItemKindTurnContext {
			sawTurnContext = true
		}
	}
	if !sawTurnContext {
		t.Fatal("mid-turn compaction did not persist a turn_context rollout line")
	}
}

func TestRunCompactTaskImpl_ContextWindowExceededTrimsAndRetries(t *testing.T) {
	// First stream call fails with ContextWindowExceeded; second succeeds.
	failing := MockTurn{StreamErr: fmt.Errorf("model: %w", errContextWindowExceeded)}
	ok := completedTurn("trimmed summary", nil)
	client := NewMockModelClient("gpt-test", nil, failing, ok)
	rec := &recordingRecorder{}
	// Two history items so there is something to trim (turn_input_len > 1).
	sess, _ := newCompactSession(t, client, rec, cUserMsg("a"), cUserMsg("b"))
	tc := &TurnContext{SubID: "turn-1", Cwd: "/work"}

	suffix, err := runCompactTaskImpl(context.Background(), sess, tc, nil, DoNotInjectInitialContext)
	if err != nil {
		t.Fatalf("runCompactTaskImpl error: %v", err)
	}
	if suffix != "trimmed summary" {
		t.Fatalf("suffix = %q, want %q", suffix, "trimmed summary")
	}
	if client.CallCount() != 2 {
		t.Fatalf("model called %d times, want 2 (one trim retry)", client.CallCount())
	}
}

func TestRunCompactTaskImpl_ContextWindowExceededSingleItemFails(t *testing.T) {
	failing := MockTurn{StreamErr: fmt.Errorf("model: %w", errContextWindowExceeded)}
	client := NewMockModelClient("gpt-test", nil, failing)
	rec := &recordingRecorder{}
	// Single history item AND no prompt input -> only one item -> cannot trim.
	sess, events := newCompactSession(t, client, rec, cUserMsg("only"))
	tc := &TurnContext{SubID: "turn-1", Cwd: "/work"}

	_, err := runCompactTaskImpl(context.Background(), sess, tc, nil, DoNotInjectInitialContext)
	if err == nil {
		t.Fatal("expected context-window-exceeded error, got nil")
	}

	// An Error event is emitted to the client.
	kinds := eventKinds(drainEvents(events))
	sawError := false
	for _, k := range kinds {
		if k == protocol.EventMsgKindError {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("no Error event emitted; kinds = %v", kinds)
	}
}

func TestRunCompactTask_EmitsTurnStarted(t *testing.T) {
	client := NewMockModelClient("gpt-test", nil, completedTurn("s", nil))
	rec := &recordingRecorder{}
	sess, events := newCompactSession(t, client, rec, cUserMsg("u"))
	tc := &TurnContext{SubID: "turn-1", Cwd: "/work"}

	err := runCompactTask(context.Background(), sess, tc, nil)
	if err != nil {
		t.Fatalf("runCompactTask error: %v", err)
	}
	kinds := eventKinds(drainEvents(events))
	if len(kinds) == 0 || kinds[0] != protocol.EventMsgKindTurnStarted {
		t.Fatalf("first event = %v, want TurnStarted; kinds = %v", kinds, kinds)
	}
}

func TestRunInlineAutoCompactTask_UsesCompactPrompt(t *testing.T) {
	client := NewMockModelClient("gpt-test", nil, completedTurn("s", nil))
	rec := &recordingRecorder{}
	sess, _ := newCompactSession(t, client, rec, cUserMsg("u"))
	custom := "CUSTOM COMPACT PROMPT"
	tc := &TurnContext{SubID: "turn-1", Cwd: "/work", CompactPrompt: &custom}

	if err := runInlineAutoCompactTask(context.Background(), sess, tc, DoNotInjectInitialContext); err != nil {
		t.Fatalf("runInlineAutoCompactTask error: %v", err)
	}
	// The compaction prompt is appended to the cloned turn input; the model sees
	// it in its prompt. Assert the mock received a prompt containing the custom
	// compaction prompt text.
	prompts := client.ReceivedPrompts()
	if len(prompts) == 0 {
		t.Fatal("model received no prompts")
	}
	found := false
	for _, in := range prompts[0].Input {
		if itemUserText(in) == custom {
			found = true
		}
	}
	if !found {
		t.Fatalf("custom compact prompt not present in model input: %#v", prompts[0].Input)
	}
}

func TestShouldUseRemoteCompactTask_AlwaysFalse(t *testing.T) {
	if shouldUseRemoteCompactTask(&TurnContext{}) {
		t.Fatal("inline port must not select remote compaction")
	}
}

func TestBuildCompactedHistory_TruncatesOverflowingNewestMessage(t *testing.T) {
	// The newest message overflows the budget and must be truncated (not dropped);
	// older messages beyond the budget are omitted. With a 2-token budget, the
	// newest 12-char message (~3 tokens) is truncated to fit.
	long := "0123456789ab" // 12 bytes -> ~3 tokens
	got := buildCompactedHistoryWithLimit(nil, []string{"older", long}, "S", 2)
	// Expect: [truncated(long), S]. "older" is omitted (budget exhausted by the
	// truncated newest message).
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2: %#v", len(got), got)
	}
	truncated := itemUserText(got[0])
	if truncated == long {
		t.Fatalf("newest message was not truncated: %q", truncated)
	}
	if itemUserText(got[1]) != "S" {
		t.Fatalf("summary not last: %q", itemUserText(got[1]))
	}
}

func TestHandleCompactStreamEvent_RecordsSideEffects(t *testing.T) {
	client := NewMockModelClient("gpt-test", nil)
	rec := &recordingRecorder{}
	sess, _ := newCompactSession(t, client, rec)
	tc := &TurnContext{SubID: "turn-1", Cwd: "/work"}

	// ServerReasoningIncluded is recorded on state.
	done, err := handleCompactStreamEvent(sess, tc, api.ResponseEvent{
		Kind:              api.ResponseEventServerReasoningIncluded,
		ReasoningIncluded: true,
	})
	if err != nil || done {
		t.Fatalf("server-reasoning event: done=%v err=%v", done, err)
	}
	var reasoning bool
	sess.WithState(func(st *SessionState) { reasoning = st.ServerReasoningIncluded() })
	if !reasoning {
		t.Fatal("ServerReasoningIncluded not recorded")
	}

	// RateLimits is recorded on state.
	limit := "custom"
	done, err = handleCompactStreamEvent(sess, tc, api.ResponseEvent{
		Kind:       api.ResponseEventRateLimits,
		RateLimits: &protocol.RateLimitSnapshot{LimitID: &limit},
	})
	if err != nil || done {
		t.Fatalf("rate-limits event: done=%v err=%v", done, err)
	}
	var rl *protocol.RateLimitSnapshot
	sess.WithState(func(st *SessionState) { rl = st.LatestRateLimits() })
	if rl == nil {
		t.Fatal("rate limits not recorded")
	}

	// Completed with usage signals done and folds usage in.
	done, err = handleCompactStreamEvent(sess, tc, api.ResponseEvent{
		Kind:       api.ResponseEventCompleted,
		TokenUsage: &protocol.TokenUsage{TotalTokens: 7},
	})
	if err != nil || !done {
		t.Fatalf("completed event: done=%v err=%v", done, err)
	}
	var info *protocol.TokenUsageInfo
	sess.WithState(func(st *SessionState) { info = st.TokenInfo() })
	if info == nil || info.TotalTokenUsage.TotalTokens != 7 {
		t.Fatalf("token usage not folded in: %#v", info)
	}
}

// equalKinds reports whether two event-kind slices are equal.
func equalKinds(a, b []protocol.EventMsgKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
