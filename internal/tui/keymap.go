package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// KeyCode is the platform-independent key identity of a [KeyBinding]. It mirrors
// the subset of crossterm's KeyCode the TUI keymap uses.
//
// Port of the relevant arms of crossterm::event::KeyCode used by
// codex-rs/tui/src/key_hint.rs and keymap.rs.
type KeyCode struct {
	// Kind discriminates the union.
	Kind KeyCodeKind
	// Ch is the rune for [KeyCodeChar]. Always lowercase for ASCII letters once
	// normalized.
	Ch rune
	// F is the function-key number for [KeyCodeF] (1-12).
	F uint8
}

// KeyCodeKind enumerates the key categories.
type KeyCodeKind uint8

const (
	// KeyCodeChar is a printable character key.
	KeyCodeChar KeyCodeKind = iota
	// KeyCodeEnter is the Enter/Return key.
	KeyCodeEnter
	// KeyCodeTab is the Tab key.
	KeyCodeTab
	// KeyCodeBackspace is the Backspace key.
	KeyCodeBackspace
	// KeyCodeEsc is the Escape key.
	KeyCodeEsc
	// KeyCodeDelete is the forward-delete key.
	KeyCodeDelete
	// KeyCodeInsert is the Insert key.
	KeyCodeInsert
	// KeyCodeUp is the up arrow.
	KeyCodeUp
	// KeyCodeDown is the down arrow.
	KeyCodeDown
	// KeyCodeLeft is the left arrow.
	KeyCodeLeft
	// KeyCodeRight is the right arrow.
	KeyCodeRight
	// KeyCodeHome is the Home key.
	KeyCodeHome
	// KeyCodeEnd is the End key.
	KeyCodeEnd
	// KeyCodePageUp is the Page Up key.
	KeyCodePageUp
	// KeyCodePageDown is the Page Down key.
	KeyCodePageDown
	// KeyCodeF is a function key F1-F12 (number in F).
	KeyCodeF
)

// Char builds a printable-character key code.
func Char(ch rune) KeyCode { return KeyCode{Kind: KeyCodeChar, Ch: ch} }

// FKey builds a function key code for F1-F12.
func FKey(n uint8) KeyCode { return KeyCode{Kind: KeyCodeF, F: n} }

// Named key-code singletons mirroring crossterm's variants.
var (
	KeyEnter     = KeyCode{Kind: KeyCodeEnter}
	KeyTab       = KeyCode{Kind: KeyCodeTab}
	KeyBackspace = KeyCode{Kind: KeyCodeBackspace}
	KeyEsc       = KeyCode{Kind: KeyCodeEsc}
	KeyDelete    = KeyCode{Kind: KeyCodeDelete}
	KeyInsert    = KeyCode{Kind: KeyCodeInsert}
	KeyUp        = KeyCode{Kind: KeyCodeUp}
	KeyDown      = KeyCode{Kind: KeyCodeDown}
	KeyLeft      = KeyCode{Kind: KeyCodeLeft}
	KeyRight     = KeyCode{Kind: KeyCodeRight}
	KeyHome      = KeyCode{Kind: KeyCodeHome}
	KeyEnd       = KeyCode{Kind: KeyCodeEnd}
	KeyPageUp    = KeyCode{Kind: KeyCodePageUp}
	KeyPageDown  = KeyCode{Kind: KeyCodePageDown}
)

// KeyModifiers is a bit set of active modifier keys.
//
// Port of crossterm::event::KeyModifiers (the CONTROL/SHIFT/ALT subset).
type KeyModifiers uint8

const (
	// ModNone is the empty modifier set.
	ModNone KeyModifiers = 0
	// ModShift is the Shift modifier.
	ModShift KeyModifiers = 1 << iota
	// ModControl is the Control modifier.
	ModControl
	// ModAlt is the Alt/Option modifier.
	ModAlt
)

// Has reports whether all bits in m are present.
func (k KeyModifiers) Has(m KeyModifiers) bool { return k&m == m }

