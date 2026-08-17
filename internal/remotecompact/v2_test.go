package remotecompact

import (
	"context"
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

func phasedMessage(role, text string, phase *protocol.MessagePhase) protocol.ResponseItem {
	return protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    role,
		Content: []protocol.ContentItem{{Type: protocol.ContentItemKindInputText, Text: text}},
		Phase:   phase,
	}
}

func phasePtr(p protocol.MessagePhase) *protocol.MessagePhase { return &p }

// TestBuildV2CompactedHistoryFiltersToInstalledRetentionShape ports the Rust
// build_v2_compacted_history_filters_to_installed_retention_shape test.
func TestBuildV2CompactedHistoryFiltersToInstalledRetentionShape(t *testing.T) {
	input := []protocol.ResponseItem{
		phasedMessage("developer", "dev", nil),
		phasedMessage("system", "sys", nil),
		phasedMessage("user", "user", nil),
		phasedMessage("assistant", "commentary", phasePtr(protocol.MessagePhaseCommentary)),
		phasedMessage("assistant", "final", phasePtr(protocol.MessagePhaseFinalAnswer)),
		functionCallItem("call_1"),
		compactionItem("old"),
	}
	output := compactionItem("new")

	got := BuildV2CompactedHistory(input, output, nil)

	// developer + system are retained candidates but dropped by
	// ShouldKeepCompactedHistoryItem; assistant/function/compaction are not
	// retention candidates. Only the user message survives, plus the output.
	want := []protocol.ResponseItem{phasedMessage("user", "user", nil), output}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%+v\nwant\n%+v", got, want)
	}
}

// TestBuildV2CompactedHistoryDiscardsMessagesBeforeTruncating ports the Rust
// build_v2_compacted_history_discards_messages_before_truncating test.
func TestBuildV2CompactedHistoryDiscardsMessagesBeforeTruncating(t *testing.T) {
	old := phasedMessage("user", "old", nil)
	newMsg := phasedMessage("user", "new", nil)
	hugeDeveloper := repeatStr("d", (RetainedMessageTokenBudget+1)*4)
	hugeContextual := "<environment_context>\n" + repeatStr("c", (RetainedMessageTokenBudget+1)*4) + "\n</environment_context>"

	input := []protocol.ResponseItem{
		old,
		phasedMessage("developer", hugeDeveloper, nil),
		phasedMessage("user", hugeContextual, nil),
		newMsg,
	}
	output := compactionItem("new")

	// keepUser drops the huge contextual user message (environment_context wrapper)
	// the way parse_turn_item would, leaving only the small real user messages.
	keepUser := func(item protocol.ResponseItem) bool {
		text := UserMessageText(item)
		return text == "old" || text == "new"
	}

	got := BuildV2CompactedHistory(input, output, keepUser)

	want := []protocol.ResponseItem{old, newMsg, output}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%+v\nwant\n%+v", got, want)
	}
}

// TestRetainedHistoryTruncationKeepsNewestMessagesFirst ports the Rust
// retained_history_truncation_keeps_newest_messages_first test.
func TestRetainedHistoryTruncationKeepsNewestMessagesFirst(t *testing.T) {
	middle := phasedMessage("user", "middle1234", nil)
	newMsg := phasedMessage("user", "new", nil)
	retained := []protocol.ResponseItem{
		phasedMessage("user", "old-old", nil),
		middle,
		newMsg,
	}

	got := TruncateRetainedMessagesForRemoteCompaction(retained, 3)

	want := []protocol.ResponseItem{
		phasedMessage("user", "midd…1 tokens truncated…1234", nil),
		newMsg,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%+v\nwant\n%+v", got, want)
	}
}

// TestRetainedHistoryTruncationPreservesImages ports the Rust
// retained_history_truncation_preserves_images_and_truncates_later_text_parts test.
func TestRetainedHistoryTruncationPreservesImages(t *testing.T) {
	item := protocol.ResponseItem{
		Type: protocol.ResponseItemKindMessage,
		Role: "user",
		Content: []protocol.ContentItem{
			{Type: protocol.ContentItemKindInputText, Text: "abcdef"},
			{Type: protocol.ContentItemKindInputImage, ImageURL: "data:image/png;base64,abc"},
			{Type: protocol.ContentItemKindOutputText, Text: "uvwxyz"},
		},
	}

	got := TruncateRetainedMessagesForRemoteCompaction([]protocol.ResponseItem{item}, 3)

	want := []protocol.ResponseItem{{
		Type: protocol.ResponseItemKindMessage,
		Role: "user",
		Content: []protocol.ContentItem{
			{Type: protocol.ContentItemKindInputText, Text: "abcdef"},
			{Type: protocol.ContentItemKindInputImage, ImageURL: "data:image/png;base64,abc"},
			{Type: protocol.ContentItemKindOutputText, Text: "uv…1 tokens truncated…yz"},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%+v\nwant\n%+v", got, want)
	}
}

// TestRetainedHistoryTruncationChargesImageOnlyMessages ports the Rust
// retained_history_truncation_charges_image_only_messages test.
func TestRetainedHistoryTruncationChargesImageOnlyMessages(t *testing.T) {
	imageOnly := protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "user",
		Content: []protocol.ContentItem{{Type: protocol.ContentItemKindInputImage, ImageURL: "data:image/png;base64,abc"}},
	}
	newest := phasedMessage("user", "new", nil)
	retained := []protocol.ResponseItem{phasedMessage("user", "old", nil), imageOnly, newest}

	got := TruncateRetainedMessagesForRemoteCompaction(retained, 2)

	want := []protocol.ResponseItem{imageOnly, newest}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%+v\nwant\n%+v", got, want)
	}
}

