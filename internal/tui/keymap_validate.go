package tui

import "fmt"

// actionBindings pairs a config-path-relative action name with its resolved
// bindings, used by the conflict validators.
type actionBindings struct {
	action   string
	bindings KeyBindingList
}

// overlap names an allowed primary/shadowed action pair plus the one binding
// they may legitimately share.
type overlap struct {
	primary  string
	shadowed string
	binding  KeyBinding
}

// reservedBinding names a fixed (non-configurable) action and the key it owns.
type reservedBinding struct {
	action  string
	binding KeyBinding
}

// validateConflicts rejects ambiguous bindings across scopes evaluated together.
//
// Port of keymap.rs RuntimeKeymap::validate_conflicts. The passes mirror the
// mixed runtime precedence: app actions run before the composer, main-surface
// handlers run before the editor, and certain fixed keys are reserved.
func (rk RuntimeKeymap) validateConflicts() error {
	appAndComposer := []actionBindings{
		{"open_transcript", rk.App.OpenTranscript},
		{"open_external_editor", rk.App.OpenExternalEditor},
		{"copy", rk.App.Copy},
		{"clear_terminal", rk.App.ClearTerminal},
		{"toggle_vim_mode", rk.App.ToggleVimMode},
		{"toggle_fast_mode", rk.App.ToggleFastMode},
		{"toggle_raw_output", rk.App.ToggleRawOutput},
		{"chat.interrupt_turn", rk.Chat.InterruptTurn},
		{"chat.decrease_reasoning_effort", rk.Chat.DecreaseReasoningEffort},
		{"chat.increase_reasoning_effort", rk.Chat.IncreaseReasoningEffort},
		{"chat.edit_queued_message", rk.Chat.EditQueuedMessage},
		{"composer.submit", rk.Composer.Submit},
		{"composer.queue", rk.Composer.Queue},
		{"composer.toggle_shortcuts", rk.Composer.ToggleShortcuts},
		{"composer.history_search_previous", rk.Composer.HistorySearchPrevious},
		{"composer.history_search_next", rk.Composer.HistorySearchNext},
	}
	if err := validateUnique("app", appAndComposer); err != nil {
		return err
	}

	if err := validateNoReserved("main", appAndComposer, mainReservedBindings, []overlap{
		{"chat.interrupt_turn", "fixed.backtrack", Plain(KeyEsc)},
	}); err != nil {
		return err
	}

	appActions := []actionBindings{
		{"open_transcript", rk.App.OpenTranscript},
		{"open_external_editor", rk.App.OpenExternalEditor},
		{"copy", rk.App.Copy},
		{"clear_terminal", rk.App.ClearTerminal},
		{"toggle_vim_mode", rk.App.ToggleVimMode},
		{"toggle_fast_mode", rk.App.ToggleFastMode},
		{"toggle_raw_output", rk.App.ToggleRawOutput},
	}
	listAndApproval := []actionBindings{
		{"list.move_up", rk.List.MoveUp},
		{"list.move_down", rk.List.MoveDown},
		{"list.move_left", rk.List.MoveLeft},
		{"list.move_right", rk.List.MoveRight},
		{"list.page_up", rk.List.PageUp},
		{"list.page_down", rk.List.PageDown},
		{"list.jump_top", rk.List.JumpTop},
		{"list.jump_bottom", rk.List.JumpBottom},
		{"list.accept", rk.List.Accept},
		{"list.cancel", rk.List.Cancel},
		{"approval.open_fullscreen", rk.Approval.OpenFullscreen},
		{"approval.open_thread", rk.Approval.OpenThread},
		{"approval.approve", rk.Approval.Approve},
		{"approval.approve_for_session", rk.Approval.ApproveForSession},
		{"approval.approve_for_prefix", rk.Approval.ApproveForPrefix},
		{"approval.deny", rk.Approval.Deny},
		{"approval.decline", rk.Approval.Decline},
		{"approval.cancel", rk.Approval.Cancel},
	}
	if err := validateNoShadow("app", appActions, listAndApproval, []overlap{
		{"clear_terminal", "list.move_right", Ctrl(Char('l'))},
	}); err != nil {
		return err
	}

	if err := validateNoShadow("request_user_input",
		[]actionBindings{{"chat.interrupt_turn", rk.Chat.InterruptTurn}},
		[]actionBindings{
			{"list.move_left", rk.List.MoveLeft},
			{"list.move_right", rk.List.MoveRight},
		}, nil); err != nil {
		return err
	}

	mainPrimary := []actionBindings{
		{"open_transcript", rk.App.OpenTranscript},
		{"open_external_editor", rk.App.OpenExternalEditor},
		{"copy", rk.App.Copy},
		{"clear_terminal", rk.App.ClearTerminal},
		{"chat.interrupt_turn", rk.Chat.InterruptTurn},
		{"chat.decrease_reasoning_effort", rk.Chat.DecreaseReasoningEffort},
		{"chat.increase_reasoning_effort", rk.Chat.IncreaseReasoningEffort},
		{"composer.submit", rk.Composer.Submit},
		{"toggle_vim_mode", rk.App.ToggleVimMode},
		{"toggle_fast_mode", rk.App.ToggleFastMode},
		{"toggle_raw_output", rk.App.ToggleRawOutput},
		{"composer.history_search_previous", rk.Composer.HistorySearchPrevious},
	}
	editorActions := editorActionBindings(rk.Editor)
	if err := validateNoShadow("main", mainPrimary, editorActions, []overlap{
		{"composer.submit", "editor.insert_newline", Plain(KeyEnter)},
	}); err != nil {
		return err
	}

	if err := validateUnique("editor", editorActions); err != nil {
		return err
	}

	if err := validateUnique("vim_normal", vimNormalActionBindings(rk.VimNormal)); err != nil {
		return err
	}
	if err := validateUnique("vim_operator", vimOperatorActionBindings(rk.VimOperator)); err != nil {
		return err
	}
	if err := validateUnique("vim_text_object", vimTextObjectActionBindings(rk.VimTextObject)); err != nil {
		return err
	}
	if err := validateUnique("pager", pagerActionBindings(rk.Pager)); err != nil {
		return err
	}
	if err := validateUnique("list", listActionBindings(rk.List)); err != nil {
		return err
	}
	if err := validateUnique("approval", approvalActionBindings(rk.Approval)); err != nil {
		return err
	}

	return nil
}

