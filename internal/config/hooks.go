package config

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// HookHandlerKind discriminates the HookHandlerConfig internally-tagged enum
// (serde tag = "type").
type HookHandlerKind string

const (
	HookHandlerCommand HookHandlerKind = "command"
	HookHandlerPrompt  HookHandlerKind = "prompt"
	HookHandlerAgent   HookHandlerKind = "agent"
)

// HookHandlerConfig is an internally-tagged enum (tag = "type"). The Command
// variant carries fields; Prompt and Agent are empty.
type HookHandlerConfig struct {
	Kind HookHandlerKind

	// Command variant fields.
	Command        string
	CommandWindows *string
	TimeoutSec     *uint64
	Async          bool
	StatusMessage  *string
}

// MarshalJSON emits the tagged form with the Command variant's renamed fields.
func (h HookHandlerConfig) MarshalJSON() ([]byte, error) {
	switch h.Kind {
	case HookHandlerCommand:
		m := map[string]any{
			"type":    "command",
			"command": h.Command,
			"async":   h.Async,
		}
		if h.CommandWindows != nil {
			m["commandWindows"] = *h.CommandWindows
		}
		if h.TimeoutSec != nil {
			m["timeout"] = *h.TimeoutSec
		}
		if h.StatusMessage != nil {
			m["statusMessage"] = *h.StatusMessage
		}
		return json.Marshal(m)
	case HookHandlerPrompt:
		return json.Marshal(map[string]any{"type": "prompt"})
	case HookHandlerAgent:
		return json.Marshal(map[string]any{"type": "agent"})
	default:
		return nil, fmt.Errorf("unknown hook handler kind: %q", h.Kind)
	}
}

// UnmarshalJSON decodes the tagged form, honoring serde renames/aliases.
func (h *HookHandlerConfig) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("decode hook handler: %w", err)
	}
	switch HookHandlerKind(probe.Type) {
	case HookHandlerCommand:
		var c struct {
			Command        string  `json:"command"`
			CommandWindows *string `json:"commandWindows"`
			CommandWinAlt  *string `json:"command_windows"`
			Timeout        *uint64 `json:"timeout"`
			Async          bool    `json:"async"`
			StatusMessage  *string `json:"statusMessage"`
		}
		if err := json.Unmarshal(data, &c); err != nil {
			return fmt.Errorf("decode command hook: %w", err)
		}
		cw := c.CommandWindows
		if cw == nil {
			cw = c.CommandWinAlt
		}
		*h = HookHandlerConfig{
			Kind:           HookHandlerCommand,
			Command:        c.Command,
			CommandWindows: cw,
			TimeoutSec:     c.Timeout,
			Async:          c.Async,
			StatusMessage:  c.StatusMessage,
		}
		return nil
	case HookHandlerPrompt:
		*h = HookHandlerConfig{Kind: HookHandlerPrompt}
		return nil
	case HookHandlerAgent:
		*h = HookHandlerConfig{Kind: HookHandlerAgent}
		return nil
	default:
		return fmt.Errorf("unknown hook handler type: %q", probe.Type)
	}
}

// MatcherGroup pairs an optional matcher with a list of handlers.
type MatcherGroup struct {
	Matcher *string             `json:"matcher,omitempty" toml:"matcher,omitempty"`
	Hooks   []HookHandlerConfig `json:"hooks" toml:"hooks"`
}

