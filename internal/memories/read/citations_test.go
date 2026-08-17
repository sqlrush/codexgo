package read

import (
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestParseMemoryCitationSupportsLegacyThreadIDs(t *testing.T) {
	first := "019c6e27-e55b-73d1-87d8-4e01f1f75043"
	second := "019c7714-3b77-74d1-9866-e1f484aae2ab"
	citations := []string{
		"<memory_citation>\n<citation_entries>\nMEMORY.md:1-2|note=[x]\n</citation_entries>\n<thread_ids>\n" +
			first + "\nnot-a-uuid\n" + second + "\n</thread_ids>\n</memory_citation>",
	}

	parsed := ParseMemoryCitation(citations)
	if parsed == nil {
		t.Fatal("expected citation to parse")
	}
	got := ThreadIDsFromMemoryCitation(parsed)
	want := []protocol.ThreadID{protocol.NewThreadID(first), protocol.NewThreadID(second)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("thread ids = %v, want %v", got, want)
	}
}

func TestParseMemoryCitationSupportsRolloutIDs(t *testing.T) {
	id := "019c6e27-e55b-73d1-87d8-4e01f1f75043"
	citations := []string{
		"<memory_citation>\n<rollout_ids>\n" + id + "\n</rollout_ids>\n</memory_citation>",
	}

	parsed := ParseMemoryCitation(citations)
	if parsed == nil {
		t.Fatal("expected citation to parse")
	}
	got := ThreadIDsFromMemoryCitation(parsed)
	want := []protocol.ThreadID{protocol.NewThreadID(id)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("thread ids = %v, want %v", got, want)
	}
}

func TestParseMemoryCitationExtractsEntriesAndRolloutIDs(t *testing.T) {
	first := "019c6e27-e55b-73d1-87d8-4e01f1f75043"
	second := "019c7714-3b77-74d1-9866-e1f484aae2ab"
	citations := []string{
		"<citation_entries>\nMEMORY.md:1-2|note=[summary]\nrollout_summaries/foo.md:10-12|note=[details]\n" +
			"</citation_entries>\n<rollout_ids>\n" + first + "\n" + second + "\n" + first + "\n</rollout_ids>",
	}

	parsed := ParseMemoryCitation(citations)
	if parsed == nil {
		t.Fatal("expected citation to parse")
	}
	wantEntries := []protocol.MemoryCitationEntry{
		{Path: "MEMORY.md", LineStart: 1, LineEnd: 2, Note: "summary"},
		{Path: "rollout_summaries/foo.md", LineStart: 10, LineEnd: 12, Note: "details"},
	}
	if !reflect.DeepEqual(parsed.Entries, wantEntries) {
		t.Fatalf("entries = %#v, want %#v", parsed.Entries, wantEntries)
	}
	wantIDs := []string{first, second}
	if !reflect.DeepEqual(parsed.RolloutIDs, wantIDs) {
		t.Fatalf("rollout ids = %v, want %v", parsed.RolloutIDs, wantIDs)
	}
}

func TestParseMemoryCitationReturnsNilWhenEmpty(t *testing.T) {
	if got := ParseMemoryCitation([]string{"no blocks here"}); got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}

func TestParseMemoryCitationEntryRejectsMalformed(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"missing note", "MEMORY.md:1-2"},
		{"missing close bracket", "MEMORY.md:1-2|note=[x"},
		{"missing colon", "MEMORY.md|note=[x]"},
		{"missing dash", "MEMORY.md:12|note=[x]"},
		{"non-numeric start", "MEMORY.md:a-2|note=[x]"},
		{"empty line", "   "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := parseMemoryCitationEntry(tc.line); ok {
				t.Fatalf("expected line %q to be rejected", tc.line)
			}
		})
	}
}

func TestIsUUID(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"019c6e27-e55b-73d1-87d8-4e01f1f75043", true},
		{"019C6E27-E55B-73D1-87D8-4E01F1F75043", true},
		{"not-a-uuid", false},
		{"", false},
		{"019c6e27e55b73d187d84e01f1f75043", false},
		{"019c6e27-e55b-73d1-87d8-4e01f1f7504g", false},
	}
	for _, tc := range tests {
		if got := isUUID(tc.in); got != tc.want {
			t.Errorf("isUUID(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
