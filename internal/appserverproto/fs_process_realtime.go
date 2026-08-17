package appserverproto

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// This file ports the v2 app-server protocol surfaces for filesystem access
// (fs/*), standalone process spawning (process/*), sandboxed command execution
// (command/exec*), and thread realtime sessions (thread/realtime/*).
//
// Rust sources:
//   - codex-rs/app-server-protocol/src/protocol/v2/fs.rs
//   - codex-rs/app-server-protocol/src/protocol/v2/process.rs
//   - codex-rs/app-server-protocol/src/protocol/v2/command_exec.rs
//   - codex-rs/app-server-protocol/src/protocol/v2/realtime.rs
//
// Method/notification name strings and JSON key order match the Rust
// definitions in codex-rs/app-server-protocol/src/protocol/common.rs.
//
// Serialization conventions reproduced here:
//   - rename_all = "camelCase": Go json tags spell the camelCase wire keys.
//   - #[serde(default, skip_serializing_if = "std::ops::Not::not")] on a bool:
//     modeled as a plain `bool` with `,omitempty` (omit when false).
//   - #[ts(optional = nullable)] WITHOUT skip_serializing_if on Option<T>:
//     ALWAYS emitted (null when absent) -> pointer field with NO omitempty.
//   - double_option (Option<Option<T>>) with skip_serializing_if Option::is_none:
//     modeled with DoubleOption[T] in a pointer field with ,omitempty so an
//     absent outer Option is omitted, while an explicit null vs value is
//     preserved by DoubleOption.

// ---------------------------------------------------------------------------
// fs/* — filesystem access (v2/fs.rs)
// ---------------------------------------------------------------------------

// FsReadFileParams is the request for `fs/readFile`. Rust: v2::FsReadFileParams.
type FsReadFileParams struct {
	// Path is the absolute path to read.
	Path protocol.AbsolutePath `json:"path"`
}

// FsReadFileResponse carries base64-encoded file contents for `fs/readFile`.
type FsReadFileResponse struct {
	// DataBase64 is the file contents encoded as base64.
	DataBase64 string `json:"dataBase64"`
}

// FsWriteFileParams is the request for `fs/writeFile`.
type FsWriteFileParams struct {
	// Path is the absolute path to write.
	Path protocol.AbsolutePath `json:"path"`
	// DataBase64 is the file contents encoded as base64.
	DataBase64 string `json:"dataBase64"`
}

// FsWriteFileResponse is the empty success response for `fs/writeFile`.
type FsWriteFileResponse struct{}

// FsCreateDirectoryParams is the request for `fs/createDirectory`.
type FsCreateDirectoryParams struct {
	// Path is the absolute directory path to create.
	Path protocol.AbsolutePath `json:"path"`
	// Recursive controls whether parents are created (defaults to true on the
	// server). Always emitted (null when absent), matching the reference which
	// has no skip_serializing_if.
	Recursive *bool `json:"recursive"`
}

// FsCreateDirectoryResponse is the empty success response for
// `fs/createDirectory`.
type FsCreateDirectoryResponse struct{}

// FsGetMetadataParams is the request for `fs/getMetadata`.
type FsGetMetadataParams struct {
	// Path is the absolute path to inspect.
	Path protocol.AbsolutePath `json:"path"`
}

// FsGetMetadataResponse is the metadata returned by `fs/getMetadata`.
type FsGetMetadataResponse struct {
	// IsDirectory reports whether the path resolves to a directory.
	IsDirectory bool `json:"isDirectory"`
	// IsFile reports whether the path resolves to a regular file.
	IsFile bool `json:"isFile"`
	// IsSymlink reports whether the path itself is a symbolic link.
	IsSymlink bool `json:"isSymlink"`
	// CreatedAtMs is the creation time in Unix milliseconds, or 0.
	CreatedAtMs int64 `json:"createdAtMs"`
	// ModifiedAtMs is the modification time in Unix milliseconds, or 0.
	ModifiedAtMs int64 `json:"modifiedAtMs"`
}

// FsReadDirectoryParams is the request for `fs/readDirectory`.
type FsReadDirectoryParams struct {
	// Path is the absolute directory path to read.
	Path protocol.AbsolutePath `json:"path"`
}

// FsReadDirectoryEntry is a single direct child entry from `fs/readDirectory`.
type FsReadDirectoryEntry struct {
	// FileName is the direct child entry name only (not a path).
	FileName string `json:"fileName"`
	// IsDirectory reports whether this entry resolves to a directory.
	IsDirectory bool `json:"isDirectory"`
	// IsFile reports whether this entry resolves to a regular file.
	IsFile bool `json:"isFile"`
}

