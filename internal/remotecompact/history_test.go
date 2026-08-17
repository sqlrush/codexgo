package remotecompact

import (
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func msgItem(role, text string) protocol.ResponseItem { return userMessage(role, text) }

func compactionItem(enc string) protocol.ResponseItem {
	return protocol.ResponseItem{Type: protocol.ResponseItemKindCompaction, EncryptedContent: strptr(enc)}
}

func functionCallItem(callID string) protocol.ResponseItem {
	return protocol.ResponseItem{
		Type:      protocol.ResponseItemKindFunctionCall,
		Name:      "shell",
		Arguments: "{}",
		CallID:    callID,
	}
}

func TestShouldKeepCompactedHistoryItem(t *testing.T) {
	keepReal := func(item protocol.ResponseItem) bool {
		// Treat any user message whose text starts with "ctx:" as droppable
		// session scaffolding, mirroring the contextual-wrapper filtering.
		return UserMessageText(item) != "ctx:prefix"
	}

	tests := []struct {
		name string
		item protocol.ResponseItem
		keep KeepUserMessage
		want bool
	}{
		{"developer dropped", msgItem("developer", "x"), nil, false},
		{"assistant kept", msgItem("assistant", "x"), nil, true},
		{"system dropped", msgItem("system", "x"), nil, false},
		{"user kept by default", msgItem("user", "hello"), nil, true},
		{"user kept by predicate", msgItem("user", "hello"), keepReal, true},
		{"contextual user dropped by predicate", msgItem("user", "ctx:prefix"), keepReal, false},
		{"compaction kept", compactionItem("e"), nil, true},
		{"context compaction kept", protocol.ResponseItem{Type: protocol.ResponseItemKindContextCompaction}, nil, true},
		{"compaction trigger dropped", protocol.ResponseItem{Type: protocol.ResponseItemKindCompactionTrigger}, nil, false},
		{"function call dropped", functionCallItem("c1"), nil, false},
		{"reasoning dropped", protocol.ResponseItem{Type: protocol.ResponseItemKindReasoning}, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldKeepCompactedHistoryItem(tt.item, tt.keep); got != tt.want {
				t.Errorf("ShouldKeepCompactedHistoryItem = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProcessCompactedHistoryFiltersAndInjects(t *testing.T) {
	compacted := []protocol.ResponseItem{
		msgItem("developer", "dev"),
		msgItem("user", "real user"),
		functionCallItem("c1"),
		compactionItem("summary"),
	}
	initial := []protocol.ResponseItem{msgItem("user", "INITIAL_CONTEXT")}

	got := ProcessCompactedHistory(compacted, initial, nil, nil)

	// developer + function call are dropped; initial context is injected before
	// the last real user message ("real user").
	want := []protocol.ResponseItem{
		msgItem("user", "INITIAL_CONTEXT"),
		msgItem("user", "real user"),
		compactionItem("summary"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProcessCompactedHistory =\n%+v\nwant\n%+v", got, want)
	}
}

func TestInsertInitialContextInjectionPoints(t *testing.T) {
	initial := []protocol.ResponseItem{msgItem("user", "CTX")}

	tests := []struct {
		name      string
		history   []protocol.ResponseItem
		isSummary IsSummaryMessage
		want      []protocol.ResponseItem
	}{
		{
			name:    "before last real user",
			history: []protocol.ResponseItem{msgItem("user", "u1"), compactionItem("c"), msgItem("user", "u2")},
			want:    []protocol.ResponseItem{msgItem("user", "u1"), compactionItem("c"), msgItem("user", "CTX"), msgItem("user", "u2")},
		},
		{
			name:      "before summary when no real user",
			history:   []protocol.ResponseItem{compactionItem("c"), msgItem("user", "SUMMARY")},
			isSummary: func(s string) bool { return s == "SUMMARY" },
			want:      []protocol.ResponseItem{compactionItem("c"), msgItem("user", "CTX"), msgItem("user", "SUMMARY")},
		},
		{
			name:    "before compaction when no user at all",
			history: []protocol.ResponseItem{msgItem("assistant", "a"), compactionItem("c")},
			want:    []protocol.ResponseItem{msgItem("assistant", "a"), msgItem("user", "CTX"), compactionItem("c")},
		},
		{
			name:    "appended when nothing matches",
			history: []protocol.ResponseItem{msgItem("assistant", "a")},
			want:    []protocol.ResponseItem{msgItem("assistant", "a"), msgItem("user", "CTX")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InsertInitialContextBeforeLastRealUserOrSummary(tt.history, initial, nil, tt.isSummary)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

func TestInsertInitialContextDoesNotMutateInput(t *testing.T) {
	history := []protocol.ResponseItem{msgItem("user", "u1")}
	initial := []protocol.ResponseItem{msgItem("user", "CTX")}
	_ = InsertInitialContextBeforeLastRealUserOrSummary(history, initial, nil, nil)
	if len(history) != 1 || UserMessageText(history[0]) != "u1" {
		t.Fatalf("input history was mutated: %+v", history)
	}
}

func TestBuildCompactRequestLogData(t *testing.T) {
	input := []protocol.ResponseItem{msgItem("user", "ab"), msgItem("user", "cde")}
	estimate := func(item protocol.ResponseItem) int64 {
		return int64(len(UserMessageText(item)))
	}

	tests := []struct {
		name         string
		instructions string
		estimate     EstimateModelVisibleBytes
		want         int64
	}{
		{"instructions plus items", "hi", estimate, int64(2 + 2 + 3)},
		{"nil estimator counts only instructions", "hello", nil, 5},
		{"empty everything", "", estimate, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildCompactRequestLogData(input, tt.instructions, tt.estimate)
			if got.FailingCompactionRequestModelVisibleBytes != tt.want {
				t.Errorf("bytes = %d, want %d", got.FailingCompactionRequestModelVisibleBytes, tt.want)
			}
		})
	}
}

func TestTrimFunctionCallHistoryToFitContextWindow(t *testing.T) {
	codexGen := func(item protocol.ResponseItem) bool {
		return item.Type == protocol.ResponseItemKindFunctionCall
	}
	// Estimate one token per item for simplicity.
	estimate := func(history []protocol.ResponseItem) (int64, bool) {
		return int64(len(history)), true
	}
	window := int64(2)

	tests := []struct {
		name        string
		history     []protocol.ResponseItem
		window      *int64
		estimate    EstimateTokenCountWithBaseInstructions
		wantDeleted int
		wantLen     int
	}{
		{
			name:        "trims trailing codex items to fit",
			history:     []protocol.ResponseItem{msgItem("user", "u"), functionCallItem("c1"), functionCallItem("c2")},
			window:      &window,
			estimate:    estimate,
			wantDeleted: 1,
			wantLen:     2,
		},
		{
			name:        "stops at non-codex trailing item",
			history:     []protocol.ResponseItem{functionCallItem("c1"), msgItem("user", "u"), msgItem("user", "v")},
			window:      &window,
			estimate:    estimate,
			wantDeleted: 0,
			wantLen:     3,
		},
		{
			name:        "nil window trims nothing",
			history:     []protocol.ResponseItem{functionCallItem("c1"), functionCallItem("c2"), functionCallItem("c3")},
			window:      nil,
			estimate:    estimate,
			wantDeleted: 0,
			wantLen:     3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, deleted := TrimFunctionCallHistoryToFitContextWindow(tt.history, tt.window, tt.estimate, codexGen)
			if deleted != tt.wantDeleted {
				t.Errorf("deleted = %d, want %d", deleted, tt.wantDeleted)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}
