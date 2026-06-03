package tui

import (
	"strings"
	"testing"
)

func TestDefaultRuntimeKeymapBindings(t *testing.T) {
	rk := DefaultRuntimeKeymap()
	if got, _ := PrimaryBinding(rk.App.OpenTranscript); got != Ctrl(Char('t')) {
		t.Fatalf("open_transcript primary = %+v, want ctrl-t", got)
	}
	if got, _ := PrimaryBinding(rk.Composer.Submit); got != Plain(KeyEnter) {
		t.Fatalf("submit primary = %+v, want enter", got)
	}
	if got, _ := PrimaryBinding(rk.Chat.InterruptTurn); got != Plain(KeyEsc) {
		t.Fatalf("interrupt primary = %+v, want esc", got)
	}
	// vim_normal append_line_end carries both shift-a and plain A.
	if len(rk.VimNormal.AppendLineEnd) != 2 {
		t.Fatalf("append_line_end has %d bindings, want 2", len(rk.VimNormal.AppendLineEnd))
	}
}

func TestLoadRuntimeKeymapDefaultsWhenEmpty(t *testing.T) {
	rk, err := LoadRuntimeKeymap(nil)
	if err != nil {
		t.Fatalf("load nil keymap: %v", err)
	}
	if got, _ := PrimaryBinding(rk.App.Copy); got != Ctrl(Char('o')) {
		t.Fatalf("copy default = %+v, want ctrl-o", got)
	}
}

func TestLoadRuntimeKeymapOverride(t *testing.T) {
	// Use a key not claimed by any default to avoid a (correct) conflict.
	tree := map[string]any{
		"global": map[string]any{
			"copy": "f5",
		},
	}
	rk, err := LoadRuntimeKeymap(tree)
	if err != nil {
		t.Fatalf("load keymap: %v", err)
	}
	if got, _ := PrimaryBinding(rk.App.Copy); got != Plain(FKey(5)) {
		t.Fatalf("copy override = %+v, want f5", got)
	}
}

func TestLoadRuntimeKeymapArrayBindings(t *testing.T) {
	tree := map[string]any{
		"composer": map[string]any{
			"submit": []any{"ctrl-enter", "ctrl-shift-enter"},
		},
	}
	rk, err := LoadRuntimeKeymap(tree)
	if err != nil {
		t.Fatalf("load keymap: %v", err)
	}
	if len(rk.Composer.Submit) != 2 {
		t.Fatalf("submit has %d bindings, want 2", len(rk.Composer.Submit))
	}
}

func TestLoadRuntimeKeymapRejectsInvalidSpec(t *testing.T) {
	tree := map[string]any{
		"composer": map[string]any{
			"submit": "meta-enter",
		},
	}
	_, err := LoadRuntimeKeymap(tree)
	if err == nil {
		t.Fatal("expected error for meta-enter")
	}
	if !strings.Contains(err.Error(), "tui.keymap.composer.submit") {
		t.Fatalf("error should mention config path: %v", err)
	}
}

func TestLoadRuntimeKeymapDeduplicates(t *testing.T) {
	tree := map[string]any{
		"composer": map[string]any{
			"submit": []any{"ctrl-enter", "ctrl-enter"},
		},
	}
	rk, err := LoadRuntimeKeymap(tree)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(rk.Composer.Submit) != 1 {
		t.Fatalf("duplicates not collapsed: %d", len(rk.Composer.Submit))
	}
}

func TestComposerGlobalFallback(t *testing.T) {
	// A global submit binding applies when composer.submit is unset.
	tree := map[string]any{
		"global": map[string]any{
			"submit": "ctrl-shift-enter",
		},
	}
	rk, err := LoadRuntimeKeymap(tree)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got, _ := PrimaryBinding(rk.Composer.Submit); got != NewKeyBinding(KeyEnter, ModControl|ModShift) {
		t.Fatalf("submit fallback = %+v", got)
	}
}

func TestConflictAppShadowsComposerSubmit(t *testing.T) {
	tree := map[string]any{
		"global": map[string]any{
			"open_transcript": "ctrl-t",
		},
		"composer": map[string]any{
			"submit": "ctrl-t",
		},
	}
	_, err := LoadRuntimeKeymap(tree)
	if err == nil {
		t.Fatal("expected shadowing conflict")
	}
	if !strings.Contains(err.Error(), "composer.submit") || !strings.Contains(err.Error(), "open_transcript") {
		t.Fatalf("conflict message = %v", err)
	}
}

func TestConflictEditorShadowedByMain(t *testing.T) {
	tree := map[string]any{
		"global": map[string]any{
			"copy": "ctrl-y",
		},
		"editor": map[string]any{
			"yank": "ctrl-y",
		},
	}
	_, err := LoadRuntimeKeymap(tree)
	if err == nil {
		t.Fatal("expected editor shadow conflict")
	}
	if !strings.Contains(err.Error(), "editor.yank") || !strings.Contains(err.Error(), "copy") {
		t.Fatalf("conflict message = %v", err)
	}
}

func TestConflictReservedKey(t *testing.T) {
	// Binding an app action to Ctrl+C (reserved for interrupt/quit) is rejected.
	tree := map[string]any{
		"global": map[string]any{
			"copy": "ctrl-c",
		},
	}
	_, err := LoadRuntimeKeymap(tree)
	if err == nil {
		t.Fatal("expected reserved-key conflict")
	}
	if !strings.Contains(err.Error(), "fixed.interrupt_or_quit") {
		t.Fatalf("conflict message = %v", err)
	}
}

func TestAllowedOverlapClearTerminalListMoveRight(t *testing.T) {
	// Defaults intentionally let clear_terminal (ctrl-l) overlap list.move_right.
	if _, err := LoadRuntimeKeymap(nil); err != nil {
		t.Fatalf("default keymap should validate: %v", err)
	}
}
