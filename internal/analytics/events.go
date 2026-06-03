package analytics

import (
	"runtime"
)

// AppServerRpcTransport identifies the transport an app-server client uses.
// Mirrors Rust `AppServerRpcTransport` (serde rename_all = "snake_case").
type AppServerRpcTransport string

const (
	AppServerRpcTransportStdio     AppServerRpcTransport = "stdio"
	AppServerRpcTransportWebsocket AppServerRpcTransport = "websocket"
	AppServerRpcTransportInProcess AppServerRpcTransport = "in_process"
)

// CodexRuntimeMetadata describes the runtime that produced an analytics event.
// Mirrors Rust `CodexRuntimeMetadata`.
type CodexRuntimeMetadata struct {
	CodexRsVersion   string `json:"codex_rs_version"`
	RuntimeOs        string `json:"runtime_os"`
	RuntimeOsVersion string `json:"runtime_os_version"`
	RuntimeArch      string `json:"runtime_arch"`
}

// Version is the codex_rs_version reported in runtime metadata. It must match
// the codex release being ported (drop-in compatibility).
const Version = "0.136.0"

// CurrentRuntimeMetadata returns runtime metadata for the current process.
// Mirrors Rust `current_runtime_metadata`. runtime_os/runtime_arch use the same
// values as Rust's std::env::consts::{OS,ARCH}, which Go's runtime.GOOS/GOARCH
// match for the platforms codex supports.
func CurrentRuntimeMetadata() CodexRuntimeMetadata {
	return CodexRuntimeMetadata{
		CodexRsVersion:   Version,
		RuntimeOs:        runtime.GOOS,
		RuntimeOsVersion: osVersion(),
		RuntimeArch:      runtime.GOARCH,
	}
}

// CodexAppServerClientMetadata describes the connected app-server client.
// Mirrors Rust `CodexAppServerClientMetadata`.
type CodexAppServerClientMetadata struct {
	ProductClientID        string                `json:"product_client_id"`
	ClientName             *string               `json:"client_name"`
	ClientVersion          *string               `json:"client_version"`
	RpcTransport           AppServerRpcTransport `json:"rpc_transport"`
	ExperimentalApiEnabled *bool                 `json:"experimental_api_enabled"`
}

// SkillInvocationEventParams mirrors Rust `SkillInvocationEventParams`.
type SkillInvocationEventParams struct {
	ProductClientID *string         `json:"product_client_id"`
	SkillScope      *string         `json:"skill_scope"`
	PluginID        *string         `json:"plugin_id"`
	RepoURL         *string         `json:"repo_url"`
	ThreadID        *string         `json:"thread_id"`
	TurnID          *string         `json:"turn_id"`
	InvokeType      *InvocationType `json:"invoke_type"`
	ModelSlug       *string         `json:"model_slug"`
}

// SkillInvocationEventRequest mirrors Rust `SkillInvocationEventRequest`.
type SkillInvocationEventRequest struct {
	EventType   string                     `json:"event_type"`
	SkillID     string                     `json:"skill_id"`
	SkillName   string                     `json:"skill_name"`
	EventParams SkillInvocationEventParams `json:"event_params"`
}

// CodexHookRunMetadata mirrors Rust `CodexHookRunMetadata`.
type CodexHookRunMetadata struct {
	ThreadID   *string        `json:"thread_id"`
	TurnID     *string        `json:"turn_id"`
	ModelSlug  *string        `json:"model_slug"`
	HookName   *string        `json:"hook_name"`
	HookSource *string        `json:"hook_source"`
	Status     *HookRunStatus `json:"status"`
}

// CodexHookRunEventRequest mirrors Rust `CodexHookRunEventRequest`.
type CodexHookRunEventRequest struct {
	EventType   string               `json:"event_type"`
	EventParams CodexHookRunMetadata `json:"event_params"`
}