// KeyBinding is one concrete key event that can trigger a TUI action.
//
// Matching via [KeyBinding.IsPress] handles exact equality plus compatibility
// fallbacks for terminals that report uppercase letters without SHIFT and Ctrl
// keys as raw C0 control characters. A binding defined as shift-a matches either
// Shift+a or plain A, and ctrl-j matches a raw LF.
//
// Port of codex-rs/tui/src/key_hint.rs `KeyBinding`. KeyBinding is comparable so
// it can be used as a map key and de-duplicated by value, matching the Rust
// derive(Eq, Hash).
type KeyBinding struct {
	key       KeyCode
	modifiers KeyModifiers
}

// NewKeyBinding constructs a binding from a key code and modifier set.
//
// Port of KeyBinding::new.
func NewKeyBinding(key KeyCode, modifiers KeyModifiers) KeyBinding {
	return KeyBinding{key: key, modifiers: modifiers}
}

// Plain builds an unmodified binding. Port of key_hint::plain.
func Plain(key KeyCode) KeyBinding { return NewKeyBinding(key, ModNone) }

// Alt builds an Alt-modified binding. Port of key_hint::alt.
func Alt(key KeyCode) KeyBinding { return NewKeyBinding(key, ModAlt) }

// Shift builds a Shift-modified binding. Port of key_hint::shift.
func Shift(key KeyCode) KeyBinding { return NewKeyBinding(key, ModShift) }

// Ctrl builds a Control-modified binding. Port of key_hint::ctrl.
func Ctrl(key KeyCode) KeyBinding { return NewKeyBinding(key, ModControl) }

// CtrlAlt builds a Control+Alt binding. Port of key_hint::ctrl_alt.
func CtrlAlt(key KeyCode) KeyBinding { return NewKeyBinding(key, ModControl|ModAlt) }

// Parts returns the binding's key code and modifier set.
//
// Port of KeyBinding::parts.
func (b KeyBinding) Parts() (KeyCode, KeyModifiers) { return b.key, b.modifiers }

// FromKeyMsg normalizes a bubbletea key event into a [KeyBinding].
//
// Port of KeyBinding::from_event combined with the bubbletea -> crossterm key
// translation. Unrecognized keys map to a zero (Char(0)) binding which never
// matches a configured action.
func FromKeyMsg(msg tea.KeyMsg) KeyBinding {
	key, mods := keyMsgToParts(msg)
	key, mods = normalizeKeyParts(key, mods)
	return KeyBinding{key: key, modifiers: mods}
}

// IsPress reports whether a key event triggers this binding, applying the
// cross-terminal normalization both sides share.
//
// Port of KeyBinding::is_press. The bubbletea runtime only delivers press/repeat
// key events (it has no release event), so the kind guard is implicit.
func (b KeyBinding) IsPress(msg tea.KeyMsg) bool {
	bk, bm := normalizeKeyParts(b.key, b.modifiers)
	ek, em := keyMsgToParts(msg)
	ek, em = normalizeKeyParts(ek, em)
	return bk == ek && bm == em
}

// DisplayLabel renders a concise human label for hint display.
//
// Port of KeyBinding::display_label (modifiers prefix + key name).
func (b KeyBinding) DisplayLabel() string {
	var sb strings.Builder
	if b.modifiers.Has(ModControl) {
		sb.WriteString("ctrl + ")
	}
	if b.modifiers.Has(ModShift) {
		sb.WriteString("shift + ")
	}
	if b.modifiers.Has(ModAlt) {
		sb.WriteString(altPrefix)
	}
	sb.WriteString(keyLabel(b.key))
	return sb.String()
}

// altPrefix mirrors key_hint.rs: the option glyph on macOS, "alt + " elsewhere.
// Go cannot conditionally compile per-OS without build tags; the macOS glyph is
// the upstream default for the (test) and macos configurations, so we use it.
const altPrefix = "⌥ + "