// FsReadDirectoryResponse lists direct child entries for `fs/readDirectory`.
type FsReadDirectoryResponse struct {
	// Entries are the direct child entries in the requested directory.
	Entries []FsReadDirectoryEntry `json:"entries"`
}

// FsRemoveParams is the request for `fs/remove`.
type FsRemoveParams struct {
	// Path is the absolute path to remove.
	Path protocol.AbsolutePath `json:"path"`
	// Recursive controls whether directory removal recurses (defaults true on
	// the server). Always emitted (null when absent).
	Recursive *bool `json:"recursive"`
	// Force controls whether missing paths are ignored (defaults true on the
	// server). Always emitted (null when absent).
	Force *bool `json:"force"`
}

// FsRemoveResponse is the empty success response for `fs/remove`.
type FsRemoveResponse struct{}

// FsCopyParams is the request for `fs/copy`.
type FsCopyParams struct {
	// SourcePath is the absolute source path.
	SourcePath protocol.AbsolutePath `json:"sourcePath"`
	// DestinationPath is the absolute destination path.
	DestinationPath protocol.AbsolutePath `json:"destinationPath"`
	// Recursive is required for directory copies and ignored for files. Rust:
	// #[serde(default, skip_serializing_if = "Not::not")] -> omit when false.
	Recursive bool `json:"recursive,omitempty"`
}

// FsCopyResponse is the empty success response for `fs/copy`.
type FsCopyResponse struct{}

// FsWatchParams is the request for `fs/watch`.
type FsWatchParams struct {
	// WatchID is the connection-scoped watch identifier.
	WatchID string `json:"watchId"`
	// Path is the absolute file or directory path to watch.
	Path protocol.AbsolutePath `json:"path"`
}

// FsWatchResponse is the success response for `fs/watch`.
type FsWatchResponse struct {
	// Path is the canonicalized path associated with the watch.
	Path protocol.AbsolutePath `json:"path"`
}

// FsUnwatchParams is the request for `fs/unwatch`.
type FsUnwatchParams struct {
	// WatchID is the watch identifier previously provided to `fs/watch`.
	WatchID string `json:"watchId"`
}

// FsUnwatchResponse is the empty success response for `fs/unwatch`.
type FsUnwatchResponse struct{}

// FsChangedNotification is the `fs/changed` notification for watch subscribers.
type FsChangedNotification struct {
	// WatchID is the watch identifier previously provided to `fs/watch`.
	WatchID string `json:"watchId"`
	// ChangedPaths are the paths associated with this event.
	ChangedPaths []protocol.AbsolutePath `json:"changedPaths"`
}

// ---------------------------------------------------------------------------
// process/* — standalone process spawning (v2/process.rs)
// ---------------------------------------------------------------------------

// ProcessTerminalSize is the PTY size in character cells for `process/spawn`.
type ProcessTerminalSize struct {
	// Rows is the terminal height in character cells.
	Rows uint16 `json:"rows"`
	// Cols is the terminal width in character cells.
	Cols uint16 `json:"cols"`
}

// ProcessSpawnParams is the request for `process/spawn`.
type ProcessSpawnParams struct {
	// Command is the argv vector. Empty arrays are rejected by the server.
	Command []string `json:"command"`
	// ProcessHandle is the client-supplied, connection-scoped process handle.
	ProcessHandle string `json:"processHandle"`
	// Cwd is the absolute working directory for the process.
	Cwd protocol.AbsolutePath `json:"cwd"`
	// TTY enables PTY mode (implies StreamStdin and StreamStdoutStderr).
	TTY bool `json:"tty,omitempty"`
	// StreamStdin allows follow-up `process/writeStdin` requests.
	StreamStdin bool `json:"streamStdin,omitempty"`
	// StreamStdoutStderr streams output via `process/outputDelta`.
	StreamStdoutStderr bool `json:"streamStdoutStderr,omitempty"`
	// OutputBytesCap is an optional per-stream capture cap. double_option:
	// absent omits the key, null disables the cap, value sets it. Modeled as a
	// value DoubleOption with omitzero so the absent state (zero value) is
	// omitted while present null/value states are preserved on round trip.
	OutputBytesCap DoubleOption[uint64] `json:"outputBytesCap,omitzero"`
	// TimeoutMs is an optional timeout. double_option: absent omits the key,
	// null disables the timeout, value sets it.
	TimeoutMs DoubleOption[int64] `json:"timeoutMs,omitzero"`
	// Env carries optional environment overrides (null value unsets a var).
	// Always emitted (null when absent).
	Env map[string]*string `json:"env"`
	// Size is the optional initial PTY size. Always emitted (null when absent).
	Size *ProcessTerminalSize `json:"size"`
}

// ProcessSpawnResponse is the empty success response for `process/spawn`.
type ProcessSpawnResponse struct{}