// CodexAppMetadata mirrors Rust `CodexAppMetadata`.
type CodexAppMetadata struct {
	ConnectorID     *string         `json:"connector_id"`
	ThreadID        *string         `json:"thread_id"`
	TurnID          *string         `json:"turn_id"`
	AppName         *string         `json:"app_name"`
	ProductClientID *string         `json:"product_client_id"`
	InvokeType      *InvocationType `json:"invoke_type"`
	ModelSlug       *string         `json:"model_slug"`
}

// CodexAppMentionedEventRequest mirrors Rust `CodexAppMentionedEventRequest`.
type CodexAppMentionedEventRequest struct {
	EventType   string           `json:"event_type"`
	EventParams CodexAppMetadata `json:"event_params"`
}

// CodexAppUsedEventRequest mirrors Rust `CodexAppUsedEventRequest`.
type CodexAppUsedEventRequest struct {
	EventType   string           `json:"event_type"`
	EventParams CodexAppMetadata `json:"event_params"`
}

// GuardianReviewEventPayload flattens the guardian review params alongside the
// session/runtime metadata. Mirrors Rust `GuardianReviewEventPayload` where
// guardian_review is `#[serde(flatten)]`.
type GuardianReviewEventPayload struct {
	SessionID       string
	AppServerClient CodexAppServerClientMetadata
	Runtime         CodexRuntimeMetadata
	GuardianReview  GuardianReviewEventParams
}

// GuardianReviewEventRequest mirrors Rust `GuardianReviewEventRequest`.
type GuardianReviewEventRequest struct {
	EventType   string                     `json:"event_type"`
	EventParams GuardianReviewEventPayload `json:"event_params"`
}

// CodexAcceptedLineFingerprintsEventParams mirrors Rust
// `CodexAcceptedLineFingerprintsEventParams`.
type CodexAcceptedLineFingerprintsEventParams struct {
	EventType            string                    `json:"event_type"`
	TurnID               string                    `json:"turn_id"`
	ThreadID             string                    `json:"thread_id"`
	ProductSurface       *string                   `json:"product_surface"`
	ModelSlug            *string                   `json:"model_slug"`
	CompletedAt          uint64                    `json:"completed_at"`
	RepoHash             *string                   `json:"repo_hash"`
	AcceptedAddedLines   uint64                    `json:"accepted_added_lines"`
	AcceptedDeletedLines uint64                    `json:"accepted_deleted_lines"`
	LineFingerprints     []AcceptedLineFingerprint `json:"line_fingerprints"`
}

// CodexAcceptedLineFingerprintsEventRequest mirrors Rust
// `CodexAcceptedLineFingerprintsEventRequest`.
type CodexAcceptedLineFingerprintsEventRequest struct {
	EventType   string                                   `json:"event_type"`
	EventParams CodexAcceptedLineFingerprintsEventParams `json:"event_params"`
}

// codexAppMetadata mirrors Rust `codex_app_metadata`.
func codexAppMetadata(tracking TrackEventsContext, app AppInvocation, productClientID string) CodexAppMetadata {
	threadID := tracking.ThreadID
	turnID := tracking.TurnID
	model := tracking.ModelSlug
	return CodexAppMetadata{
		ConnectorID:     app.ConnectorID,
		ThreadID:        &threadID,
		TurnID:          &turnID,
		AppName:         app.AppName,
		ProductClientID: &productClientID,
		InvokeType:      app.InvocationType,
		ModelSlug:       &model,
	}
}

// codexHookRunMetadata mirrors Rust `codex_hook_run_metadata`.
func codexHookRunMetadata(tracking TrackEventsContext, hook HookRunFact) CodexHookRunMetadata {
	threadID := tracking.ThreadID
	turnID := tracking.TurnID
	model := tracking.ModelSlug
	hookName := analyticsHookEventName(hook.EventName)
	hookSource := analyticsHookSource(hook.HookSource)
	status := analyticsHookStatus(hook.Status)
	return CodexHookRunMetadata{
		ThreadID:   &threadID,
		TurnID:     &turnID,
		ModelSlug:  &model,
		HookName:   &hookName,
		HookSource: &hookSource,
		Status:     &status,
	}
}
