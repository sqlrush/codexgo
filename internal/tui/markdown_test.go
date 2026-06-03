package tui

import (
	"strings"
	"testing"
)

func testTheme() Theme {
	return DefaultTheme(Capabilities{})
}

// plain returns the concatenated unstyled text of a line.
func plain(l Line) string {
	var b strings.Builder
	for _, s := range l.Spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

func plainLines(lines []Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = plain(l)
	}
	return out
}

func TestMarkdownRenderHeadingAndParagraph(t *testing.T) {
	r := NewMarkdownRenderer(testTheme())
	got := plainLines(r.Render("# Title\n\nBody text."))
	want := []string{"# Title", "", "Body text."}
	if len(got) != len(want) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMarkdownRenderUnorderedList(t *testing.T) {
	r := NewMarkdownRenderer(testTheme())
	got := plainLines(r.Render("- one\n- two\n"))
	want := []string{"- one", "- two"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("list = %q, want %q", got, want)
	}
}

func TestMarkdownRenderOrderedList(t *testing.T) {
	r := NewMarkdownRenderer(testTheme())
	got := plainLines(r.Render("1. alpha\n2. beta\n"))
	want := []string{"1. alpha", "2. beta"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ordered list = %q, want %q", got, want)
	}
}

func TestMarkdownRenderFencedCodeBlock(t *testing.T) {
	r := NewMarkdownRenderer(testTheme())
	lines := r.Render("```go\nfunc main() {}\n```\n")
	got := plainLines(lines)
	if len(got) != 1 || got[0] != "func main() {}" {
		t.Fatalf("code block = %q, want single line 'func main() {}'", got)
	}
	// The code should be syntax-highlighted: more than one span (keyword + rest).
	if len(lines[0].Spans) < 2 {
		t.Fatalf("expected highlighted spans, got %d", len(lines[0].Spans))
	}
}

func TestMarkdownInlineCodeStyled(t *testing.T) {
	r := NewMarkdownRenderer(testTheme())
	lines := r.Render("use `go test` here\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d", len(lines))
	}
	var found bool
	for _, s := range lines[0].Spans {
		if s.Text == "go test" && s.Style.Fg != nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("inline code span 'go test' not styled: %+v", lines[0].Spans)
	}
}

func TestMarkdownBlockquoteAccented(t *testing.T) {
	r := NewMarkdownRenderer(testTheme())
	lines := r.Render("> quoted\n")
	if len(lines) == 0 {
		t.Fatal("blockquote produced no lines")
	}
	if !strings.Contains(plain(lines[0]), "quoted") {
		t.Fatalf("blockquote text missing: %q", plain(lines[0]))
	}
}
