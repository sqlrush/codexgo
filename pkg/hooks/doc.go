// Package hooks ports the codex Claude-compatible lifecycle hook engine from
// codex-rs/hooks.
//
// It models the ten lifecycle events (PreToolUse, PermissionRequest,
// PostToolUse, PreCompact, PostCompact, SessionStart, UserPromptSubmit,
// SubagentStart, SubagentStop, Stop), matcher evaluation, subprocess command
// execution with the per-event stdin payload, outcome merging across multiple
// matching handlers, and hook-state persistence merging.
//
// JSON wire shapes (the per-event command input/output schemas) match the Rust
// implementation byte-for-byte after key-order canonicalization. The Command
// hook configuration types are reused from internal/config; this package does
// not redefine them.
package hooks

import "github.com/sqlrush/codexgo/pkg/protocol"

// HookEventNames are the hook event labels as they appear in hooks JSON and
// config files, in canonical declaration order. Mirrors HOOK_EVENT_NAMES.
var HookEventNames = [10]string{
	"PreToolUse",
	"PermissionRequest",
	"PostToolUse",
	"PreCompact",
	"PostCompact",
	"SessionStart",
	"UserPromptSubmit",
	"SubagentStart",
	"SubagentStop",
	"Stop",
}

// HookEventNamesWithMatchers are the events whose matcher fields are meaningful
// during dispatch. Mirrors HOOK_EVENT_NAMES_WITH_MATCHERS.
var HookEventNamesWithMatchers = [8]string{
	"PreToolUse",
	"PermissionRequest",
	"PostToolUse",
	"PreCompact",
	"PostCompact",
	"SessionStart",
	"SubagentStart",
	"SubagentStop",
}

// HookEventKeyLabel returns the hook event label used in persisted hook-state
// keys (snake_case). Mirrors hook_event_key_label.
func HookEventKeyLabel(name protocol.HookEventName) string {
	switch name {
	case protocol.HookEventNamePreToolUse:
		return "pre_tool_use"
	case protocol.HookEventNamePermissionRequest:
		return "permission_request"
	case protocol.HookEventNamePostToolUse:
		return "post_tool_use"
	case protocol.HookEventNamePreCompact:
		return "pre_compact"
	case protocol.HookEventNamePostCompact:
		return "post_compact"
	case protocol.HookEventNameSessionStart:
		return "session_start"
	case protocol.HookEventNameUserPromptSubmit:
		return "user_prompt_submit"
	case protocol.HookEventNameSubagentStart:
		return "subagent_start"
	case protocol.HookEventNameSubagentStop:
		return "subagent_stop"
	case protocol.HookEventNameStop:
		return "stop"
	default:
		return string(name)
	}
}