// TestRetainedHistoryTruncationDropsImageOnlyAfterBudgetSpent ports the Rust
// retained_history_truncation_drops_image_only_messages_after_budget_is_spent test.
func TestRetainedHistoryTruncationDropsImageOnlyAfterBudgetSpent(t *testing.T) {
	imageOnly := protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "user",
		Content: []protocol.ContentItem{{Type: protocol.ContentItemKindInputImage, ImageURL: "data:image/png;base64,abc"}},
	}
	newest := phasedMessage("user", "new", nil)
	retained := []protocol.ResponseItem{imageOnly, newest}

	got := TruncateRetainedMessagesForRemoteCompaction(retained, 1)

	want := []protocol.ResponseItem{newest}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%+v\nwant\n%+v", got, want)
	}
}

// TestCollectCompactionOutputAcceptsAdditionalOutputItems ports the Rust
// collect_compaction_output_accepts_additional_output_items test.
func TestCollectCompactionOutputAcceptsAdditionalOutputItems(t *testing.T) {
	compaction := compactionItem("encrypted")
	assistant := phasedMessage("assistant", "IGNORED_COMPACT_REPLY", phasePtr(protocol.MessagePhaseFinalAnswer))

	stream := makeStream(
		outputItemDone(assistant),
		outputItemDone(compaction),
		completed("resp-compact"),
	)

	got, err := CollectCompactionOutput(context.Background(), stream)
	if err != nil {
		t.Fatalf("CollectCompactionOutput: %v", err)
	}
	if !reflect.DeepEqual(got.Item, compaction) {
		t.Errorf("item = %+v, want %+v", got.Item, compaction)
	}
	if got.ResponseID != "resp-compact" {
		t.Errorf("response id = %q, want resp-compact", got.ResponseID)
	}
}

func TestCollectCompactionOutputErrors(t *testing.T) {
	compaction := compactionItem("e")

	tests := []struct {
		name    string
		results []api.ResponseResult
		wantErr string
	}{
		{
			name:    "no completed event",
			results: []api.ResponseResult{outputItemDone(compaction)},
			wantErr: "stream closed before response.completed",
		},
		{
			name:    "zero compaction items",
			results: []api.ResponseResult{completed("r")},
			wantErr: "expected exactly one compaction output item, got 0",
		},
		{
			name: "two compaction items",
			results: []api.ResponseResult{
				outputItemDone(compaction),
				outputItemDone(compactionItem("e2")),
				completed("r"),
			},
			wantErr: "expected exactly one compaction output item, got 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := makeStream(tt.results...)
			_, err := CollectCompactionOutput(context.Background(), stream)
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestCollectCompactionOutputPropagatesStreamError(t *testing.T) {
	stream := makeStream(api.ResponseResult{Err: api.NewStreamError("boom")})
	_, err := CollectCompactionOutput(context.Background(), stream)
	if err == nil || !contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want containing boom", err)
	}
}

// --- helpers ---

func outputItemDone(item protocol.ResponseItem) api.ResponseResult {
	cp := item
	return api.ResponseResult{Event: &api.ResponseEvent{Kind: api.ResponseEventOutputItemDone, Item: &cp}}
}

func completed(id string) api.ResponseResult {
	return api.ResponseResult{Event: &api.ResponseEvent{Kind: api.ResponseEventCompleted, ResponseID: id}}
}

func makeStream(results ...api.ResponseResult) api.ResponseStream {
	ch := make(chan api.ResponseResult, len(results))
	for _, r := range results {
		ch <- r
	}
	close(ch)
	return api.ResponseStream{Events: ch}
}

func repeatStr(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

func contains(s, substr string) bool {
	return len(substr) == 0 || indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