// ProcessWriteStdinParams is the request for `process/writeStdin`.
type ProcessWriteStdinParams struct {
	// ProcessHandle is the connection-scoped handle from `process/spawn`.
	ProcessHandle string `json:"processHandle"`
	// DeltaBase64 is optional base64-encoded stdin bytes. Always emitted (null
	// when absent).
	DeltaBase64 *string `json:"deltaBase64"`
	// CloseStdin closes stdin after writing DeltaBase64 if present.
	CloseStdin bool `json:"closeStdin,omitempty"`
}

// ProcessWriteStdinResponse is the empty success response for
// `process/writeStdin`.
type ProcessWriteStdinResponse struct{}

// ProcessKillParams is the request for `process/kill`.
type ProcessKillParams struct {
	// ProcessHandle is the connection-scoped handle from `process/spawn`.
	ProcessHandle string `json:"processHandle"`
}

// ProcessKillResponse is the empty success response for `process/kill`.
type ProcessKillResponse struct{}

// ProcessResizePtyParams is the request for `process/resizePty`.
type ProcessResizePtyParams struct {
	// ProcessHandle is the connection-scoped handle from `process/spawn`.
	ProcessHandle string `json:"processHandle"`
	// Size is the new PTY size in character cells.
	Size ProcessTerminalSize `json:"size"`
}

// ProcessResizePtyResponse is the empty success response for
// `process/resizePty`.
type ProcessResizePtyResponse struct{}

// ProcessOutputStream labels a `process/outputDelta` stream. Rust enum with
// rename_all = "camelCase" over unit variants.
type ProcessOutputStream string

const (
	// ProcessOutputStreamStdout is the stdout stream (PTY multiplexes here).
	ProcessOutputStreamStdout ProcessOutputStream = "stdout"
	// ProcessOutputStreamStderr is the stderr stream.
	ProcessOutputStreamStderr ProcessOutputStream = "stderr"
)

// ProcessOutputDeltaNotification is the `process/outputDelta` notification.
type ProcessOutputDeltaNotification struct {
	// ProcessHandle is the connection-scoped handle from `process/spawn`.
	ProcessHandle string `json:"processHandle"`
	// Stream is the output stream this chunk belongs to.
	Stream ProcessOutputStream `json:"stream"`
	// DeltaBase64 is the base64-encoded output bytes.
	DeltaBase64 string `json:"deltaBase64"`
	// CapReached is true on the final chunk when output was truncated.
	CapReached bool `json:"capReached"`
}

// ProcessExitedNotification is the `process/exited` notification.
type ProcessExitedNotification struct {
	// ProcessHandle is the connection-scoped handle from `process/spawn`.
	ProcessHandle string `json:"processHandle"`
	// ExitCode is the process exit code.
	ExitCode int32 `json:"exitCode"`
	// Stdout is the buffered stdout capture (empty when streamed).
	Stdout string `json:"stdout"`
	// StdoutCapReached reports whether stdout reached OutputBytesCap.
	StdoutCapReached bool `json:"stdoutCapReached"`
	// Stderr is the buffered stderr capture (empty when streamed).
	Stderr string `json:"stderr"`
	// StderrCapReached reports whether stderr reached OutputBytesCap.
	StderrCapReached bool `json:"stderrCapReached"`
}

// ---------------------------------------------------------------------------
// command/exec* — sandboxed command execution (v2/command_exec.rs)
// ---------------------------------------------------------------------------

// CommandExecTerminalSize is the PTY size in character cells for `command/exec`.
type CommandExecTerminalSize struct {
	// Rows is the terminal height in character cells.
	Rows uint16 `json:"rows"`
	// Cols is the terminal width in character cells.
	Cols uint16 `json:"cols"`
}

