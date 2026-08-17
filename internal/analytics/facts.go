package analytics

import (
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// NowUnixSeconds returns the current Unix time in seconds. Mirrors Rust
// `now_unix_seconds`.
func NowUnixSeconds() uint64 {
	return uint64(time.Now().Unix())
}

// NowUnixMillis returns the current Unix time in milliseconds. Mirrors Rust
// `now_unix_millis`.
func NowUnixMillis() uint64 {
	return uint64(time.Now().UnixMilli())
}

// TrackEventsContext carries the per-turn identifiers attached to analytics
// events. Mirrors Rust `TrackEventsContext`.
type TrackEventsContext struct {
	ModelSlug string
	ThreadID  string
	TurnID    string
}

// BuildTrackEventsContext constructs a [TrackEventsContext]. Mirrors Rust
// `build_track_events_context`.
func BuildTrackEventsContext(modelSlug, threadID, turnID string) TrackEventsContext {
	return TrackEventsContext{ModelSlug: modelSlug, ThreadID: threadID, TurnID: turnID}
}

// InvocationType distinguishes explicit vs implicit invocations. Mirrors Rust
// `InvocationType` (serde rename_all = "lowercase").
type InvocationType string

const (
	InvocationTypeExplicit InvocationType = "explicit"
	InvocationTypeImplicit InvocationType = "implicit"
)

// SkillScope mirrors codex_protocol::protocol::SkillScope used by skill
// invocations. The analytics layer only needs the snake_case string form.
type SkillScope string

const (
	SkillScopeUser    SkillScope = "user"
	SkillScopeProject SkillScope = "project"
	SkillScopePlugin  SkillScope = "plugin"
	SkillScopeBuiltin SkillScope = "builtin"
)

// SkillInvocation records a single skill invocation. Mirrors Rust
// `SkillInvocation`.
type SkillInvocation struct {
	SkillName      string
	SkillScope     SkillScope
	SkillPath      string
	PluginID       *string
	InvocationType InvocationType
}

// HookEventName enumerates hook lifecycle events. Mirrors
// codex_protocol::protocol::HookEventName as used by analytics.
type HookEventName string

const (
	HookEventNamePreToolUse        HookEventName = "PreToolUse"
	HookEventNamePermissionRequest HookEventName = "PermissionRequest"
	HookEventNamePostToolUse       HookEventName = "PostToolUse"
	HookEventNamePreCompact        HookEventName = "PreCompact"
	HookEventNamePostCompact       HookEventName = "PostCompact"
	HookEventNameSessionStart      HookEventName = "SessionStart"
	HookEventNameUserPromptSubmit  HookEventName = "UserPromptSubmit"
	HookEventNameSubagentStart     HookEventName = "SubagentStart"
	HookEventNameSubagentStop      HookEventName = "SubagentStop"
	HookEventNameStop              HookEventName = "Stop"
)

// HookSource enumerates the origin of a hook. Mirrors
// codex_protocol::protocol::HookSource.
type HookSource string

const (
	HookSourceSystem                  HookSource = "system"
	HookSourceUser                    HookSource = "user"
	HookSourceProject                 HookSource = "project"
	HookSourceMdm                     HookSource = "mdm"
	HookSourceSessionFlags            HookSource = "session_flags"
	HookSourcePlugin                  HookSource = "plugin"
	HookSourceCloudRequirements       HookSource = "cloud_requirements"
	HookSourceLegacyManagedConfigFile HookSource = "legacy_managed_config_file"
	HookSourceLegacyManagedConfigMdm  HookSource = "legacy_managed_config_mdm"
	HookSourceUnknown                 HookSource = "unknown"
)

// HookRunStatus enumerates the terminal status of a hook run. Mirrors
// codex_protocol::protocol::HookRunStatus (serde rename_all = "snake_case").
type HookRunStatus string

const (
	HookRunStatusRunning   HookRunStatus = "running"
	HookRunStatusCompleted HookRunStatus = "completed"
	HookRunStatusFailed    HookRunStatus = "failed"
	HookRunStatusSkipped   HookRunStatus = "skipped"
	HookRunStatusBlocked   HookRunStatus = "blocked"
)

// HookRunFact records the outcome of a single hook run. Mirrors Rust
// `HookRunFact`.
type HookRunFact struct {
	EventName  HookEventName
	HookSource HookSource
	Status     HookRunStatus
}

// TurnTokenUsageFact reports token usage for a completed turn. Mirrors Rust
// `TurnTokenUsageFact`.
type TurnTokenUsageFact struct {
	TurnID     string
	ThreadID   string
	TokenUsage protocol.TokenUsage
}

// AppInvocation describes an app/connector mention or use. Mirrors Rust
// `AppInvocation`.
type AppInvocation struct {
	ConnectorID    *string
	AppName        *string
	InvocationType *InvocationType
}

// analyticsHookEventName mirrors Rust `analytics_hook_event_name`. The values
// already match the canonical names; the function exists to keep the mapping
// explicit and faithful.
func analyticsHookEventName(name HookEventName) string {
	switch name {
	case HookEventNamePreToolUse:
		return "PreToolUse"
	case HookEventNamePermissionRequest:
		return "PermissionRequest"
	case HookEventNamePostToolUse:
		return "PostToolUse"
	case HookEventNamePreCompact:
		return "PreCompact"
	case HookEventNamePostCompact:
		return "PostCompact"
	case HookEventNameSessionStart:
		return "SessionStart"
	case HookEventNameUserPromptSubmit:
		return "UserPromptSubmit"
	case HookEventNameSubagentStart:
		return "SubagentStart"
	case HookEventNameSubagentStop:
		return "SubagentStop"
	case HookEventNameStop:
		return "Stop"
	default:
		return string(name)
	}
}

// analyticsHookSource mirrors Rust `analytics_hook_source`.
func analyticsHookSource(source HookSource) string {
	switch source {
	case HookSourceSystem:
		return "system"
	case HookSourceUser:
		return "user"
	case HookSourceProject:
		return "project"
	case HookSourceMdm:
		return "mdm"
	case HookSourceSessionFlags:
		return "session_flags"
	case HookSourcePlugin:
		return "plugin"
	case HookSourceCloudRequirements:
		return "cloud_requirements"
	case HookSourceLegacyManagedConfigFile:
		return "legacy_managed_config_file"
	case HookSourceLegacyManagedConfigMdm:
		return "legacy_managed_config_mdm"
	default:
		return "unknown"
	}
}

// analyticsHookStatus mirrors Rust `analytics_hook_status`: Running is
// normalized to Failed defensively.
func analyticsHookStatus(status HookRunStatus) HookRunStatus {
	if status == HookRunStatusRunning {
		return HookRunStatusFailed
	}
	return status
}