// validateUnique rejects two actions in the same context sharing a key.
//
// Port of keymap::validate_unique.
func validateUnique(context string, pairs []actionBindings) error {
	seen := map[KeyBinding]string{}
	for _, p := range pairs {
		for _, b := range p.bindings {
			if prev, ok := seen[b]; ok {
				return fmt.Errorf(
					"ambiguous `tui.keymap.%s` bindings: `%s` and `%s` use the same key; "+
						"set unique keys in `~/.codexgo/config.toml` and retry; "+
						"see the Codex keymap documentation for supported actions and examples",
					context, prev, p.action,
				)
			}
			seen[b] = p.action
		}
	}
	return nil
}

// validateNoShadow rejects a shadowed action sharing a key with a primary action
// unless the pair is explicitly allowed.
//
// Port of keymap::validate_no_shadow_with_allowed_overlaps.
func validateNoShadow(context string, primary, shadowed []actionBindings, allowed []overlap) error {
	seen := map[KeyBinding]string{}
	for _, p := range primary {
		for _, b := range p.bindings {
			seen[b] = p.action
		}
	}
	for _, s := range shadowed {
		for _, b := range s.bindings {
			prev, ok := seen[b]
			if !ok {
				continue
			}
			if overlapAllowed(allowed, prev, s.action, b) {
				continue
			}
			return fmt.Errorf(
				"ambiguous `tui.keymap.%s` bindings: `%s` shadows `%s` with the same key; "+
					"set unique keys in `~/.codexgo/config.toml` and retry; "+
					"see the Codex keymap documentation for supported actions and examples",
				context, prev, s.action,
			)
		}
	}
	return nil
}

