package tui

import (
	"bytes"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// drainAllCmds recursively runs a tea.Cmd, collecting every produced message
// (flattening tea.Batch and tea.Sequence). It is a small test harness for
// asserting the side effects of a command tree without a running program.
//
// tea.Batch produces an exported tea.BatchMsg ([]tea.Cmd); tea.Sequence produces
// an unexported sequenceMsg whose underlying type is []tea.Cmd, so it is detected
// via reflection over any slice-of-Cmd message.
func drainAllCmds(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, drainAllCmds(c)...)
		}
		return out
	}
	if cmds, ok := asCmdSlice(msg); ok {
		var out []tea.Msg
		for _, c := range cmds {
			out = append(out, drainAllCmds(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// asCmdSlice reports whether msg's underlying type is a slice of tea.Cmd (e.g.
// the unexported sequenceMsg) and returns it as []tea.Cmd.
func asCmdSlice(msg tea.Msg) ([]tea.Cmd, bool) {
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice {
		return nil, false
	}
	if v.Type().Elem() != reflect.TypeOf(tea.Cmd(nil)) {
		return nil, false
	}
	out := make([]tea.Cmd, v.Len())
	for i := 0; i < v.Len(); i++ {
		out[i], _ = v.Index(i).Interface().(tea.Cmd)
	}
	return out, true
}

// TestClearTerminalSequenceMatchesCodex pins the exact ANSI clear bytes against
// the sequence captured from codex 0.136.0.
func TestClearTerminalSequenceMatchesCodex(t *testing.T) {
	want := "\x1b[r\x1b[0m\x1b[H\x1b[2J\x1b[3J\x1b[H"
	if clearTerminalSequence != want {
		t.Fatalf("clear sequence = %q, want %q", clearTerminalSequence, want)
	}
}

// TestClearUIEmitsExactSequence verifies that handling a ClearUIEvent writes the
// exact codex clear sequence to the model's output writer.
func TestClearUIEmitsExactSequence(t *testing.T) {
	var out bytes.Buffer
	transcript := NewChatTranscript(DefaultTheme(Capabilities{ColorLevel: ColorLevelTrueColor})).
		WithSessionHeader("0.136.0", "gpt-5.5", "~/work")
	m := NewModel(ModelConfig{
		Caps:       Capabilities{ColorLevel: ColorLevelTrueColor},
		Transcript: transcript,
		Inline:     true,
		Output:     &out,
	})
	m.width = 80
	m.height = 24

	updated, cmd := m.Update(ClearUIEvent{})
	// Run the full command tree so the raw-write command executes.
	_ = drainAllCmds(cmd)

	if got := out.String(); got != clearTerminalSequence {
		t.Fatalf("clear output = %q, want %q", got, clearTerminalSequence)
	}

	// The transcript must have been reset to a fresh, header-seeded one: the
	// header card is present again and starts undrained-then-redrained.
	tr, ok := updated.(Model).transcript.(ChatTranscript)
	if !ok {
		t.Fatalf("transcript is %T, want ChatTranscript", updated.(Model).transcript)
	}
	if len(tr.cells) != 1 {
		t.Fatalf("fresh transcript has %d cells, want 1 (re-seeded header)", len(tr.cells))
	}
}

// TestClearUIReseedsHeaderIntoScrollback verifies the fresh session-header card
// is re-emitted into scrollback after /clear: the fresh transcript's header cell
// is marked flushed (drained) by the clear handler, proving the re-drain ran.
func TestClearUIReseedsHeaderIntoScrollback(t *testing.T) {
	var out bytes.Buffer
	transcript := NewChatTranscript(DefaultTheme(Capabilities{ColorLevel: ColorLevelTrueColor})).
		WithSessionHeader("0.136.0", "gpt-5.5", "~/work")
	m := NewModel(ModelConfig{
		Caps:       Capabilities{ColorLevel: ColorLevelTrueColor},
		Transcript: transcript,
		Inline:     true,
		Output:     &out,
	})
	m.width = 80
	m.height = 24

	updated, cmd := m.Update(ClearUIEvent{})
	_ = drainAllCmds(cmd)

	tr := updated.(Model).transcript.(ChatTranscript)
	if tr.flushedCells != 1 || !tr.scrollbackEmitted {
		t.Fatalf("fresh header not re-drained: flushed=%d emitted=%v (want 1, true)",
			tr.flushedCells, tr.scrollbackEmitted)
	}
}

// TestClearUIResetForClearDiscardsHistory verifies the transcript reset drops
// prior history cells but re-seeds the header.
func TestClearUIResetForClearDiscardsHistory(t *testing.T) {
	transcript := NewChatTranscript(DefaultTheme(Capabilities{ColorLevel: ColorLevelTrueColor})).
		WithSessionHeader("0.136.0", "gpt-5.5", "~/work")
	transcript = transcript.AppendUserMessage("hello").(ChatTranscript)
	if len(transcript.cells) != 2 {
		t.Fatalf("pre-clear cells = %d, want 2 (header + user)", len(transcript.cells))
	}
	fresh := transcript.ResetForClear().(ChatTranscript)
	if len(fresh.cells) != 1 {
		t.Fatalf("post-clear cells = %d, want 1 (header only)", len(fresh.cells))
	}
	if fresh.flushedCells != 0 || fresh.scrollbackEmitted {
		t.Fatalf("post-clear drain state not reset: flushed=%d emitted=%v", fresh.flushedCells, fresh.scrollbackEmitted)
	}
}
