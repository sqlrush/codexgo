package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestParseKeybindingCanonical(t *testing.T) {
	b, ok := ParseKeybinding("ctrl-alt-shift-a")
	if !ok {
		t.Fatal("expected binding to parse")
	}
	key, mods := b.Parts()
	if key != Char('a') {
		t.Fatalf("key = %+v, want Char('a')", key)
	}
	if mods != ModControl|ModAlt|ModShift {
		t.Fatalf("mods = %v, want ctrl|alt|shift", mods)
	}
}

func TestParseKeybindingNamedKeys(t *testing.T) {
	cases := map[string]KeyCode{
		"enter":     KeyEnter,
		"shift-tab": KeyTab,
		"esc":       KeyEsc,
		"page-up":   KeyPageUp,
		"page-down": KeyPageDown,
		"space":     Char(' '),
		"minus":     Char('-'),
		"f5":        FKey(5),
		"home":      KeyHome,
	}
	for spec, want := range cases {
		b, ok := ParseKeybinding(spec)
		if !ok {
			t.Fatalf("%q failed to parse", spec)
		}
		if key, _ := b.Parts(); key != want {
			t.Fatalf("%q -> %+v, want %+v", spec, key, want)
		}
	}
}

func TestParseKeybindingTrailingHyphen(t *testing.T) {
	// "ctrl--" means Ctrl plus the literal '-' key.
	b, ok := ParseKeybinding("ctrl--")
	if !ok {
		t.Fatal("ctrl-- should parse")
	}
	key, mods := b.Parts()
	if key != Char('-') || mods != ModControl {
		t.Fatalf("ctrl-- = %+v / %v", key, mods)
	}
}

func TestParseKeybindingRejectsInvalid(t *testing.T) {
	for _, spec := range []string{"meta-enter", "f13", "ctrl", "", "ctrl-nope"} {
		if _, ok := ParseKeybinding(spec); ok {
			t.Fatalf("expected %q to be rejected", spec)
		}
	}
}

func TestShiftedLetterMatchesUppercase(t *testing.T) {
	binding := Shift(Char('a'))
	// Plain uppercase A (no SHIFT reported) must match shift-a.
	if !binding.IsPress(runeKey('A')) {
		t.Fatal("shift-a should match plain uppercase A")
	}
	// Lowercase a must NOT match shift-a.
	if binding.IsPress(runeKey('a')) {
		t.Fatal("shift-a must not match plain lowercase a")
	}
}

func TestCtrlBindingMatchesCtrlKeyMsg(t *testing.T) {
	binding := Ctrl(Char('k'))
	if !binding.IsPress(tea.KeyMsg{Type: tea.KeyCtrlK}) {
		t.Fatal("ctrl-k should match KeyCtrlK")
	}
	if binding.IsPress(tea.KeyMsg{Type: tea.KeyCtrlJ}) {
		t.Fatal("ctrl-k must not match KeyCtrlJ")
	}
}

func TestNormalizeC0ControlChar(t *testing.T) {
	// A raw C0 control char (0x10 == Ctrl+P) reported as an unmodified rune must
	// normalize to Ctrl+p.
	key, mods := normalizeKeyParts(Char(0x10), ModNone)
	if key != Char('p') || mods != ModControl {
		t.Fatalf("normalize 0x10 = %+v / %v, want Ctrl+p", key, mods)
	}
}

func TestKeyBindingListIsPressed(t *testing.T) {
	list := KeyBindingList{Plain(Char('a')), Ctrl(Char('b'))}
	if !list.IsPressed(runeKey('a')) {
		t.Fatal("should match plain a")
	}
	if !list.IsPressed(tea.KeyMsg{Type: tea.KeyCtrlB}) {
		t.Fatal("should match ctrl-b")
	}
	if list.IsPressed(runeKey('c')) {
		t.Fatal("should not match c")
	}
}

func TestPrimaryBinding(t *testing.T) {
	list := KeyBindingList{Plain(KeyEnter), Shift(KeyEnter)}
	b, ok := PrimaryBinding(list)
	if !ok || b != Plain(KeyEnter) {
		t.Fatalf("primary = %+v ok=%v", b, ok)
	}
	if _, ok := PrimaryBinding(nil); ok {
		t.Fatal("empty list should have no primary")
	}
}

func TestIsPlainTextKeyEvent(t *testing.T) {
	if !IsPlainTextKeyEvent(runeKey('j')) {
		t.Fatal("plain j is text input")
	}
	if IsPlainTextKeyEvent(tea.KeyMsg{Type: tea.KeyCtrlJ}) {
		t.Fatal("ctrl-j is not text input")
	}
	if IsPlainTextKeyEvent(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true}) {
		t.Fatal("alt-x is not text input")
	}
}

func TestDisplayLabel(t *testing.T) {
	cases := map[KeyBinding]string{
		Ctrl(Char('t')):  "ctrl + t",
		Shift(Char('a')): "shift + a",
		Plain(KeyEnter):  "enter",
		Plain(Char(' ')): "space",
		Plain(KeyPageUp): "pgup",
	}
	for b, want := range cases {
		if got := b.DisplayLabel(); got != want {
			t.Fatalf("DisplayLabel(%+v) = %q, want %q", b, got, want)
		}
	}
}