// HookEventsToml maps each hook event name (PascalCase keys) to its matcher
// groups. Field renames mirror serde.
type HookEventsToml struct {
	PreToolUse        []MatcherGroup `json:"PreToolUse,omitempty" toml:"PreToolUse,omitempty"`
	PermissionRequest []MatcherGroup `json:"PermissionRequest,omitempty" toml:"PermissionRequest,omitempty"`
	PostToolUse       []MatcherGroup `json:"PostToolUse,omitempty" toml:"PostToolUse,omitempty"`
	PreCompact        []MatcherGroup `json:"PreCompact,omitempty" toml:"PreCompact,omitempty"`
	PostCompact       []MatcherGroup `json:"PostCompact,omitempty" toml:"PostCompact,omitempty"`
	SessionStart      []MatcherGroup `json:"SessionStart,omitempty" toml:"SessionStart,omitempty"`
	UserPromptSubmit  []MatcherGroup `json:"UserPromptSubmit,omitempty" toml:"UserPromptSubmit,omitempty"`
	SubagentStart     []MatcherGroup `json:"SubagentStart,omitempty" toml:"SubagentStart,omitempty"`
	SubagentStop      []MatcherGroup `json:"SubagentStop,omitempty" toml:"SubagentStop,omitempty"`
	Stop              []MatcherGroup `json:"Stop,omitempty" toml:"Stop,omitempty"`
}

// IsEmpty reports whether no hook events are configured.
func (e HookEventsToml) IsEmpty() bool {
	return len(e.PreToolUse) == 0 &&
		len(e.PermissionRequest) == 0 &&
		len(e.PostToolUse) == 0 &&
		len(e.PreCompact) == 0 &&
		len(e.PostCompact) == 0 &&
		len(e.SessionStart) == 0 &&
		len(e.UserPromptSubmit) == 0 &&
		len(e.SubagentStart) == 0 &&
		len(e.SubagentStop) == 0 &&
		len(e.Stop) == 0
}

// HandlerCount returns the total number of handlers across all events.
func (e HookEventsToml) HandlerCount() int {
	total := 0
	for _, groups := range e.allGroups() {
		for _, g := range groups {
			total += len(g.Hooks)
		}
	}
	return total
}

// EventGroup pairs a hook event name with its matcher groups.
type EventGroup struct {
	Event  protocol.HookEventName
	Groups []MatcherGroup
}

// IntoMatcherGroups returns the events in canonical order with their protocol
// event names.
func (e HookEventsToml) IntoMatcherGroups() []EventGroup {
	return []EventGroup{
		{protocol.HookEventNamePreToolUse, e.PreToolUse},
		{protocol.HookEventNamePermissionRequest, e.PermissionRequest},
		{protocol.HookEventNamePostToolUse, e.PostToolUse},
		{protocol.HookEventNamePreCompact, e.PreCompact},
		{protocol.HookEventNamePostCompact, e.PostCompact},
		{protocol.HookEventNameSessionStart, e.SessionStart},
		{protocol.HookEventNameUserPromptSubmit, e.UserPromptSubmit},
		{protocol.HookEventNameSubagentStart, e.SubagentStart},
		{protocol.HookEventNameSubagentStop, e.SubagentStop},
		{protocol.HookEventNameStop, e.Stop},
	}
}

func (e HookEventsToml) allGroups() [][]MatcherGroup {
	return [][]MatcherGroup{
		e.PreToolUse, e.PermissionRequest, e.PostToolUse, e.PreCompact,
		e.PostCompact, e.SessionStart, e.UserPromptSubmit, e.SubagentStart,
		e.SubagentStop, e.Stop,
	}
}

// HookStateToml is per-hook persisted state.
type HookStateToml struct {
	Enabled     *bool   `json:"enabled,omitempty" toml:"enabled,omitempty"`
	TrustedHash *string `json:"trusted_hash,omitempty" toml:"trusted_hash,omitempty"`
}

// HooksToml is the [hooks] table: flattened event groups plus a state map. The
// state map is skipped when empty.
type HooksToml struct {
	Events HookEventsToml           `json:"-" toml:"-"`
	State  map[string]HookStateToml `json:"state,omitempty" toml:"state,omitempty"`
}

// HooksFile is the standalone hooks.json/hooks.toml file shape.
type HooksFile struct {
	Hooks HookEventsToml `json:"hooks" toml:"hooks"`
}