// CommandExecParams is the request for `command/exec`.
//
// Unlike ProcessSpawnParams, the optional limit fields here are plain Option
// (always emitted as null when absent, no double_option). PermissionProfile is
// gated behind the experimental "command/exec.permissionProfile" feature but
// shares the same wire shape regardless.
type CommandExecParams struct {
	// Command is the argv vector. Empty arrays are rejected by the server.
	Command []string `json:"command"`
	// ProcessID is the optional connection-scoped process id. Always emitted
	// (null when absent).
	ProcessID *string `json:"processId"`
	// TTY enables PTY mode (implies StreamStdin and StreamStdoutStderr).
	TTY bool `json:"tty,omitempty"`
	// StreamStdin allows follow-up `command/exec/write` requests.
	StreamStdin bool `json:"streamStdin,omitempty"`
	// StreamStdoutStderr streams output via `command/exec/outputDelta`.
	StreamStdoutStderr bool `json:"streamStdoutStderr,omitempty"`
	// OutputBytesCap is the optional per-stream capture cap. Always emitted
	// (null when absent). Cannot be combined with DisableOutputCap.
	OutputBytesCap *uint64 `json:"outputBytesCap"`
	// DisableOutputCap disables capture truncation for this request.
	DisableOutputCap bool `json:"disableOutputCap,omitempty"`
	// DisableTimeout disables the timeout for this request.
	DisableTimeout bool `json:"disableTimeout,omitempty"`
	// TimeoutMs is the optional timeout. Always emitted (null when absent).
	TimeoutMs *int64 `json:"timeoutMs"`
	// Cwd is the optional working directory (defaults to server cwd). Always
	// emitted (null when absent). Rust PathBuf -> string.
	Cwd *string `json:"cwd"`
	// Env carries optional environment overrides (null value unsets a var).
	// Always emitted (null when absent).
	Env map[string]*string `json:"env"`
	// Size is the optional initial PTY size. Always emitted (null when absent).
	Size *CommandExecTerminalSize `json:"size"`
	// SandboxPolicy is the optional sandbox policy. Always emitted (null when
	// absent). Cannot be combined with PermissionProfile.
	SandboxPolicy *protocol.SandboxPolicy `json:"sandboxPolicy"`
	// PermissionProfile is the optional active permissions profile id. Always
	// emitted (null when absent). Experimental:
	// "command/exec.permissionProfile". Cannot be combined with SandboxPolicy.
	PermissionProfile *string `json:"permissionProfile"`
}

// CommandExecResponse is the final buffered result for `command/exec`.
type CommandExecResponse struct {
	// ExitCode is the process exit code.
	ExitCode int32 `json:"exitCode"`
	// Stdout is the buffered stdout capture (empty when streamed).
	Stdout string `json:"stdout"`
	// Stderr is the buffered stderr capture (empty when streamed).
	Stderr string `json:"stderr"`
}

// CommandExecWriteParams is the request for `command/exec/write`.
type CommandExecWriteParams struct {
	// ProcessID is the connection-scoped id from the original `command/exec`.
	ProcessID string `json:"processId"`
	// DeltaBase64 is optional base64-encoded stdin bytes. Always emitted (null
	// when absent).
	DeltaBase64 *string `json:"deltaBase64"`
	// CloseStdin closes stdin after writing DeltaBase64 if present.
	CloseStdin bool `json:"closeStdin,omitempty"`
}

// CommandExecWriteResponse is the empty success response for
// `command/exec/write`.
type CommandExecWriteResponse struct{}

// CommandExecTerminateParams is the request for `command/exec/terminate`.
type CommandExecTerminateParams struct {
	// ProcessID is the connection-scoped id from the original `command/exec`.
	ProcessID string `json:"processId"`
}

// CommandExecTerminateResponse is the empty success response for
// `command/exec/terminate`.
type CommandExecTerminateResponse struct{}

// CommandExecResizeParams is the request for `command/exec/resize`.
type CommandExecResizeParams struct {
	// ProcessID is the connection-scoped id from the original `command/exec`.
	ProcessID string `json:"processId"`
	// Size is the new PTY size in character cells.
	Size CommandExecTerminalSize `json:"size"`
}

// CommandExecResizeResponse is the empty success response for
// `command/exec/resize`.
type CommandExecResizeResponse struct{}

// CommandExecOutputStream labels a `command/exec/outputDelta` stream.
type CommandExecOutputStream string

const (
	// CommandExecOutputStreamStdout is the stdout stream (PTY multiplexes here).
	CommandExecOutputStreamStdout CommandExecOutputStream = "stdout"
	// CommandExecOutputStreamStderr is the stderr stream.
	CommandExecOutputStreamStderr CommandExecOutputStream = "stderr"
)

// CommandExecOutputDeltaNotification is the `command/exec/outputDelta`
// notification.
type CommandExecOutputDeltaNotification struct {
	// ProcessID is the connection-scoped id from the original `command/exec`.
	ProcessID string `json:"processId"`
	// Stream is the output stream for this chunk.
	Stream CommandExecOutputStream `json:"stream"`
	// DeltaBase64 is the base64-encoded output bytes.
	DeltaBase64 string `json:"deltaBase64"`
	// CapReached is true on the final chunk when OutputBytesCap truncated later
	// output on the stream.
	CapReached bool `json:"capReached"`
}

// ---------------------------------------------------------------------------
// thread/realtime/* — thread realtime sessions (v2/realtime.rs)
// ---------------------------------------------------------------------------
//
// The whole realtime surface is EXPERIMENTAL and gated behind per-method
// experimental feature flags (see init() registration below).

