package read

import (
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

func strPtr(s string) *string { return &s }

func TestMemoriesUsageKindAsTag(t *testing.T) {
	tests := []struct {
		kind MemoriesUsageKind
		want string
	}{
		{MemoryMd, "memory_md"},
		{MemorySummary, "memory_summary"},
		{RawMemories, "raw_memories"},
		{RolloutSummaries, "rollout_summaries"},
		{Skills, "skills"},
	}
	for _, tc := range tests {
		if got := tc.kind.AsTag(); got != tc.want {
			t.Errorf("AsTag(%d) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

func TestMemoriesUsageKindsFromParsedCommands(t *testing.T) {
	tests := []struct {
		name     string
		commands []protocol.ParsedCommand
		safe     bool
		want     []MemoriesUsageKind
	}{
		{
			name:     "unsafe returns nil",
			commands: []protocol.ParsedCommand{protocol.NewReadParsedCommand("cat", "MEMORY.md", "memories/MEMORY.md")},
			safe:     false,
			want:     nil,
		},
		{
			name: "read classifies memory artifacts",
			commands: []protocol.ParsedCommand{
				protocol.NewReadParsedCommand("cat", "MEMORY.md", "x/memories/MEMORY.md"),
				protocol.NewReadParsedCommand("cat", "memory_summary.md", "x/memories/memory_summary.md"),
				protocol.NewReadParsedCommand("cat", "raw_memories.md", "x/memories/raw_memories.md"),
			},
			safe: true,
			want: []MemoriesUsageKind{MemoryMd, MemorySummary, RawMemories},
		},
		{
			name: "search with rollout path",
			commands: []protocol.ParsedCommand{
				protocol.NewSearchParsedCommand("rg", strPtr("q"), strPtr("x/memories/rollout_summaries/foo.md")),
			},
			safe: true,
			want: []MemoriesUsageKind{RolloutSummaries},
		},
		{
			name: "search with nil path skipped",
			commands: []protocol.ParsedCommand{
				protocol.NewSearchParsedCommand("rg", strPtr("q"), nil),
			},
			safe: true,
			want: nil,
		},
		{
			name: "skills path",
			commands: []protocol.ParsedCommand{
				protocol.NewReadParsedCommand("cat", "SKILL.md", "x/memories/skills/foo/SKILL.md"),
			},
			safe: true,
			want: []MemoriesUsageKind{Skills},
		},
		{
			name: "list and unknown ignored",
			commands: []protocol.ParsedCommand{
				protocol.NewListFilesParsedCommand("ls", strPtr("x/memories/skills")),
				protocol.NewUnknownParsedCommand("foo"),
			},
			safe: true,
			want: nil,
		},
		{
			name: "unrelated path ignored",
			commands: []protocol.ParsedCommand{
				protocol.NewReadParsedCommand("cat", "main.go", "src/main.go"),
			},
			safe: true,
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MemoriesUsageKindsFromParsedCommands(tc.commands, tc.safe)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("kinds = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMemoryRoot(t *testing.T) {
	base := abspath.ResolvePathAgainstBase("/home/user/.codex", "/")
	got := MemoryRoot(base)
	want := "/home/user/.codex/memories"
	if got.Path() != want {
		t.Fatalf("MemoryRoot = %q, want %q", got.Path(), want)
	}
}