// validateNoReserved rejects a configurable action using a fixed reserved key
// unless explicitly allowed.
//
// Port of keymap::validate_no_reserved.
func validateNoReserved(context string, pairs []actionBindings, reserved []reservedBinding, allowed []overlap) error {
	for _, p := range pairs {
		for _, b := range p.bindings {
			reservedAction, ok := reservedActionFor(reserved, b)
			if !ok {
				continue
			}
			if overlapAllowed(allowed, p.action, reservedAction, b) {
				continue
			}
			return fmt.Errorf(
				"ambiguous `tui.keymap.%s` bindings: `%s` uses a key reserved by `%s`; "+
					"set a different key in `~/.codexgo/config.toml` and retry; "+
					"see the Codex keymap documentation for supported actions and examples",
				context, p.action, reservedAction,
			)
		}
	}
	return nil
}

func overlapAllowed(allowed []overlap, primary, shadowed string, b KeyBinding) bool {
	for _, a := range allowed {
		if a.primary == primary && a.shadowed == shadowed && a.binding == b {
			return true
		}
	}
	return false
}

func reservedActionFor(reserved []reservedBinding, b KeyBinding) (string, bool) {
	for _, r := range reserved {
		if r.binding == b {
			return r.action, true
		}
	}
	return "", false
}

// mainReservedBindings lists the fixed main-surface keys configurable actions
// must not reuse. Port of keymap::MAIN_RESERVED_BINDINGS.
var mainReservedBindings = []reservedBinding{
	{"fixed.interrupt_or_quit", Ctrl(Char('c'))},
	{"fixed.quit", Ctrl(Char('d'))},
	{"fixed.paste_image", Ctrl(Char('v'))},
	{"fixed.paste_image", CtrlAlt(Char('v'))},
	{"fixed.cycle_collaboration_mode", Shift(KeyTab)},
	{"fixed.backtrack", Plain(KeyEsc)},
	{"fixed.previous_agent", Alt(KeyLeft)},
	{"fixed.next_agent", Alt(KeyRight)},
	{"fixed.slash_command", Plain(Char('/'))},
	{"fixed.shell_command", Plain(Char('!'))},
	{"fixed.file_paths", Plain(Char('@'))},
	{"fixed.connector_mentions", Plain(Char('$'))},
}

func editorActionBindings(e EditorKeymap) []actionBindings {
	return []actionBindings{
		{"editor.insert_newline", e.InsertNewline},
		{"editor.move_left", e.MoveLeft},
		{"editor.move_right", e.MoveRight},
		{"editor.move_up", e.MoveUp},
		{"editor.move_down", e.MoveDown},
		{"editor.move_word_left", e.MoveWordLeft},
		{"editor.move_word_right", e.MoveWordRight},
		{"editor.move_line_start", e.MoveLineStart},
		{"editor.move_line_end", e.MoveLineEnd},
		{"editor.delete_backward", e.DeleteBackward},
		{"editor.delete_forward", e.DeleteForward},
		{"editor.delete_backward_word", e.DeleteBackwardWord},
		{"editor.delete_forward_word", e.DeleteForwardWord},
		{"editor.kill_line_start", e.KillLineStart},
		{"editor.kill_whole_line", e.KillWholeLine},
		{"editor.kill_line_end", e.KillLineEnd},
		{"editor.yank", e.Yank},
	}
}

