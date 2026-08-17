package local

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
	"github.com/sqlrush/codexgo/pkg/threadstore"
)

// createPersistedThreadWithItems creates a live thread, appends the given
// conversation items (so it has a non-empty preview from the first user
// message), persists, and shuts the writer down so the rollout file is
// discoverable by the scan path. It returns the thread id and its rollout path.
func createPersistedThreadWithItems(t *testing.T, store *LocalThreadStore, n int, firstUser string, items ...rollout.RolloutItem) (protocol.ThreadID, string) {
	t.Helper()
	ctx := context.Background()
	threadID := protocol.NewThreadID(uuidFor(n))
	cwd := store.config.CodexHome

	if err := store.CreateThread(ctx, threadstore.CreateThreadParams{
		ThreadID: threadID,
		Source:   rollout.NewCliSource(),
		Metadata: threadstore.ThreadPersistenceMetadata{
			Cwd:           &cwd,
			ModelProvider: "test-provider",
			MemoryMode:    protocol.ThreadMemoryModeEnabled,
		},
	}); err != nil {
		t.Fatalf("create thread %d: %v", n, err)
	}

	all := make([]rollout.RolloutItem, 0, len(items)+1)
	all = append(all, rollout.NewEventMsgItem(protocol.EventMsg{
		Type:        protocol.EventMsgKindUserMessage,
		UserMessage: &protocol.UserMessageEvent{Message: firstUser},
	}))
	all = append(all, items...)

	if err := store.AppendItems(ctx, threadstore.AppendThreadItemsParams{ThreadID: threadID, Items: all}); err != nil {
		t.Fatalf("append items %d: %v", n, err)
	}
	if err := store.PersistThread(ctx, threadID); err != nil {
		t.Fatalf("persist thread %d: %v", n, err)
	}
	if err := store.FlushThread(ctx, threadID); err != nil {
		t.Fatalf("flush thread %d: %v", n, err)
	}
	path, err := store.LiveRolloutPath(threadID)
	if err != nil {
		t.Fatalf("live rollout path %d: %v", n, err)
	}
	if err := store.ShutdownThread(ctx, threadID); err != nil {
		t.Fatalf("shutdown thread %d: %v", n, err)
	}
	return threadID, path
}

// agentMessage builds an agent-message rollout item.
func agentMessage(text string) rollout.RolloutItem {
	return rollout.NewEventMsgItem(protocol.EventMsg{
		Type:         protocol.EventMsgKindAgentMessage,
		AgentMessage: &protocol.AgentMessageEvent{Message: text},
	})
}

// assistantResponseMessage builds an assistant-role response Message item with a
// single output_text content part.
func assistantResponseMessage(text string) rollout.RolloutItem {
	return rollout.NewResponseItem(protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "assistant",
		Content: []protocol.ContentItem{{Type: protocol.ContentItemKindOutputText, Text: text}},
	})
}

// turnContextItem builds a (non-conversation) turn-context rollout item carrying
// the given cwd, used to prove a raw-transcript match that is NOT conversation
// text yields no snippet (and thus no result). TurnContextItem re-emits its Raw
// payload verbatim, so we supply a minimal raw object.
func turnContextItem(cwd string) rollout.RolloutItem {
	raw := json.RawMessage(`{"cwd":` + mustJSON(cwd) + `}`)
	return rollout.NewTurnContextItem(rollout.TurnContextItem{Cwd: cwd, Raw: raw})
}