func keyLabel(k KeyCode) string {
	switch k.Kind {
	case KeyCodeEnter:
		return "enter"
	case KeyCodeChar:
		if k.Ch == ' ' {
			return "space"
		}
		return strings.ToLower(string(k.Ch))
	case KeyCodeUp:
		return "↑"
	case KeyCodeDown:
		return "↓"
	case KeyCodeLeft:
		return "←"
	case KeyCodeRight:
		return "→"
	case KeyCodePageUp:
		return "pgup"
	case KeyCodePageDown:
		return "pgdn"
	case KeyCodeTab:
		return "tab"
	case KeyCodeBackspace:
		return "backspace"
	case KeyCodeEsc:
		return "esc"
	case KeyCodeDelete:
		return "delete"
	case KeyCodeInsert:
		return "insert"
	case KeyCodeHome:
		return "home"
	case KeyCodeEnd:
		return "end"
	case KeyCodeF:
		return fmt.Sprintf("f%d", k.F)
	default:
		return ""
	}
}

// normalizeKeyParts folds cross-terminal key reporting quirks into a canonical
// form so a binding and an event compare equal when they should.
//
// Port of key_hint::normalize_key_parts:
//   - An unmodified raw C0 control character is converted to Ctrl+<letter>.
//   - An uppercase ASCII char gains SHIFT and lowercases.
func normalizeKeyParts(key KeyCode, mods KeyModifiers) (KeyCode, KeyModifiers) {
	if key.Kind != KeyCodeChar {
		return key, mods
	}
	ch := key.Ch
	if mods == ModNone {
		if ctrlCh, ok := c0ControlCharToCtrlChar(ch); ok {
			return Char(ctrlCh), ModControl
		}
	}
	if ch >= 'A' && ch <= 'Z' {
		return Char(ch + ('a' - 'A')), mods | ModShift
	}
	return key, mods
}

// c0ControlCharToCtrlChar maps a raw C0 control rune to the letter/digit that a
// Ctrl chord would produce. Port of key_hint::c0_control_char_to_ctrl_char.
func c0ControlCharToCtrlChar(ch rune) (rune, bool) {
	code := uint32(ch)
	switch {
	case code == 0x00:
		return ' ', true
	case code >= 0x01 && code <= 0x1a:
		return rune(code - 0x01 + uint32('a')), true
	case code >= 0x1c && code <= 0x1f:
		return rune(code - 0x1c + uint32('4')), true
	default:
		return 0, false
	}
}

// keyMsgToParts translates a bubbletea key event into a (KeyCode, KeyModifiers)
// pair. bubbletea reports modifiers via the alt flag and the textual key name;
// Ctrl chords arrive as either a "ctrl+x" type string or a raw control rune.
func keyMsgToParts(msg tea.KeyMsg) (KeyCode, KeyModifiers) {
	var mods KeyModifiers
	if msg.Alt {
		mods |= ModAlt
	}

	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace:
		runes := msg.Runes
		if msg.Type == tea.KeySpace || len(runes) == 0 {
			return Char(' '), mods
		}
		return Char(runes[0]), mods
	case tea.KeyEnter:
		return KeyEnter, mods
	case tea.KeyTab:
		return KeyTab, mods
	case tea.KeyShiftTab:
		return KeyTab, mods | ModShift
	case tea.KeyBackspace:
		return KeyBackspace, mods
	case tea.KeyDelete:
		return KeyDelete, mods
	case tea.KeyEsc:
		return KeyEsc, mods
	case tea.KeyInsert:
		return KeyInsert, mods
	case tea.KeyUp:
		return KeyUp, mods
	case tea.KeyDown:
		return KeyDown, mods
	case tea.KeyLeft:
		return KeyLeft, mods
	case tea.KeyRight:
		return KeyRight, mods
	case tea.KeyShiftUp:
		return KeyUp, mods | ModShift
	case tea.KeyShiftDown:
		return KeyDown, mods | ModShift
	case tea.KeyShiftLeft:
		return KeyLeft, mods | ModShift
	case tea.KeyShiftRight:
		return KeyRight, mods | ModShift
	case tea.KeyHome:
		return KeyHome, mods
	case tea.KeyEnd:
		return KeyEnd, mods
	case tea.KeyPgUp:
		return KeyPageUp, mods
	case tea.KeyPgDown:
		return KeyPageDown, mods
	}

	if code, mod, ok := ctrlKeyMsgToParts(msg); ok {
		return code, mod | mods
	}
	if msg.Type >= tea.KeyF1 && msg.Type <= tea.KeyF12 {
		return FKey(uint8(msg.Type-tea.KeyF1) + 1), mods
	}
	return Char(0), mods
}

