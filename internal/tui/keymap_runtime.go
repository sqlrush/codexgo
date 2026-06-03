package tui

import "fmt"

// RuntimeKeymap is the fully resolved keymap snapshot used by TUI input
// handlers. Resolution precedence is context-specific binding, then
// tui.keymap.global for actions that support global fallback, then built-in
// defaults.
//
// Port of codex-rs/tui/src/keymap.rs `RuntimeKeymap`. It is immutable once
// constructed; rebuild it from config rather than mutating a snapshot.
type RuntimeKeymap struct {
	App           AppKeymap
	Chat          ChatKeymap
	Composer      ComposerKeymap
	Editor        EditorKeymap
	VimNormal     VimNormalKeymap
	VimOperator   VimOperatorKeymap
	VimTextObject VimTextObjectKeymap
	Pager         PagerKeymap
	List          ListKeymap
	Approval      ApprovalKeymap
}

// AppKeymap holds the app-scope (tui.keymap.global) action bindings.
//
// Port of keymap.rs `AppKeymap`.
type AppKeymap struct {
	OpenTranscript     KeyBindingList
	OpenExternalEditor KeyBindingList
	Copy               KeyBindingList
	ClearTerminal      KeyBindingList
	ToggleVimMode      KeyBindingList
	ToggleFastMode     KeyBindingList
	ToggleRawOutput    KeyBindingList
}

// ChatKeymap holds chat-level bindings evaluated at the app event layer.
//
// Port of keymap.rs `ChatKeymap`.
type ChatKeymap struct {
	InterruptTurn           KeyBindingList
	DecreaseReasoningEffort KeyBindingList
	IncreaseReasoningEffort KeyBindingList
	EditQueuedMessage       KeyBindingList
}

// ComposerKeymap holds composer-level bindings.
//
// Port of keymap.rs `ComposerKeymap`. Submit, Queue and ToggleShortcuts support
// global fallback.
type ComposerKeymap struct {
	Submit                KeyBindingList
	Queue                 KeyBindingList
	ToggleShortcuts       KeyBindingList
	HistorySearchPrevious KeyBindingList
	HistorySearchNext     KeyBindingList
}

// EditorKeymap holds the composer textarea editing bindings.
//
// Port of keymap.rs `EditorKeymap`.
type EditorKeymap struct {
	InsertNewline      KeyBindingList
	MoveLeft           KeyBindingList
	MoveRight          KeyBindingList
	MoveUp             KeyBindingList
	MoveDown           KeyBindingList
	MoveWordLeft       KeyBindingList
	MoveWordRight      KeyBindingList
	MoveLineStart      KeyBindingList
	MoveLineEnd        KeyBindingList
	DeleteBackward     KeyBindingList
	DeleteForward      KeyBindingList
	DeleteBackwardWord KeyBindingList
	DeleteForwardWord  KeyBindingList
	KillLineStart      KeyBindingList
	KillWholeLine      KeyBindingList
	KillLineEnd        KeyBindingList
	Yank               KeyBindingList
}

// VimNormalKeymap holds Vim normal-mode bindings.
//
// Port of keymap.rs `VimNormalKeymap`.
type VimNormalKeymap struct {
	EnterInsert         KeyBindingList
	AppendAfterCursor   KeyBindingList
	AppendLineEnd       KeyBindingList
	InsertLineStart     KeyBindingList
	OpenLineBelow       KeyBindingList
	OpenLineAbove       KeyBindingList
	MoveLeft            KeyBindingList
	MoveRight           KeyBindingList
	MoveUp              KeyBindingList
	MoveDown            KeyBindingList
	MoveWordForward     KeyBindingList
	MoveWordBackward    KeyBindingList
	MoveWordEnd         KeyBindingList
	MoveLineStart       KeyBindingList
	MoveLineEnd         KeyBindingList
	DeleteChar          KeyBindingList
	SubstituteChar      KeyBindingList
	DeleteToLineEnd     KeyBindingList
	ChangeToLineEnd     KeyBindingList
	YankLine            KeyBindingList
	PasteAfter          KeyBindingList
	StartDeleteOperator KeyBindingList
	StartYankOperator   KeyBindingList
	StartChangeOperator KeyBindingList
	CancelOperator      KeyBindingList
}

