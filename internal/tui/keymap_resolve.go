package tui

// resolveRuntimeKeymap applies context -> global -> default precedence per
// action, returning a resolved (but not yet conflict-validated) keymap.
//
// Port of the body of keymap.rs RuntimeKeymap::from_config. The resolve_local!
// and resolve_with_global! macros are expanded inline as Go calls. Action names
// in the config maps use the snake_case keys upstream uses.
func resolveRuntimeKeymap(cfg keymapConfig) (RuntimeKeymap, error) {
	d := builtInDefaults()
	var rk RuntimeKeymap
	var err error

	// local resolves a context-local action without global fallback.
	local := func(ctx map[string]KeybindingsSpec, action string, fallback KeyBindingList, ctxName string) KeyBindingList {
		if err != nil {
			return nil
		}
		var out KeyBindingList
		out, err = resolveBindings(specRef(ctx, action), fallback, "tui.keymap."+ctxName+"."+action)
		return out
	}
	// global resolves a composer action with global fallback.
	withGlobal := func(action string, fallback KeyBindingList) KeyBindingList {
		if err != nil {
			return nil
		}
		var out KeyBindingList
		out, err = resolveWithGlobalFallback(
			specRef(cfg.composer, action), specRef(cfg.global, action),
			fallback, "tui.keymap.composer."+action,
		)
		return out
	}
	// appAction resolves an app-scope (global) action. Upstream reads these from
	// keymap.global with the path tui.keymap.global.<action>.
	appAction := func(action string, fallback KeyBindingList) KeyBindingList {
		if err != nil {
			return nil
		}
		var out KeyBindingList
		out, err = resolveBindings(specRef(cfg.global, action), fallback, "tui.keymap.global."+action)
		return out
	}

	rk.App = AppKeymap{
		OpenTranscript:     appAction("open_transcript", d.App.OpenTranscript),
		OpenExternalEditor: appAction("open_external_editor", d.App.OpenExternalEditor),
		Copy:               appAction("copy", d.App.Copy),
		ClearTerminal:      appAction("clear_terminal", d.App.ClearTerminal),
		ToggleVimMode:      appAction("toggle_vim_mode", d.App.ToggleVimMode),
		ToggleFastMode:     appAction("toggle_fast_mode", d.App.ToggleFastMode),
		ToggleRawOutput:    appAction("toggle_raw_output", d.App.ToggleRawOutput),
	}

	rk.Chat = ChatKeymap{
		InterruptTurn:           local(cfg.chat, "interrupt_turn", d.Chat.InterruptTurn, "chat"),
		DecreaseReasoningEffort: local(cfg.chat, "decrease_reasoning_effort", d.Chat.DecreaseReasoningEffort, "chat"),
		IncreaseReasoningEffort: local(cfg.chat, "increase_reasoning_effort", d.Chat.IncreaseReasoningEffort, "chat"),
		EditQueuedMessage:       local(cfg.chat, "edit_queued_message", d.Chat.EditQueuedMessage, "chat"),
	}

	rk.Composer = ComposerKeymap{
		Submit:                withGlobal("submit", d.Composer.Submit),
		Queue:                 withGlobal("queue", d.Composer.Queue),
		ToggleShortcuts:       withGlobal("toggle_shortcuts", d.Composer.ToggleShortcuts),
		HistorySearchPrevious: local(cfg.composer, "history_search_previous", d.Composer.HistorySearchPrevious, "composer"),
		HistorySearchNext:     local(cfg.composer, "history_search_next", d.Composer.HistorySearchNext, "composer"),
	}

	rk.Editor = EditorKeymap{
		InsertNewline:      local(cfg.editor, "insert_newline", d.Editor.InsertNewline, "editor"),
		MoveLeft:           local(cfg.editor, "move_left", d.Editor.MoveLeft, "editor"),
		MoveRight:          local(cfg.editor, "move_right", d.Editor.MoveRight, "editor"),
		MoveUp:             local(cfg.editor, "move_up", d.Editor.MoveUp, "editor"),
		MoveDown:           local(cfg.editor, "move_down", d.Editor.MoveDown, "editor"),
		MoveWordLeft:       local(cfg.editor, "move_word_left", d.Editor.MoveWordLeft, "editor"),
		MoveWordRight:      local(cfg.editor, "move_word_right", d.Editor.MoveWordRight, "editor"),
		MoveLineStart:      local(cfg.editor, "move_line_start", d.Editor.MoveLineStart, "editor"),
		MoveLineEnd:        local(cfg.editor, "move_line_end", d.Editor.MoveLineEnd, "editor"),
		DeleteBackward:     local(cfg.editor, "delete_backward", d.Editor.DeleteBackward, "editor"),
		DeleteForward:      local(cfg.editor, "delete_forward", d.Editor.DeleteForward, "editor"),
		DeleteBackwardWord: local(cfg.editor, "delete_backward_word", d.Editor.DeleteBackwardWord, "editor"),
		DeleteForwardWord:  local(cfg.editor, "delete_forward_word", d.Editor.DeleteForwardWord, "editor"),
		KillLineStart:      local(cfg.editor, "kill_line_start", d.Editor.KillLineStart, "editor"),
		KillWholeLine:      local(cfg.editor, "kill_whole_line", d.Editor.KillWholeLine, "editor"),
		KillLineEnd:        local(cfg.editor, "kill_line_end", d.Editor.KillLineEnd, "editor"),
		Yank:               local(cfg.editor, "yank", d.Editor.Yank, "editor"),
	}

	rk.VimNormal = VimNormalKeymap{
		EnterInsert:         local(cfg.vimNormal, "enter_insert", d.VimNormal.EnterInsert, "vim_normal"),
		AppendAfterCursor:   local(cfg.vimNormal, "append_after_cursor", d.VimNormal.AppendAfterCursor, "vim_normal"),
		AppendLineEnd:       local(cfg.vimNormal, "append_line_end", d.VimNormal.AppendLineEnd, "vim_normal"),
		InsertLineStart:     local(cfg.vimNormal, "insert_line_start", d.VimNormal.InsertLineStart, "vim_normal"),
		OpenLineBelow:       local(cfg.vimNormal, "open_line_below", d.VimNormal.OpenLineBelow, "vim_normal"),
		OpenLineAbove:       local(cfg.vimNormal, "open_line_above", d.VimNormal.OpenLineAbove, "vim_normal"),
		MoveLeft:            local(cfg.vimNormal, "move_left", d.VimNormal.MoveLeft, "vim_normal"),
		MoveRight:           local(cfg.vimNormal, "move_right", d.VimNormal.MoveRight, "vim_normal"),
		MoveUp:              local(cfg.vimNormal, "move_up", d.VimNormal.MoveUp, "vim_normal"),
		MoveDown:            local(cfg.vimNormal, "move_down", d.VimNormal.MoveDown, "vim_normal"),
		MoveWordForward:     local(cfg.vimNormal, "move_word_forward", d.VimNormal.MoveWordForward, "vim_normal"),
		MoveWordBackward:    local(cfg.vimNormal, "move_word_backward", d.VimNormal.MoveWordBackward, "vim_normal"),
		MoveWordEnd:         local(cfg.vimNormal, "move_word_end", d.VimNormal.MoveWordEnd, "vim_normal"),
		MoveLineStart:       local(cfg.vimNormal, "move_line_start", d.VimNormal.MoveLineStart, "vim_normal"),
		MoveLineEnd:         local(cfg.vimNormal, "move_line_end", d.VimNormal.MoveLineEnd, "vim_normal"),
		DeleteChar:          local(cfg.vimNormal, "delete_char", d.VimNormal.DeleteChar, "vim_normal"),
		SubstituteChar:      local(cfg.vimNormal, "substitute_char", d.VimNormal.SubstituteChar, "vim_normal"),
		DeleteToLineEnd:     local(cfg.vimNormal, "delete_to_line_end", d.VimNormal.DeleteToLineEnd, "vim_normal"),
		ChangeToLineEnd:     local(cfg.vimNormal, "change_to_line_end", d.VimNormal.ChangeToLineEnd, "vim_normal"),
		YankLine:            local(cfg.vimNormal, "yank_line", d.VimNormal.YankLine, "vim_normal"),
		PasteAfter:          local(cfg.vimNormal, "paste_after", d.VimNormal.PasteAfter, "vim_normal"),
		StartDeleteOperator: local(cfg.vimNormal, "start_delete_operator", d.VimNormal.StartDeleteOperator, "vim_normal"),
		StartYankOperator:   local(cfg.vimNormal, "start_yank_operator", d.VimNormal.StartYankOperator, "vim_normal"),
		StartChangeOperator: local(cfg.vimNormal, "start_change_operator", d.VimNormal.StartChangeOperator, "vim_normal"),
		CancelOperator:      local(cfg.vimNormal, "cancel_operator", d.VimNormal.CancelOperator, "vim_normal"),
	}

	rk.VimOperator = VimOperatorKeymap{
		DeleteLine:             local(cfg.vimOperator, "delete_line", d.VimOperator.DeleteLine, "vim_operator"),
		YankLine:               local(cfg.vimOperator, "yank_line", d.VimOperator.YankLine, "vim_operator"),
		MotionLeft:             local(cfg.vimOperator, "motion_left", d.VimOperator.MotionLeft, "vim_operator"),
		MotionRight:            local(cfg.vimOperator, "motion_right", d.VimOperator.MotionRight, "vim_operator"),
		MotionUp:               local(cfg.vimOperator, "motion_up", d.VimOperator.MotionUp, "vim_operator"),
		MotionDown:             local(cfg.vimOperator, "motion_down", d.VimOperator.MotionDown, "vim_operator"),
		MotionWordForward:      local(cfg.vimOperator, "motion_word_forward", d.VimOperator.MotionWordForward, "vim_operator"),
		MotionWordBackward:     local(cfg.vimOperator, "motion_word_backward", d.VimOperator.MotionWordBackward, "vim_operator"),
		MotionWordEnd:          local(cfg.vimOperator, "motion_word_end", d.VimOperator.MotionWordEnd, "vim_operator"),
		MotionLineStart:        local(cfg.vimOperator, "motion_line_start", d.VimOperator.MotionLineStart, "vim_operator"),
		MotionLineEnd:          local(cfg.vimOperator, "motion_line_end", d.VimOperator.MotionLineEnd, "vim_operator"),
		SelectInnerTextObject:  local(cfg.vimOperator, "select_inner_text_object", d.VimOperator.SelectInnerTextObject, "vim_operator"),
		SelectAroundTextObject: local(cfg.vimOperator, "select_around_text_object", d.VimOperator.SelectAroundTextObject, "vim_operator"),
		Cancel:                 local(cfg.vimOperator, "cancel", d.VimOperator.Cancel, "vim_operator"),
	}

	rk.VimTextObject = VimTextObjectKeymap{
		Word:        local(cfg.vimTextObject, "word", d.VimTextObject.Word, "vim_text_object"),
		BigWord:     local(cfg.vimTextObject, "big_word", d.VimTextObject.BigWord, "vim_text_object"),
		Parentheses: local(cfg.vimTextObject, "parentheses", d.VimTextObject.Parentheses, "vim_text_object"),
		Brackets:    local(cfg.vimTextObject, "brackets", d.VimTextObject.Brackets, "vim_text_object"),
		Braces:      local(cfg.vimTextObject, "braces", d.VimTextObject.Braces, "vim_text_object"),
		DoubleQuote: local(cfg.vimTextObject, "double_quote", d.VimTextObject.DoubleQuote, "vim_text_object"),
		SingleQuote: local(cfg.vimTextObject, "single_quote", d.VimTextObject.SingleQuote, "vim_text_object"),
		Backtick:    local(cfg.vimTextObject, "backtick", d.VimTextObject.Backtick, "vim_text_object"),
		Cancel:      local(cfg.vimTextObject, "cancel", d.VimTextObject.Cancel, "vim_text_object"),
	}

	rk.Pager = PagerKeymap{
		ScrollUp:        local(cfg.pager, "scroll_up", d.Pager.ScrollUp, "pager"),
		ScrollDown:      local(cfg.pager, "scroll_down", d.Pager.ScrollDown, "pager"),
		PageUp:          local(cfg.pager, "page_up", d.Pager.PageUp, "pager"),
		PageDown:        local(cfg.pager, "page_down", d.Pager.PageDown, "pager"),
		HalfPageUp:      local(cfg.pager, "half_page_up", d.Pager.HalfPageUp, "pager"),
		HalfPageDown:    local(cfg.pager, "half_page_down", d.Pager.HalfPageDown, "pager"),
		JumpTop:         local(cfg.pager, "jump_top", d.Pager.JumpTop, "pager"),
		JumpBottom:      local(cfg.pager, "jump_bottom", d.Pager.JumpBottom, "pager"),
		Close:           local(cfg.pager, "close", d.Pager.Close, "pager"),
		CloseTranscript: local(cfg.pager, "close_transcript", d.Pager.CloseTranscript, "pager"),
	}

	rk.List = ListKeymap{
		MoveUp:     local(cfg.list, "move_up", d.List.MoveUp, "list"),
		MoveDown:   local(cfg.list, "move_down", d.List.MoveDown, "list"),
		MoveLeft:   local(cfg.list, "move_left", d.List.MoveLeft, "list"),
		MoveRight:  local(cfg.list, "move_right", d.List.MoveRight, "list"),
		PageUp:     local(cfg.list, "page_up", d.List.PageUp, "list"),
		PageDown:   local(cfg.list, "page_down", d.List.PageDown, "list"),
		JumpTop:    local(cfg.list, "jump_top", d.List.JumpTop, "list"),
		JumpBottom: local(cfg.list, "jump_bottom", d.List.JumpBottom, "list"),
		Accept:     local(cfg.list, "accept", d.List.Accept, "list"),
		Cancel:     local(cfg.list, "cancel", d.List.Cancel, "list"),
	}

	rk.Approval = ApprovalKeymap{
		OpenFullscreen:    local(cfg.approval, "open_fullscreen", d.Approval.OpenFullscreen, "approval"),
		OpenThread:        local(cfg.approval, "open_thread", d.Approval.OpenThread, "approval"),
		Approve:           local(cfg.approval, "approve", d.Approval.Approve, "approval"),
		ApproveForSession: local(cfg.approval, "approve_for_session", d.Approval.ApproveForSession, "approval"),
		ApproveForPrefix:  local(cfg.approval, "approve_for_prefix", d.Approval.ApproveForPrefix, "approval"),
		Deny:              local(cfg.approval, "deny", d.Approval.Deny, "approval"),
		Decline:           local(cfg.approval, "decline", d.Approval.Decline, "approval"),
		Cancel:            local(cfg.approval, "cancel", d.Approval.Cancel, "approval"),
	}

	if err != nil {
		return RuntimeKeymap{}, err
	}
	return rk, nil
}
