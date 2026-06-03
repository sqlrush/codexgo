package hooks

import (
	"fmt"
	"sort"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// CommandShell describes how hook commands are invoked. An empty Program uses
// the platform default shell ("/bin/sh -lc" on Unix, "cmd.exe /C" on Windows).
// Mirrors CommandShell.
type CommandShell struct {
	Program string
	Args    []string
}

// ConfiguredHandler is one resolved command hook handler ready for dispatch.
// Mirrors ConfiguredHandler.
type ConfiguredHandler struct {
	EventName     protocol.HookEventName
	Matcher       *string
	Command       string
	TimeoutSec    uint64
	StatusMessage *string
	SourcePath    abspath.AbsolutePathBuf
	Source        protocol.HookSource
	DisplayOrder  int64
	Env           map[string]string
}

// RunID returns the stable run identifier for this handler. Mirrors
// ConfiguredHandler::run_id.
func (h ConfiguredHandler) RunID() string {
	return fmt.Sprintf("%s:%d:%s", h.eventNameLabel(), h.DisplayOrder, h.SourcePath.String())
}

func (h ConfiguredHandler) eventNameLabel() string {
	switch h.EventName {
	case protocol.HookEventNamePreToolUse:
		return "pre-tool-use"
	case protocol.HookEventNamePermissionRequest:
		return "permission-request"
	case protocol.HookEventNamePostToolUse:
		return "post-tool-use"
	case protocol.HookEventNamePreCompact:
		return "pre-compact"
	case protocol.HookEventNamePostCompact:
		return "post-compact"
	case protocol.HookEventNameSessionStart:
		return "session-start"
	case protocol.HookEventNameUserPromptSubmit:
		return "user-prompt-submit"
	case protocol.HookEventNameSubagentStart:
		return "subagent-start"
	case protocol.HookEventNameSubagentStop:
		return "subagent-stop"
	case protocol.HookEventNameStop:
		return "stop"
	default:
		return string(h.EventName)
	}
}

// sortedEnvKeys returns the handler env keys in deterministic order so that
// stdin payloads and command env are reproducible.
func (h ConfiguredHandler) sortedEnvKeys() []string {
	keys := make([]string, 0, len(h.Env))
	for k := range h.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
