package tui

import "testing"

func TestComposerFooterDefaultStatusLine(t *testing.T) {
	f := ComposerFooter{Model: "gpt-5.5", Directory: "~/work"}
	// model-with-reasoning · current-dir, indented by FOOTER_INDENT_COLS (2).
	want := "  gpt-5.5 default · ~/work"
	if got := f.line(80); got != want {
		t.Fatalf("line(80) = %q, want %q", got, want)
	}
}

func TestComposerFooterReasoningLabel(t *testing.T) {
	f := ComposerFooter{Model: "gpt-5.5", ReasoningLabel: "high", Directory: "~/x"}
	want := "  gpt-5.5 high · ~/x"
	if got := f.line(80); got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
}

func TestComposerFooterTruncatesWithEllipsis(t *testing.T) {
	// A long directory must be truncated to width-indent with a trailing ellipsis,
	// mirroring truncate_line_with_ellipsis_if_overflow over the status line.
	f := ComposerFooter{Model: "m", Directory: "/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	got := f.line(20)
	if r := []rune(got); len(r) != 20 || r[len(r)-1] != '…' {
		t.Fatalf("line(20) = %q (len %d), want width 20 ending in ellipsis", got, len([]rune(got)))
	}
	if got[:2] != "  " {
		t.Fatalf("line should keep the 2-col indent, got %q", got)
	}
}

func TestComposerFooterEmptyWithoutModelOrDir(t *testing.T) {
	if got := (ComposerFooter{}).line(80); got != "" {
		t.Fatalf("empty footer = %q, want empty", got)
	}
}

func TestComposerFooterDirOnly(t *testing.T) {
	f := ComposerFooter{Directory: "~/work"}
	if got := f.line(80); got != "  ~/work" {
		t.Fatalf("dir-only footer = %q, want %q", got, "  ~/work")
	}
}
