package tui

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseKeybinding parses one canonical key-spec string such as "ctrl-a",
// "shift-enter", or "page-down" into a [KeyBinding]. It returns ok=false for
// unrecognized specs so callers can produce config-path-aware diagnostics.
//
// Port of keymap::parse_keybinding. Modifier tokens (ctrl/alt/shift) may appear
// in any order and combination, terminated by exactly one key token. A trailing
// key name may itself contain hyphens (e.g. "ctrl--" is Ctrl plus "-").
func ParseKeybinding(spec string) (KeyBinding, bool) {
	parts := strings.Split(spec, "-")
	var mods KeyModifiers
	keyName := ""
	found := false
	consumed := 0

	for i, part := range parts {
		switch part {
		case "ctrl":
			mods |= ModControl
		case "alt":
			mods |= ModAlt
		case "shift":
			mods |= ModShift
		default:
			// The first non-modifier token is the key name, even when empty (the
			// empty token from a trailing hyphen denotes the literal '-' key).
			keyName = part
			found = true
			consumed = i + 1
			goto done
		}
		consumed = i + 1
	}
done:
	if !found {
		return KeyBinding{}, false
	}
	// Re-attach any hyphenated trailing segments to the key name. A trailing
	// empty segment yields "-" so specs like "ctrl--" map to Ctrl plus '-'.
	if consumed < len(parts) {
		keyName += "-" + strings.Join(parts[consumed:], "-")
	}

	key, ok := parseKeyName(keyName)
	if !ok {
		return KeyBinding{}, false
	}
	return NewKeyBinding(key, mods), true
}

// parseKeyName resolves a key-name token into a [KeyCode]. Port of the match in
// keymap::parse_keybinding.
func parseKeyName(name string) (KeyCode, bool) {
	switch name {
	case "enter":
		return KeyEnter, true
	case "tab":
		return KeyTab, true
	case "backspace":
		return KeyBackspace, true
	case "esc":
		return KeyEsc, true
	case "delete":
		return KeyDelete, true
	case "up":
		return KeyUp, true
	case "down":
		return KeyDown, true
	case "left":
		return KeyLeft, true
	case "right":
		return KeyRight, true
	case "home":
		return KeyHome, true
	case "end":
		return KeyEnd, true
	case "page-up":
		return KeyPageUp, true
	case "page-down":
		return KeyPageDown, true
	case "space":
		return Char(' '), true
	case "minus":
		return Char('-'), true
	}
	if len(name) == 1 {
		return Char(rune(name[0])), true
	}
	if strings.HasPrefix(name, "f") {
		n, err := strconv.Atoi(name[1:])
		if err != nil {
			return KeyCode{}, false
		}
		if n >= 1 && n <= 12 {
			return FKey(uint8(n)), true
		}
		return KeyCode{}, false
	}
	return KeyCode{}, false
}

// KeybindingsSpec is the parsed config value for one action: either a single
// spec string or a list of spec strings.
//
// Port of codex_config::types::KeybindingsSpec (the untagged String | [String]
// enum). A present-but-empty list unbinds the action.
type KeybindingsSpec struct {
	specs []string
}

// NewKeybindingsSpec builds a spec from zero or more raw key-spec strings.
func NewKeybindingsSpec(specs ...string) KeybindingsSpec {
	return KeybindingsSpec{specs: append([]string(nil), specs...)}
}

// Specs returns the raw spec strings in declaration order.
//
// Port of KeybindingsSpec::specs.
func (s KeybindingsSpec) Specs() []string { return s.specs }

// parseSpec parses a single spec value (string or []string) from a decoded TOML
// node. It returns ok=false when the node is not a string or string list.
func parseSpec(node any) (KeybindingsSpec, bool) {
	switch v := node.(type) {
	case string:
		return KeybindingsSpec{specs: []string{v}}, true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return KeybindingsSpec{}, false
			}
			out = append(out, s)
		}
		return KeybindingsSpec{specs: out}, true
	case []string:
		return KeybindingsSpec{specs: append([]string(nil), v...)}, true
	default:
		return KeybindingsSpec{}, false
	}
}

// parseBindings parses a spec into concrete bindings, de-duplicating while
// preserving first-seen order so the first key stays the primary UI hint.
//
// Port of keymap::parse_bindings. Errors are wrapped with the config path and a
// user-facing next step.
func parseBindings(spec KeybindingsSpec, path string) (KeyBindingList, error) {
	var parsed KeyBindingList
	for _, raw := range spec.specs {
		binding, ok := ParseKeybinding(raw)
		if !ok {
			return nil, fmt.Errorf(
				"invalid `%s` = `%s`: use values like `ctrl-a`, `shift-enter`, or `page-down`; "+
					"see the Codex keymap documentation for supported actions and examples",
				path, raw,
			)
		}
		if !containsBinding(parsed, binding) {
			parsed = append(parsed, binding)
		}
	}
	return parsed, nil
}

func containsBinding(list KeyBindingList, b KeyBinding) bool {
	for _, existing := range list {
		if existing == b {
			return true
		}
	}
	return false
}
