package tui

import (
	"strings"
	"testing"
)

// TestUserCellMarkerAndBlanks verifies the user echo cell renders codex's
// UserHistoryCell::display_lines shape: a leading blank line, the "› " marker
// (bold+dim) on the first body line with the body text unstyled, and a trailing
// blank line.
func TestUserCellMarkerAndBlanks(t *testing.T) {
	cell := NewUserCell(testTheme(), "hi")
	lines := cell.Lines(80)

	if len(lines) != 3 {
		t.Fatalf("want 3 lines (blank, body, blank), got %d: %+v", len(lines), lines)
	}
	if len(lines[0].Spans) != 0 {
		t.Errorf("leading line should be blank, got %+v", lines[0].Spans)
	}
	if len(lines[2].Spans) != 0 {
		t.Errorf("trailing line should be blank, got %+v", lines[2].Spans)
	}

	body := lines[1]
	if len(body.Spans) < 2 {
		t.Fatalf("body line should have a marker span + text span, got %+v", body.Spans)
	}
	marker := body.Spans[0]
	if marker.Text != "› " {
		t.Errorf("marker = %q, want %q", marker.Text, "› ")
	}
	if !marker.Style.Bold || !marker.Style.Dim {
		t.Errorf("marker style = %+v, want Bold+Dim", marker.Style)
	}
	if marker.Style.Fg != nil {
		t.Errorf("marker should carry no foreground color, got %v", marker.Style.Fg)
	}
	if got := body.Spans[1].Text; got != "hi" {
		t.Errorf("body text = %q, want %q", got, "hi")
	}
	if st := body.Spans[1].Style; st.Bold || st.Dim || st.Fg != nil {
		t.Errorf("body text should be unstyled (terminal default), got %+v", st)
	}
}

// TestUserCellEmptyMessageRendersNothing verifies a whitespace-only submission
// produces no lines (codex trims trailing blank message lines and skips empty
// messages).
func TestUserCellEmptyMessageRendersNothing(t *testing.T) {
	if got := NewUserCell(testTheme(), "   \n\t").Lines(80); got != nil {
		t.Fatalf("empty message should render nothing, got %+v", got)
	}
}

// TestUserCellWrapsWithContinuationIndent verifies that a message wider than the
// content width wraps onto continuation lines prefixed with a two-space indent,
// not the marker.
func TestUserCellWrapsWithContinuationIndent(t *testing.T) {
	cell := NewUserCell(testTheme(), "one two three four five six seven")
	lines := cell.Lines(12) // narrow width forces wrapping

	if len(lines) < 4 {
		t.Fatalf("expected wrapping into multiple body lines, got %d: %+v", len(lines), lines)
	}
	// First body line carries the marker.
	if lines[1].Spans[0].Text != "› " {
		t.Errorf("first body marker = %q, want %q", lines[1].Spans[0].Text, "› ")
	}
	// Second body line carries the two-space continuation indent (unstyled).
	cont := lines[2].Spans[0]
	if cont.Text != "  " {
		t.Errorf("continuation prefix = %q, want two spaces", cont.Text)
	}
	if cont.Style.Bold || cont.Style.Dim {
		t.Errorf("continuation prefix should be unstyled, got %+v", cont.Style)
	}
}

// TestAgentCellBulletPrefix verifies the agent message cell renders codex's
// AgentMarkdownCell prefix: a dim "• " bullet on the first line, two-space indent
// on continuation lines, with the body text inheriting the markdown renderer's
// default (no theme foreground).
func TestAgentCellBulletPrefix(t *testing.T) {
	cell := NewAgentCell(NewMarkdownRenderer(testTheme()), "Hello from parity")
	lines := cell.Lines(80)

	if len(lines) == 0 {
		t.Fatal("agent cell produced no lines")
	}
	bullet := lines[0].Spans[0]
	if bullet.Text != "• " {
		t.Errorf("bullet = %q, want %q", bullet.Text, "• ")
	}
	if !bullet.Style.Dim {
		t.Errorf("bullet style = %+v, want Dim", bullet.Style)
	}
	if bullet.Style.Bold || bullet.Style.Fg != nil {
		t.Errorf("bullet should be dim only (no bold, no fg), got %+v", bullet.Style)
	}
	// The body text immediately after the bullet must carry no foreground color
	// (codex plain markdown text is Style::default()).
	if len(lines[0].Spans) < 2 {
		t.Fatalf("first line should have bullet + body span, got %+v", lines[0].Spans)
	}
	if fg := lines[0].Spans[1].Style.Fg; fg != nil {
		t.Errorf("agent body text should have no foreground color, got %v", fg)
	}
}

// TestAgentCellMultilineContinuationIndent verifies continuation lines of a
// multi-paragraph agent message use the two-space indent, not the bullet.
func TestAgentCellMultilineContinuationIndent(t *testing.T) {
	cell := NewAgentCell(NewMarkdownRenderer(testTheme()), "first line\n\nsecond line")
	lines := cell.Lines(80)
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d", len(lines))
	}
	if lines[0].Spans[0].Text != "• " {
		t.Errorf("first line prefix = %q, want bullet", lines[0].Spans[0].Text)
	}
	for i := 1; i < len(lines); i++ {
		if len(lines[i].Spans) == 0 {
			continue
		}
		if got := lines[i].Spans[0].Text; got != "  " {
			t.Errorf("line %d prefix = %q, want two-space indent", i, got)
		}
	}
}

// TestTranscriptAppendUserMessageEcho verifies the transcript echoes a submitted
// user message into a user cell (codex's on_user_message_display), rendered with
// the "› " marker.
func TestTranscriptAppendUserMessageEcho(t *testing.T) {
	tr := NewChatTranscript(testTheme()).AppendUserMessage("hi there").(ChatTranscript)
	out := tr.View(Rect{Width: 40, Height: 6})
	if !strings.Contains(out, "› hi there") {
		t.Fatalf("user echo with marker not rendered: %q", out)
	}
}

// TestTranscriptAppendUserMessageEmptyIgnored verifies an empty submission adds
// no cell.
func TestTranscriptAppendUserMessageEmptyIgnored(t *testing.T) {
	tr := NewChatTranscript(testTheme())
	got := tr.AppendUserMessage("   ").(ChatTranscript)
	if len(got.cells) != 0 {
		t.Fatalf("empty submission should add no cell, got %d cells", len(got.cells))
	}
}