func vimNormalActionBindings(v VimNormalKeymap) []actionBindings {
	return []actionBindings{
		{"enter_insert", v.EnterInsert},
		{"append_after_cursor", v.AppendAfterCursor},
		{"append_line_end", v.AppendLineEnd},
		{"insert_line_start", v.InsertLineStart},
		{"open_line_below", v.OpenLineBelow},
		{"open_line_above", v.OpenLineAbove},
		{"move_left", v.MoveLeft},
		{"move_right", v.MoveRight},
		{"move_up", v.MoveUp},
		{"move_down", v.MoveDown},
		{"move_word_forward", v.MoveWordForward},
		{"move_word_backward", v.MoveWordBackward},
		{"move_word_end", v.MoveWordEnd},
		{"move_line_start", v.MoveLineStart},
		{"move_line_end", v.MoveLineEnd},
		{"delete_char", v.DeleteChar},
		{"substitute_char", v.SubstituteChar},
		{"delete_to_line_end", v.DeleteToLineEnd},
		{"change_to_line_end", v.ChangeToLineEnd},
		{"yank_line", v.YankLine},
		{"paste_after", v.PasteAfter},
		{"start_delete_operator", v.StartDeleteOperator},
		{"start_yank_operator", v.StartYankOperator},
		{"start_change_operator", v.StartChangeOperator},
		{"cancel_operator", v.CancelOperator},
	}
}

func vimOperatorActionBindings(v VimOperatorKeymap) []actionBindings {
	return []actionBindings{
		{"delete_line", v.DeleteLine},
		{"yank_line", v.YankLine},
		{"motion_left", v.MotionLeft},
		{"motion_right", v.MotionRight},
		{"motion_up", v.MotionUp},
		{"motion_down", v.MotionDown},
		{"motion_word_forward", v.MotionWordForward},
		{"motion_word_backward", v.MotionWordBackward},
		{"motion_word_end", v.MotionWordEnd},
		{"motion_line_start", v.MotionLineStart},
		{"motion_line_end", v.MotionLineEnd},
		{"select_inner_text_object", v.SelectInnerTextObject},
		{"select_around_text_object", v.SelectAroundTextObject},
		{"cancel", v.Cancel},
	}
}

func vimTextObjectActionBindings(v VimTextObjectKeymap) []actionBindings {
	return []actionBindings{
		{"word", v.Word},
		{"big_word", v.BigWord},
		{"parentheses", v.Parentheses},
		{"brackets", v.Brackets},
		{"braces", v.Braces},
		{"double_quote", v.DoubleQuote},
		{"single_quote", v.SingleQuote},
		{"backtick", v.Backtick},
		{"cancel", v.Cancel},
	}
}

func pagerActionBindings(p PagerKeymap) []actionBindings {
	return []actionBindings{
		{"scroll_up", p.ScrollUp},
		{"scroll_down", p.ScrollDown},
		{"page_up", p.PageUp},
		{"page_down", p.PageDown},
		{"half_page_up", p.HalfPageUp},
		{"half_page_down", p.HalfPageDown},
		{"jump_top", p.JumpTop},
		{"jump_bottom", p.JumpBottom},
		{"close", p.Close},
		{"close_transcript", p.CloseTranscript},
	}
}

func listActionBindings(l ListKeymap) []actionBindings {
	return []actionBindings{
		{"move_up", l.MoveUp},
		{"move_down", l.MoveDown},
		{"move_left", l.MoveLeft},
		{"move_right", l.MoveRight},
		{"page_up", l.PageUp},
		{"page_down", l.PageDown},
		{"jump_top", l.JumpTop},
		{"jump_bottom", l.JumpBottom},
		{"accept", l.Accept},
		{"cancel", l.Cancel},
	}
}

func approvalActionBindings(a ApprovalKeymap) []actionBindings {
	return []actionBindings{
		{"open_fullscreen", a.OpenFullscreen},
		{"open_thread", a.OpenThread},
		{"approve", a.Approve},
		{"approve_for_session", a.ApproveForSession},
		{"approve_for_prefix", a.ApproveForPrefix},
		{"deny", a.Deny},
		{"decline", a.Decline},
		{"cancel", a.Cancel},
	}
}