// RealtimeConversationVersionV2 is the v2 realtime conversation protocol
// version, distinct from the internal protocol RealtimeConversationVersion whose
// wire values are "V1"/"V2". The v2 API uses serde rename_all = "snake_case"
// (core codex_protocol::RealtimeConversationVersion), so the wire values are the
// lowercase "v1"/"v2".
type RealtimeConversationVersionV2 string

const (
	// RealtimeConversationVersionV2V1 is the legacy "v1" realtime version.
	RealtimeConversationVersionV2V1 RealtimeConversationVersionV2 = "v1"
	// RealtimeConversationVersionV2V2 is the default "v2" realtime version.
	RealtimeConversationVersionV2V2 RealtimeConversationVersionV2 = "v2"
)

// RealtimeVoicesList enumerates voices supported by realtime conversation.
// Rust: codex_protocol::protocol::RealtimeVoicesList, rename_all = "camelCase".
type RealtimeVoicesList struct {
	// V1 lists the v1 voice set.
	V1 []protocol.RealtimeVoice `json:"v1"`
	// V2 lists the v2 voice set.
	V2 []protocol.RealtimeVoice `json:"v2"`
	// DefaultV1 is the default v1 voice.
	DefaultV1 protocol.RealtimeVoice `json:"defaultV1"`
	// DefaultV2 is the default v2 voice.
	DefaultV2 protocol.RealtimeVoice `json:"defaultV2"`
}

// ThreadRealtimeAudioChunk is a realtime audio chunk for thread realtime. Rust:
// rename_all = "camelCase" (distinct from the core snake_case
// RealtimeAudioFrame, which this mirrors field-for-field).
type ThreadRealtimeAudioChunk struct {
	// Data is the base64-encoded PCM audio data.
	Data string `json:"data"`
	// SampleRate is the audio sample rate in Hz.
	SampleRate uint32 `json:"sampleRate"`
	// NumChannels is the number of audio channels.
	NumChannels uint16 `json:"numChannels"`
	// SamplesPerChannel is the optional per-channel sample count. Always emitted
	// (null when absent), matching the reference Option without skip.
	SamplesPerChannel *uint32 `json:"samplesPerChannel"`
	// ItemID is the optional originating item id. Always emitted (null).
	ItemID *string `json:"itemId"`
}

// ThreadRealtimeStartTransportKind discriminates the realtime transport.
type ThreadRealtimeStartTransportKind string

const (
	// ThreadRealtimeStartTransportWebsocket selects the WebSocket transport.
	ThreadRealtimeStartTransportWebsocket ThreadRealtimeStartTransportKind = "websocket"
	// ThreadRealtimeStartTransportWebrtc selects the WebRTC transport.
	ThreadRealtimeStartTransportWebrtc ThreadRealtimeStartTransportKind = "webrtc"
)

// ThreadRealtimeStartTransport is the internally-tagged transport for
// `thread/realtime/start`. Rust: serde tag = "type", rename_all = "camelCase".
//
//   - Websocket: {"type":"websocket"}
//   - Webrtc:    {"type":"webrtc","sdp":"<offer>"}
type ThreadRealtimeStartTransport struct {
	// Kind is the active transport variant.
	Kind ThreadRealtimeStartTransportKind
	// SDP is the WebRTC SDP offer; only meaningful when Kind is Webrtc.
	SDP string
}

// NewWebsocketTransport builds a WebSocket realtime transport.
func NewWebsocketTransport() ThreadRealtimeStartTransport {
	return ThreadRealtimeStartTransport{Kind: ThreadRealtimeStartTransportWebsocket}
}

// NewWebrtcTransport builds a WebRTC realtime transport carrying an SDP offer.
func NewWebrtcTransport(sdp string) ThreadRealtimeStartTransport {
	return ThreadRealtimeStartTransport{Kind: ThreadRealtimeStartTransportWebrtc, SDP: sdp}
}

// MarshalJSON encodes the internally-tagged transport.
func (t ThreadRealtimeStartTransport) MarshalJSON() ([]byte, error) {
	switch t.Kind {
	case ThreadRealtimeStartTransportWebsocket:
		return json.Marshal(map[string]string{"type": string(t.Kind)})
	case ThreadRealtimeStartTransportWebrtc:
		return json.Marshal(map[string]string{"type": string(t.Kind), "sdp": t.SDP})
	default:
		return nil, fmt.Errorf("appserverproto: unknown ThreadRealtimeStartTransport kind: %q", t.Kind)
	}
}

