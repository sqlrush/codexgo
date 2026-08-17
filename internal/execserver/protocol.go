package execserver

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// JSON-RPC method names used by the executor process and filesystem services.
//
// Rust: the `*_METHOD` constants in protocol.rs.
const (
	InitializeMethod           = "initialize"
	InitializedMethod          = "initialized"
	ExecMethod                 = "process/start"
	ExecReadMethod             = "process/read"
	ExecWriteMethod            = "process/write"
	ExecTerminateMethod        = "process/terminate"
	ExecOutputDeltaMethod      = "process/output"
	ExecExitedMethod           = "process/exited"
	ExecClosedMethod           = "process/closed"
	FsReadFileMethod           = "fs/readFile"
	FsWriteFileMethod          = "fs/writeFile"
	FsCreateDirectoryMethod    = "fs/createDirectory"
	FsGetMetadataMethod        = "fs/getMetadata"
	FsReadDirectoryMethod      = "fs/readDirectory"
	FsRemoveMethod             = "fs/remove"
	FsCopyMethod               = "fs/copy"
	HTTPRequestMethod          = "http/request"
	HTTPRequestBodyDeltaMethod = "http/request/bodyDelta"
)

// ByteChunk is a base64-encoded byte buffer.
//
// Rust: `#[serde(transparent)] pub struct ByteChunk(#[serde(with = "base64_bytes")] pub Vec<u8>)`.
// On the wire it is a bare base64 (standard alphabet, with padding) JSON string.
type ByteChunk []byte

// MarshalJSON encodes the bytes as a standard-alphabet base64 JSON string.
func (b ByteChunk) MarshalJSON() ([]byte, error) {
	return json.Marshal(base64.StdEncoding.EncodeToString(b))
}

