package tui

import (
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestDiffRendererUnifiedDiff(t *testing.T) {
	r := NewDiffRenderer(testTheme())
	diff := "--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n"
	lines := plainLines(r.RenderUnifiedDiff(diff))
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"@@ -1 +1 @@", "-old", "+new"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("diff missing %q in %q", want, joined)
		}
	}
}

func TestDiffRendererAddDeleteColoring(t *testing.T) {
	r := NewDiffRenderer(testTheme())
	lines := r.RenderUnifiedDiff("+added\n-removed\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	// Added line uses success color, deleted uses error color.
	if lines[0].Spans[0].Style.Fg != testTheme().Success {
		t.Fatalf("added line not success-colored")
	}
	if lines[1].Spans[0].Style.Fg != testTheme().Error {
		t.Fatalf("removed line not error-colored")
	}
}

func TestDiffRendererFileChanges(t *testing.T) {
	r := NewDiffRenderer(testTheme())
	changes := map[string]protocol.FileChange{
		"new.txt": {Kind: protocol.FileChangeKindAdd, Content: "line1\nline2\n"},
	}
	lines := plainLines(r.RenderFileChanges(changes))
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "added new.txt") {
		t.Fatalf("file header missing: %q", joined)
	}
	if !strings.Contains(joined, "+line1") {
		t.Fatalf("added content missing: %q", joined)
	}
}