// UnmarshalJSON decodes the internally-tagged transport on the "type" field.
func (t *ThreadRealtimeStartTransport) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type ThreadRealtimeStartTransportKind `json:"type"`
		SDP  string                           `json:"sdp"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	switch probe.Type {
	case ThreadRealtimeStartTransportWebsocket:
		*t = ThreadRealtimeStartTransport{Kind: probe.Type}
		return nil
	case ThreadRealtimeStartTransportWebrtc:
		*t = ThreadRealtimeStartTransport{Kind: probe.Type, SDP: probe.SDP}
		return nil
	default:
		return fmt.Errorf("appserverproto: unknown ThreadRealtimeStartTransport type: %q", probe.Type)
	}
}

// ThreadRealtimeStartParams is the request for `thread/realtime/start`.
type ThreadRealtimeStartParams struct {
	// ThreadID identifies the thread to start a realtime session on.
	ThreadID string `json:"threadId"`
	// OutputModality selects text or audio output for the realtime session.
	OutputModality protocol.RealtimeOutputModality `json:"outputModality"`
	// Prompt is an optional initial prompt. double_option: absent omits the
	// key, null clears it, value sets it. Modeled as a value DoubleOption with
	// omitzero (matching Rust's default + double_option + skip_serializing_if):
	// a pointer field would collapse the absent vs explicit-null distinction
	// because encoding/json nils pointers on a JSON null instead of calling
	// UnmarshalJSON.
	Prompt DoubleOption[string] `json:"prompt,omitzero"`
	// RealtimeSessionID is the optional resumable session id. Always emitted
	// (null when absent).
	RealtimeSessionID *string `json:"realtimeSessionId"`
	// Transport is the optional transport selection. Always emitted (null when
	// absent).
	Transport *ThreadRealtimeStartTransport `json:"transport"`
	// Voice is the optional voice selection. Always emitted (null when absent).
	Voice *protocol.RealtimeVoice `json:"voice"`
}

// ThreadRealtimeStartResponse is the empty response for `thread/realtime/start`.
type ThreadRealtimeStartResponse struct{}

// ThreadRealtimeAppendAudioParams is the request for
// `thread/realtime/appendAudio`.
type ThreadRealtimeAppendAudioParams struct {
	// ThreadID identifies the realtime thread.
	ThreadID string `json:"threadId"`
	// Audio is the realtime audio chunk to append.
	Audio ThreadRealtimeAudioChunk `json:"audio"`
}

// ThreadRealtimeAppendAudioResponse is the empty response for
// `thread/realtime/appendAudio`.
type ThreadRealtimeAppendAudioResponse struct{}

// ThreadRealtimeAppendTextParams is the request for
// `thread/realtime/appendText`.
type ThreadRealtimeAppendTextParams struct {
	// ThreadID identifies the realtime thread.
	ThreadID string `json:"threadId"`
	// Text is the input text to append.
	Text string `json:"text"`
}

// ThreadRealtimeAppendTextResponse is the empty response for
// `thread/realtime/appendText`.
type ThreadRealtimeAppendTextResponse struct{}

// ThreadRealtimeStopParams is the request for `thread/realtime/stop`.
type ThreadRealtimeStopParams struct {
	// ThreadID identifies the realtime thread to stop.
	ThreadID string `json:"threadId"`
}

// ThreadRealtimeStopResponse is the empty response for `thread/realtime/stop`.
type ThreadRealtimeStopResponse struct{}

// ThreadRealtimeListVoicesParams is the request for
// `thread/realtime/listVoices`.
type ThreadRealtimeListVoicesParams struct{}

// ThreadRealtimeListVoicesResponse lists supported realtime voices.
type ThreadRealtimeListVoicesResponse struct {
	// Voices is the supported voices list.
	Voices RealtimeVoicesList `json:"voices"`
}

// ThreadRealtimeStartedNotification is the `thread/realtime/started`
// notification.
type ThreadRealtimeStartedNotification struct {
	// ThreadID identifies the realtime thread.
	ThreadID string `json:"threadId"`
	// RealtimeSessionID is the optional resumable session id. Always emitted
	// (null when absent).
	RealtimeSessionID *string `json:"realtimeSessionId"`
	// Version is the realtime conversation protocol version.
	Version RealtimeConversationVersionV2 `json:"version"`
}

// ThreadRealtimeItemAddedNotification is the `thread/realtime/itemAdded`
// notification. Item is a raw JSON value (backend-defined shape).
type ThreadRealtimeItemAddedNotification struct {
	// ThreadID identifies the realtime thread.
	ThreadID string `json:"threadId"`
	// Item is the raw non-audio realtime item emitted by the backend
	// (serde_json::Value passed through unchanged).
	Item json.RawMessage `json:"item"`
}

// ThreadRealtimeTranscriptDeltaNotification is the
// `thread/realtime/transcript/delta` notification.
type ThreadRealtimeTranscriptDeltaNotification struct {
	// ThreadID identifies the realtime thread.
	ThreadID string `json:"threadId"`
	// Role is the speaker role for the transcript.
	Role string `json:"role"`
	// Delta is the live transcript delta from the realtime event.
	Delta string `json:"delta"`
}

// ThreadRealtimeTranscriptDoneNotification is the
// `thread/realtime/transcript/done` notification.
type ThreadRealtimeTranscriptDoneNotification struct {
	// ThreadID identifies the realtime thread.
	ThreadID string `json:"threadId"`
	// Role is the speaker role for the transcript.
	Role string `json:"role"`
	// Text is the final complete text for the transcript part.
	Text string `json:"text"`
}

// ThreadRealtimeOutputAudioDeltaNotification is the
// `thread/realtime/outputAudio/delta` notification.
type ThreadRealtimeOutputAudioDeltaNotification struct {
	// ThreadID identifies the realtime thread.
	ThreadID string `json:"threadId"`
	// Audio is the streamed output audio chunk.
	Audio ThreadRealtimeAudioChunk `json:"audio"`
}

// ThreadRealtimeSdpNotification is the `thread/realtime/sdp` notification.
type ThreadRealtimeSdpNotification struct {
	// ThreadID identifies the realtime thread.
	ThreadID string `json:"threadId"`
	// SDP is the remote SDP for a WebRTC realtime session.
	SDP string `json:"sdp"`
}

// ThreadRealtimeErrorNotification is the `thread/realtime/error` notification.
type ThreadRealtimeErrorNotification struct {
	// ThreadID identifies the realtime thread.
	ThreadID string `json:"threadId"`
	// Message is the error message.
	Message string `json:"message"`
}

// ThreadRealtimeClosedNotification is the `thread/realtime/closed` notification.
type ThreadRealtimeClosedNotification struct {
	// ThreadID identifies the realtime thread.
	ThreadID string `json:"threadId"`
	// Reason is the optional close reason. Always emitted (null when absent).
	Reason *string `json:"reason"`
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func init() {
	// fs/* — all non-experimental, concurrent filesystem requests.
	Register(MethodSpec{
		Method:    "fs/readFile",
		NewParams: func() any { return new(FsReadFileParams) },
		NewResult: func() any { return new(FsReadFileResponse) },
	})
	Register(MethodSpec{
		Method:    "fs/writeFile",
		NewParams: func() any { return new(FsWriteFileParams) },
		NewResult: func() any { return new(FsWriteFileResponse) },
	})
	Register(MethodSpec{
		Method:    "fs/createDirectory",
		NewParams: func() any { return new(FsCreateDirectoryParams) },
		NewResult: func() any { return new(FsCreateDirectoryResponse) },
	})
	Register(MethodSpec{
		Method:    "fs/getMetadata",
		NewParams: func() any { return new(FsGetMetadataParams) },
		NewResult: func() any { return new(FsGetMetadataResponse) },
	})
	Register(MethodSpec{
		Method:    "fs/readDirectory",
		NewParams: func() any { return new(FsReadDirectoryParams) },
		NewResult: func() any { return new(FsReadDirectoryResponse) },
	})
	Register(MethodSpec{
		Method:    "fs/remove",
		NewParams: func() any { return new(FsRemoveParams) },
		NewResult: func() any { return new(FsRemoveResponse) },
	})
	Register(MethodSpec{
		Method:    "fs/copy",
		NewParams: func() any { return new(FsCopyParams) },
		NewResult: func() any { return new(FsCopyResponse) },
	})
	Register(MethodSpec{
		Method:    "fs/watch",
		NewParams: func() any { return new(FsWatchParams) },
		NewResult: func() any { return new(FsWatchResponse) },
	})
	Register(MethodSpec{
		Method:    "fs/unwatch",
		NewParams: func() any { return new(FsUnwatchParams) },
		NewResult: func() any { return new(FsUnwatchResponse) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "fs/changed",
		NewParams: func() any { return new(FsChangedNotification) },
	})

	// command/exec* — sandboxed execution. command/exec itself is not gated,
	// but its permissionProfile field is experimental within the params.
	Register(MethodSpec{
		Method:    "command/exec",
		NewParams: func() any { return new(CommandExecParams) },
		NewResult: func() any { return new(CommandExecResponse) },
	})
	Register(MethodSpec{
		Method:    "command/exec/write",
		NewParams: func() any { return new(CommandExecWriteParams) },
		NewResult: func() any { return new(CommandExecWriteResponse) },
	})
	Register(MethodSpec{
		Method:    "command/exec/terminate",
		NewParams: func() any { return new(CommandExecTerminateParams) },
		NewResult: func() any { return new(CommandExecTerminateResponse) },
	})
	Register(MethodSpec{
		Method:    "command/exec/resize",
		NewParams: func() any { return new(CommandExecResizeParams) },
		NewResult: func() any { return new(CommandExecResizeResponse) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "command/exec/outputDelta",
		NewParams: func() any { return new(CommandExecOutputDeltaNotification) },
	})

	// process/* — standalone process spawning. All methods are experimental.
	Register(MethodSpec{
		Method:       "process/spawn",
		NewParams:    func() any { return new(ProcessSpawnParams) },
		NewResult:    func() any { return new(ProcessSpawnResponse) },
		Experimental: true,
	})
	Register(MethodSpec{
		Method:       "process/writeStdin",
		NewParams:    func() any { return new(ProcessWriteStdinParams) },
		NewResult:    func() any { return new(ProcessWriteStdinResponse) },
		Experimental: true,
	})
	Register(MethodSpec{
		Method:       "process/kill",
		NewParams:    func() any { return new(ProcessKillParams) },
		NewResult:    func() any { return new(ProcessKillResponse) },
		Experimental: true,
	})
	Register(MethodSpec{
		Method:       "process/resizePty",
		NewParams:    func() any { return new(ProcessResizePtyParams) },
		NewResult:    func() any { return new(ProcessResizePtyResponse) },
		Experimental: true,
	})
	RegisterNotification(NotificationSpec{
		Method:       "process/outputDelta",
		NewParams:    func() any { return new(ProcessOutputDeltaNotification) },
		Experimental: true,
	})
	RegisterNotification(NotificationSpec{
		Method:       "process/exited",
		NewParams:    func() any { return new(ProcessExitedNotification) },
		Experimental: true,
	})

	// thread/realtime/* — experimental realtime requests.
	Register(MethodSpec{
		Method:       "thread/realtime/start",
		NewParams:    func() any { return new(ThreadRealtimeStartParams) },
		NewResult:    func() any { return new(ThreadRealtimeStartResponse) },
		Experimental: true,
	})
	Register(MethodSpec{
		Method:       "thread/realtime/appendAudio",
		NewParams:    func() any { return new(ThreadRealtimeAppendAudioParams) },
		NewResult:    func() any { return new(ThreadRealtimeAppendAudioResponse) },
		Experimental: true,
	})
	Register(MethodSpec{
		Method:       "thread/realtime/appendText",
		NewParams:    func() any { return new(ThreadRealtimeAppendTextParams) },
		NewResult:    func() any { return new(ThreadRealtimeAppendTextResponse) },
		Experimental: true,
	})
	Register(MethodSpec{
		Method:       "thread/realtime/stop",
		NewParams:    func() any { return new(ThreadRealtimeStopParams) },
		NewResult:    func() any { return new(ThreadRealtimeStopResponse) },
		Experimental: true,
	})
	Register(MethodSpec{
		Method:       "thread/realtime/listVoices",
		NewParams:    func() any { return new(ThreadRealtimeListVoicesParams) },
		NewResult:    func() any { return new(ThreadRealtimeListVoicesResponse) },
		Experimental: true,
	})

	// thread/realtime/* — experimental realtime notifications.
	RegisterNotification(NotificationSpec{
		Method:       "thread/realtime/started",
		NewParams:    func() any { return new(ThreadRealtimeStartedNotification) },
		Experimental: true,
	})
	RegisterNotification(NotificationSpec{
		Method:       "thread/realtime/itemAdded",
		NewParams:    func() any { return new(ThreadRealtimeItemAddedNotification) },
		Experimental: true,
	})
	RegisterNotification(NotificationSpec{
		Method:       "thread/realtime/transcript/delta",
		NewParams:    func() any { return new(ThreadRealtimeTranscriptDeltaNotification) },
		Experimental: true,
	})
	RegisterNotification(NotificationSpec{
		Method:       "thread/realtime/transcript/done",
		NewParams:    func() any { return new(ThreadRealtimeTranscriptDoneNotification) },
		Experimental: true,
	})
	RegisterNotification(NotificationSpec{
		Method:       "thread/realtime/outputAudio/delta",
		NewParams:    func() any { return new(ThreadRealtimeOutputAudioDeltaNotification) },
		Experimental: true,
	})
	RegisterNotification(NotificationSpec{
		Method:       "thread/realtime/sdp",
		NewParams:    func() any { return new(ThreadRealtimeSdpNotification) },
		Experimental: true,
	})
	RegisterNotification(NotificationSpec{
		Method:       "thread/realtime/error",
		NewParams:    func() any { return new(ThreadRealtimeErrorNotification) },
		Experimental: true,
	})
	RegisterNotification(NotificationSpec{
		Method:       "thread/realtime/closed",
		NewParams:    func() any { return new(ThreadRealtimeClosedNotification) },
		Experimental: true,
	})
}