// ctrlKeyMsgToParts decodes bubbletea's Ctrl key constants (KeyCtrlA..KeyCtrlZ
// plus the C0 punctuation chords) into Char(letter)+ModControl.
func ctrlKeyMsgToParts(msg tea.KeyMsg) (KeyCode, KeyModifiers, bool) {
	switch msg.Type {
	case tea.KeyCtrlAt:
		return Char(' '), ModControl, true // Ctrl+@ == Ctrl+Space (NUL)
	case tea.KeyCtrlA, tea.KeyCtrlB, tea.KeyCtrlC, tea.KeyCtrlD, tea.KeyCtrlE,
		tea.KeyCtrlF, tea.KeyCtrlG, tea.KeyCtrlH, tea.KeyCtrlI, tea.KeyCtrlJ,
		tea.KeyCtrlK, tea.KeyCtrlL, tea.KeyCtrlM, tea.KeyCtrlN, tea.KeyCtrlO,
		tea.KeyCtrlP, tea.KeyCtrlQ, tea.KeyCtrlR, tea.KeyCtrlS, tea.KeyCtrlT,
		tea.KeyCtrlU, tea.KeyCtrlV, tea.KeyCtrlW, tea.KeyCtrlX, tea.KeyCtrlY,
		tea.KeyCtrlZ:
		letter := rune(msg.Type-tea.KeyCtrlA) + 'a'
		return Char(letter), ModControl, true
	case tea.KeyCtrlBackslash:
		return Char('4'), ModControl, true
	case tea.KeyCtrlCloseBracket:
		return Char('5'), ModControl, true
	case tea.KeyCtrlCaret:
		return Char('6'), ModControl, true
	case tea.KeyCtrlUnderscore:
		return Char('7'), ModControl, true
	default:
		return KeyCode{}, ModNone, false
	}
}

// KeyBindingList is a set of alternative bindings for a single action. Order is
// for UI hint selection only (see [PrimaryBinding]), never dispatch priority.
//
// Port of the [KeyBinding] slice semantics in key_hint.rs / keymap.rs.
type KeyBindingList []KeyBinding

// IsPressed reports whether any binding in the set matches the event.
//
// Port of KeyBindingListExt::is_pressed.
func (l KeyBindingList) IsPressed(msg tea.KeyMsg) bool {
	for _, b := range l {
		if b.IsPress(msg) {
			return true
		}
	}
	return false
}

// PrimaryBinding returns the first binding, the preferred concise UI hint.
//
// Port of keymap::primary_binding.
func PrimaryBinding(l KeyBindingList) (KeyBinding, bool) {
	if len(l) == 0 {
		return KeyBinding{}, false
	}
	return l[0], true
}

// IsPlainTextKeyEvent reports whether an event should be treated as literal text
// input. Searchable pickers call this before dispatching plain-character
// navigation so a configured j/k/h/l does not steal query input.
//
// Port of key_hint::is_plain_text_key_event.
func IsPlainTextKeyEvent(msg tea.KeyMsg) bool {
	if msg.Alt {
		return false
	}
	if msg.Type != tea.KeyRunes && msg.Type != tea.KeySpace {
		return false
	}
	for _, ch := range msg.Runes {
		if ch < 0x20 || ch == 0x7f {
			return false
		}
	}
	return true
}