// VimOperatorKeymap holds Vim operator-pending bindings (after d/y/c).
//
// Port of keymap.rs `VimOperatorKeymap`.
type VimOperatorKeymap struct {
	DeleteLine             KeyBindingList
	YankLine               KeyBindingList
	MotionLeft             KeyBindingList
	MotionRight            KeyBindingList
	MotionUp               KeyBindingList
	MotionDown             KeyBindingList
	MotionWordForward      KeyBindingList
	MotionWordBackward     KeyBindingList
	MotionWordEnd          KeyBindingList
	MotionLineStart        KeyBindingList
	MotionLineEnd          KeyBindingList
	SelectInnerTextObject  KeyBindingList
	SelectAroundTextObject KeyBindingList
	Cancel                 KeyBindingList
}

// VimTextObjectKeymap holds Vim text-object bindings (after i/a prefix).
//
// Port of keymap.rs `VimTextObjectKeymap`.
type VimTextObjectKeymap struct {
	Word        KeyBindingList
	BigWord     KeyBindingList
	Parentheses KeyBindingList
	Brackets    KeyBindingList
	Braces      KeyBindingList
	DoubleQuote KeyBindingList
	SingleQuote KeyBindingList
	Backtick    KeyBindingList
	Cancel      KeyBindingList
}

// PagerKeymap holds transcript/overlay pager bindings.
//
// Port of keymap.rs `PagerKeymap`.
type PagerKeymap struct {
	ScrollUp        KeyBindingList
	ScrollDown      KeyBindingList
	PageUp          KeyBindingList
	PageDown        KeyBindingList
	HalfPageUp      KeyBindingList
	HalfPageDown    KeyBindingList
	JumpTop         KeyBindingList
	JumpBottom      KeyBindingList
	Close           KeyBindingList
	CloseTranscript KeyBindingList
}

// ListKeymap holds generic list-picker navigation bindings.
//
// Port of keymap.rs `ListKeymap`.
type ListKeymap struct {
	MoveUp     KeyBindingList
	MoveDown   KeyBindingList
	MoveLeft   KeyBindingList
	MoveRight  KeyBindingList
	PageUp     KeyBindingList
	PageDown   KeyBindingList
	JumpTop    KeyBindingList
	JumpBottom KeyBindingList
	Accept     KeyBindingList
	Cancel     KeyBindingList
}

// ApprovalKeymap holds approval-modal bindings.
//
// Port of keymap.rs `ApprovalKeymap`.
type ApprovalKeymap struct {
	OpenFullscreen    KeyBindingList
	OpenThread        KeyBindingList
	Approve           KeyBindingList
	ApproveForSession KeyBindingList
	ApproveForPrefix  KeyBindingList
	Deny              KeyBindingList
	Decline           KeyBindingList
	Cancel            KeyBindingList
}

// DefaultRuntimeKeymap returns the built-in defaults. This is a convenience for
// bootstrapping UI state before user config has been loaded; do not use it as a
// fallback after parsing config, as that would ignore explicit unbindings and
// conflict diagnostics.
//
// Port of RuntimeKeymap::defaults / built_in_defaults.
func DefaultRuntimeKeymap() RuntimeKeymap {
	return builtInDefaults()
}

// LoadRuntimeKeymap resolves a runtime keymap from the foundation config,
// applying precedence and validation. The keymap config is the opaque
// tui.keymap tree (config.Tui.Keymap).
//
// Port of RuntimeKeymap::from_config, adapted to decode the opaque keymap tree.
// Returns a wrapped error when a spec cannot be parsed or a context has
// ambiguous bindings.
func LoadRuntimeKeymap(keymapTree any) (RuntimeKeymap, error) {
	cfg := decodeKeymapConfig(keymapTree)
	rk, err := resolveRuntimeKeymap(cfg)
	if err != nil {
		return RuntimeKeymap{}, fmt.Errorf("resolve keymap: %w", err)
	}
	if err := rk.validateConflicts(); err != nil {
		return RuntimeKeymap{}, fmt.Errorf("validate keymap: %w", err)
	}
	return rk, nil
}

// resolveBindings resolves one action without global fallback. A missing config
// value inherits the built-in fallback; a configured value, including an empty
// list, replaces the fallback.
//
// Port of keymap::resolve_bindings.
func resolveBindings(configured *KeybindingsSpec, fallback KeyBindingList, path string) (KeyBindingList, error) {
	if configured == nil {
		return fallback, nil
	}
	return parseBindings(*configured, path)
}

// resolveWithGlobalFallback resolves one action with context -> global -> default
// precedence. A configured empty list at the context level is authoritative and
// unbinds the action.
//
// Port of keymap::resolve_bindings_with_global_fallback.
func resolveWithGlobalFallback(configured, global *KeybindingsSpec, fallback KeyBindingList, path string) (KeyBindingList, error) {
	if configured != nil {
		return parseBindings(*configured, path)
	}
	if global != nil {
		return parseBindings(*global, path)
	}
	return fallback, nil
}