// UnmarshalJSON decodes a standard-alphabet base64 JSON string into bytes.
func (b *ByteChunk) UnmarshalJSON(data []byte) error {
	var encoded string
	if err := json.Unmarshal(data, &encoded); err != nil {
		return fmt.Errorf("execserver: ByteChunk must be a base64 string: %w", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("execserver: ByteChunk base64 decode: %w", err)
	}
	*b = decoded
	return nil
}

// ExecOutputStream identifies which stream an output chunk came from.
//
// Rust: `#[serde(rename_all = "camelCase")] enum ExecOutputStream { Stdout, Stderr, Pty }`.
type ExecOutputStream string

const (
	ExecOutputStreamStdout ExecOutputStream = "stdout"
	ExecOutputStreamStderr ExecOutputStream = "stderr"
	ExecOutputStreamPty    ExecOutputStream = "pty"
)

// WriteStatus reports the outcome of a process/write call.
//
// Rust: `#[serde(rename_all = "camelCase")] enum WriteStatus { Accepted, UnknownProcess, StdinClosed, Starting }`.
type WriteStatus string

const (
	WriteStatusAccepted       WriteStatus = "accepted"
	WriteStatusUnknownProcess WriteStatus = "unknownProcess"
	WriteStatusStdinClosed    WriteStatus = "stdinClosed"
	WriteStatusStarting       WriteStatus = "starting"
)

// InitializeParams is the payload for the initialize request.
//
// Rust: `InitializeParams` with camelCase fields; resumeSessionId is omitted
// when absent (serde `#[serde(default)]`, Option<String>).
type InitializeParams struct {
	ClientName      string  `json:"clientName"`
	ResumeSessionID *string `json:"resumeSessionId"`
}

// InitializeResponse is the response to the initialize request.
type InitializeResponse struct {
	SessionID string `json:"sessionId"`
}

// ExecParams is the payload for the process/start request.
//
// Rust: `ExecParams` with camelCase fields. envPolicy and pipeStdin default to
// their zero values; arg0 is an Option<String> that is always present (null when
// absent).
type ExecParams struct {
	// ProcessID is the client-chosen logical process handle.
	ProcessID ProcessId `json:"processId"`
	// Argv is the command and its arguments; argv[0] is the program.
	Argv []string `json:"argv"`
	// Cwd is the working directory for the child.
	Cwd string `json:"cwd"`
	// EnvPolicy, when set, filters the inherited environment before Env is
	// overlaid on top.
	EnvPolicy *ExecEnvPolicy `json:"envPolicy"`
	// Env is the explicit environment overlay applied last.
	Env map[string]string `json:"env"`
	// Tty requests a pseudo-terminal for the child.
	Tty bool `json:"tty"`
	// PipeStdin keeps non-tty stdin writable through process/write.
	PipeStdin bool `json:"pipeStdin"`
	// Arg0 overrides argv[0] the child observes, when non-nil.
	Arg0 *string `json:"arg0"`
}

// ExecEnvPolicy mirrors the executor-side ShellEnvironmentPolicy.
//
// Rust: `ExecEnvPolicy` with camelCase fields.
type ExecEnvPolicy struct {
	Inherit               protocol.ShellEnvironmentPolicyInherit `json:"inherit"`
	IgnoreDefaultExcludes bool                                   `json:"ignoreDefaultExcludes"`
	Exclude               []string                               `json:"exclude"`
	Set                   map[string]string                      `json:"set"`
	IncludeOnly           []string                               `json:"includeOnly"`
}

// ExecResponse is the response to the process/start request.
type ExecResponse struct {
	ProcessID ProcessId `json:"processId"`
}

// ReadParams is the payload for the process/read request.
//
// Rust: `ReadParams` with camelCase fields; afterSeq/maxBytes/waitMs are
// Option fields that are always present (null when absent).
type ReadParams struct {
	ProcessID ProcessId `json:"processId"`
	AfterSeq  *uint64   `json:"afterSeq"`
	MaxBytes  *int      `json:"maxBytes"`
	WaitMs    *uint64   `json:"waitMs"`
}

// ProcessOutputChunk is a single retained output frame.
type ProcessOutputChunk struct {
	Seq    uint64           `json:"seq"`
	Stream ExecOutputStream `json:"stream"`
	Chunk  ByteChunk        `json:"chunk"`
}

// ReadResponse is the response to the process/read request.
type ReadResponse struct {
	Chunks   []ProcessOutputChunk `json:"chunks"`
	NextSeq  uint64               `json:"nextSeq"`
	Exited   bool                 `json:"exited"`
	ExitCode *int                 `json:"exitCode"`
	Closed   bool                 `json:"closed"`
	Failure  *string              `json:"failure"`
}

// WriteParams is the payload for the process/write request.
type WriteParams struct {
	ProcessID ProcessId `json:"processId"`
	Chunk     ByteChunk `json:"chunk"`
}

// WriteResponse is the response to the process/write request.
type WriteResponse struct {
	Status WriteStatus `json:"status"`
}

// TerminateParams is the payload for the process/terminate request.
type TerminateParams struct {
	ProcessID ProcessId `json:"processId"`
}

// TerminateResponse is the response to the process/terminate request.
type TerminateResponse struct {
	Running bool `json:"running"`
}

// ExecOutputDeltaNotification is the process/output notification.
type ExecOutputDeltaNotification struct {
	ProcessID ProcessId        `json:"processId"`
	Seq       uint64           `json:"seq"`
	Stream    ExecOutputStream `json:"stream"`
	Chunk     ByteChunk        `json:"chunk"`
}

// ExecExitedNotification is the process/exited notification.
type ExecExitedNotification struct {
	ProcessID ProcessId `json:"processId"`
	Seq       uint64    `json:"seq"`
	ExitCode  int       `json:"exitCode"`
}

// ExecClosedNotification is the process/closed notification.
type ExecClosedNotification struct {
	ProcessID ProcessId `json:"processId"`
	Seq       uint64    `json:"seq"`
}