// mustJSON encodes v as a compact JSON string for embedding in a raw payload.
func mustJSON(v string) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestSearchThreadsMatchesAgentMessage verifies a term appearing only in an
// AGENT message body is found — the transcript-scan behavior the prior
// substring-only port (title/preview/cwd/first-user-message) missed.
func TestSearchThreadsMatchesAgentMessage(t *testing.T) {
	store := newScanStore(t)
	ctx := context.Background()

	createPersistedThreadWithItems(t, store, 101, "what is the capital of france",
		agentMessage("The capital of France is Paris, a lovely city."))

	page, err := store.SearchThreads(ctx, threadstore.SearchThreadsParams{PageSize: 10, SearchTerm: "Paris"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("results = %d, want 1", len(page.Items))
	}
	if !strings.Contains(page.Items[0].Snippet, "Paris") {
		t.Fatalf("snippet should contain the match, got %q", page.Items[0].Snippet)
	}
}

// TestSearchThreadsMatchesAssistantResponseItem verifies a term in an
// assistant-role response Message item is searched.
func TestSearchThreadsMatchesAssistantResponseItem(t *testing.T) {
	store := newScanStore(t)
	ctx := context.Background()

	createPersistedThreadWithItems(t, store, 102, "hello there",
		assistantResponseMessage("Here is the answer: quetzalcoatl appears here."))

	page, err := store.SearchThreads(ctx, threadstore.SearchThreadsParams{PageSize: 10, SearchTerm: "quetzalcoatl"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("results = %d, want 1", len(page.Items))
	}
}

// TestSearchThreadsMatchInNonConversationFieldYieldsNoResult verifies the
// snippet gate: a term that appears in the RAW transcript only inside a
// non-conversation item (turn_context cwd) matches the path but yields no
// conversation-text snippet, so the thread is excluded — mirroring codex, which
// requires first_rollout_content_match_snippet to succeed.
func TestSearchThreadsMatchInNonConversationFieldYieldsNoResult(t *testing.T) {
	store := newScanStore(t)
	ctx := context.Background()

	// The term "zzmagicpath" appears only in the turn_context cwd, never in any
	// user/agent/assistant message text.
	createPersistedThreadWithItems(t, store, 103, "ordinary user prompt",
		turnContextItem("/home/user/zzmagicpath/project"),
		agentMessage("an ordinary reply with no magic word"))

	// Sanity: the raw path scan DOES match (proving the gate, not a miss).
	matches, err := searchRolloutPaths(store.config.CodexHome, false, "zzmagicpath")
	if err != nil {
		t.Fatalf("searchRolloutPaths: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("raw path scan should match 1 file, got %d", len(matches))
	}

	page, err := store.SearchThreads(ctx, threadstore.SearchThreadsParams{PageSize: 10, SearchTerm: "zzmagicpath"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("results = %d, want 0 (no conversation-text snippet)", len(page.Items))
	}
}

// TestSearchThreadsCaseInsensitive verifies the match is case-insensitive in
// both the path scan and the snippet extraction.
func TestSearchThreadsCaseInsensitive(t *testing.T) {
	store := newScanStore(t)
	ctx := context.Background()

	createPersistedThreadWithItems(t, store, 104, "Mixed Case Needle here",
		agentMessage("reply body"))

	for _, term := range []string{"needle", "NEEDLE", "NeEdLe"} {
		page, err := store.SearchThreads(ctx, threadstore.SearchThreadsParams{PageSize: 10, SearchTerm: term})
		if err != nil {
			t.Fatalf("search %q: %v", term, err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("term %q results = %d, want 1", term, len(page.Items))
		}
	}
}

// TestSearchThreadsEmptyTermErrors verifies an empty term is rejected with an
// InvalidRequest error, mirroring the Rust guard.
func TestSearchThreadsEmptyTermErrors(t *testing.T) {
	store := newScanStore(t)
	_, err := store.SearchThreads(context.Background(), threadstore.SearchThreadsParams{PageSize: 10, SearchTerm: ""})
	var storeErr *threadstore.Error
	if !errors.As(err, &storeErr) || storeErr.Kind != threadstore.ErrorKindInvalidRequest {
		t.Fatalf("expected InvalidRequest, got %v", err)
	}
}

// TestSearchThreadsPagingAndOrdering verifies results are returned in sort order
// with overflow detection producing a next cursor that continues the search.
func TestSearchThreadsPagingAndOrdering(t *testing.T) {
	store := newScanStore(t)
	ctx := context.Background()

	// Three threads each containing the shared term in their first user message,
	// created in order so updated_at increases with n.
	for n := 111; n <= 113; n++ {
		createPersistedThreadWithItems(t, store, n, "shared sharedterm message", agentMessage("ack"))
	}

	first, err := store.SearchThreads(ctx, threadstore.SearchThreadsParams{
		PageSize:      2,
		SearchTerm:    "sharedterm",
		SortKey:       threadstore.ThreadSortKeyUpdatedAt,
		SortDirection: threadstore.SortDirectionDesc,
	})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Items) != 2 {
		t.Fatalf("first page results = %d, want 2", len(first.Items))
	}
	if first.NextCursor == nil {
		t.Fatalf("expected a next cursor with 3 matches and page size 2")
	}
	// Descending updated_at order within the page.
	for i := 1; i < len(first.Items); i++ {
		prev := sortTimestamp(first.Items[i-1].Thread, threadstore.ThreadSortKeyUpdatedAt)
		cur := sortTimestamp(first.Items[i].Thread, threadstore.ThreadSortKeyUpdatedAt)
		if cur.After(prev) {
			t.Fatalf("descending order violated at %d", i)
		}
	}

	second, err := store.SearchThreads(ctx, threadstore.SearchThreadsParams{
		PageSize:      2,
		Cursor:        first.NextCursor,
		SearchTerm:    "sharedterm",
		SortKey:       threadstore.ThreadSortKeyUpdatedAt,
		SortDirection: threadstore.SortDirectionDesc,
	})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Items) != 1 {
		t.Fatalf("second page results = %d, want 1", len(second.Items))
	}
	if second.NextCursor != nil {
		t.Fatalf("second page should be the last page")
	}
}

// TestSearchThreadsBadCursorErrors verifies an unparseable cursor is rejected.
func TestSearchThreadsBadCursorErrors(t *testing.T) {
	store := newScanStore(t)
	bad := "not-a-cursor"
	_, err := store.SearchThreads(context.Background(), threadstore.SearchThreadsParams{
		PageSize:   10,
		SearchTerm: "x",
		Cursor:     &bad,
	})
	var storeErr *threadstore.Error
	if !errors.As(err, &storeErr) || storeErr.Kind != threadstore.ErrorKindInvalidRequest {
		t.Fatalf("expected InvalidRequest for bad cursor, got %v", err)
	}
}

// TestSearchThreadsNoMatch verifies an absent term returns an empty page.
func TestSearchThreadsNoMatch(t *testing.T) {
	store := newScanStore(t)
	ctx := context.Background()
	createPersistedThreadWithItems(t, store, 121, "the quick brown fox", agentMessage("jumped over"))

	page, err := store.SearchThreads(ctx, threadstore.SearchThreadsParams{PageSize: 10, SearchTerm: "absenttoken"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("results = %d, want 0", len(page.Items))
	}
	if page.NextCursor != nil {
		t.Fatalf("empty result should have no next cursor")
	}
}

// --- unit tests for the snippet/excerpt helpers (mirrored Rust functions) ---

// TestExcerptAroundMatch verifies the excerpt window, whitespace normalization,
// and ellipsis markers, mirroring rollout/src/search.rs::excerpt_around_match.
func TestExcerptAroundMatch(t *testing.T) {
	matcher := regexp.MustCompile("(?i)" + regexp.QuoteMeta("needle"))

	t.Run("short text no ellipsis", func(t *testing.T) {
		got, ok := excerptAroundMatch("a needle here", matcher)
		if !ok {
			t.Fatalf("expected a match")
		}
		if got != "a needle here" {
			t.Fatalf("snippet = %q, want %q", got, "a needle here")
		}
	})

	t.Run("normalizes whitespace", func(t *testing.T) {
		got, ok := excerptAroundMatch("a\n\t needle    here", matcher)
		if !ok {
			t.Fatalf("expected a match")
		}
		if got != "a needle here" {
			t.Fatalf("snippet = %q, want %q", got, "a needle here")
		}
	})

	t.Run("leading and trailing ellipsis", func(t *testing.T) {
		before := strings.Repeat("x", 100)
		after := strings.Repeat("y", 200)
		text := before + " needle " + after
		got, ok := excerptAroundMatch(text, matcher)
		if !ok {
			t.Fatalf("expected a match")
		}
		if !strings.HasPrefix(got, "... ") {
			t.Fatalf("snippet should start with ellipsis: %q", got)
		}
		if !strings.HasSuffix(got, " ...") {
			t.Fatalf("snippet should end with ellipsis: %q", got)
		}
		if !strings.Contains(got, "needle") {
			t.Fatalf("snippet should contain the match: %q", got)
		}
	})

	t.Run("no match", func(t *testing.T) {
		if _, ok := excerptAroundMatch("nothing here", matcher); ok {
			t.Fatalf("expected no match")
		}
	})
}

// TestJSONEscapedSearchTerm verifies the term is escaped to its in-JSON-string
// form, mirroring rollout/src/search.rs::json_escaped_search_term.
func TestJSONEscapedSearchTerm(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain", "plain"},
		{`a "quote"`, `a \"quote\"`},
		{"tab\there", `tab\there`},
		{"new\nline", `new\nline`},
		{`back\slash`, `back\\slash`},
	}
	for _, tt := range tests {
		if got := jsonEscapedSearchTerm(tt.in); got != tt.want {
			t.Errorf("jsonEscapedSearchTerm(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestStripUserMessagePrefix verifies the USER_MESSAGE_BEGIN marker is stripped,
// mirroring rollout/src/search.rs::strip_user_message_prefix.
func TestStripUserMessagePrefix(t *testing.T) {
	withMarker := "some preamble " + userMessageBegin + "\n  the real request  "
	if got := stripUserMessagePrefix(withMarker); got != "the real request" {
		t.Errorf("strip = %q, want %q", got, "the real request")
	}
	if got := stripUserMessagePrefix("  plain message  "); got != "plain message" {
		t.Errorf("strip plain = %q, want %q", got, "plain message")
	}
}

// TestConversationTextFromItem verifies which item kinds yield conversation
// text, mirroring rollout/src/search.rs::conversation_text_from_item.
func TestConversationTextFromItem(t *testing.T) {
	t.Run("user event", func(t *testing.T) {
		text, ok := conversationTextFromItem(rollout.NewEventMsgItem(protocol.EventMsg{
			Type:        protocol.EventMsgKindUserMessage,
			UserMessage: &protocol.UserMessageEvent{Message: userMessageBegin + " hi"},
		}))
		if !ok || text != "hi" {
			t.Fatalf("user event = (%q, %v), want (%q, true)", text, ok, "hi")
		}
	})

	t.Run("agent event", func(t *testing.T) {
		text, ok := conversationTextFromItem(agentMessage("  reply  "))
		if !ok || text != "reply" {
			t.Fatalf("agent event = (%q, %v), want (%q, true)", text, ok, "reply")
		}
	})

	t.Run("assistant response item", func(t *testing.T) {
		text, ok := conversationTextFromItem(assistantResponseMessage("answer"))
		if !ok || text != "answer" {
			t.Fatalf("assistant response = (%q, %v), want (%q, true)", text, ok, "answer")
		}
	})

	t.Run("system response item skipped", func(t *testing.T) {
		item := rollout.NewResponseItem(protocol.ResponseItem{
			Type:    protocol.ResponseItemKindMessage,
			Role:    "system",
			Content: []protocol.ContentItem{{Type: protocol.ContentItemKindInputText, Text: "ignored"}},
		})
		if _, ok := conversationTextFromItem(item); ok {
			t.Fatalf("system-role response item should not yield conversation text")
		}
	})

	t.Run("turn_context skipped", func(t *testing.T) {
		if _, ok := conversationTextFromItem(turnContextItem("/cwd")); ok {
			t.Fatalf("turn_context should not yield conversation text")
		}
	})
}
