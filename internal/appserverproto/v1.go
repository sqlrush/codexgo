package appserverproto

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// AuthMode is the authentication mode for OpenAI-backed providers. Defined by
// the app-server protocol (not codex-protocol).
//
// Rust: common::AuthMode, serde rename_all = "lowercase" with two explicit
// renames (chatgptAuthTokens, agentIdentity).
type AuthMode string

const (
	// AuthModeApiKey is an OpenAI API key supplied by the caller.
	AuthModeApiKey AuthMode = "apikey"
	// AuthModeChatgpt is ChatGPT OAuth managed by Codex.
	AuthModeChatgpt AuthMode = "chatgpt"
	// AuthModeChatgptAuthTokens is host-app-supplied in-memory ChatGPT tokens.
	AuthModeChatgptAuthTokens AuthMode = "chatgptAuthTokens"
	// AuthModeAgentIdentity is programmatic auth backed by an Agent Identity.
	AuthModeAgentIdentity AuthMode = "agentIdentity"
)

// GitSha is a git commit SHA. Rust: a newtype `GitSha(pub String)` that
// (de)serializes as a bare string.
type GitSha string

// String returns the SHA string.
func (g GitSha) String() string { return string(g) }

// InitializeParams is the v1 initialize handshake request payload.
//
// Rust: v1::InitializeParams, serde rename_all = "camelCase". `capabilities` is
// omitted when absent (skip_serializing_if).
type InitializeParams struct {
	ClientInfo   ClientInfo              `json:"clientInfo"`
	Capabilities *InitializeCapabilities `json:"capabilities,omitempty"`
}

// ClientInfo identifies the connecting client. Rust: v1::ClientInfo, camelCase.
// `title` has no skip_serializing_if, so it is always emitted (null when absent).
type ClientInfo struct {
	Name    string  `json:"name"`
	Title   *string `json:"title"`
	Version string  `json:"version"`
}

// InitializeCapabilities are client-declared capabilities negotiated during
// initialize. Rust: v1::InitializeCapabilities, camelCase. The two bool fields
// are `#[serde(default)]` (default false, always emitted). The opt-out list is
// always emitted (null when absent).
type InitializeCapabilities struct {
	ExperimentalApi           bool      `json:"experimentalApi"`
	RequestAttestation        bool      `json:"requestAttestation"`
	OptOutNotificationMethods *[]string `json:"optOutNotificationMethods"`
}

// InitializeResponse is the v1 initialize handshake response.
//
// Rust: v1::InitializeResponse, camelCase. `codexHome` is an absolute path.
type InitializeResponse struct {
	UserAgent      string                `json:"userAgent"`
	CodexHome      protocol.AbsolutePath `json:"codexHome"`
	PlatformFamily string                `json:"platformFamily"`
	PlatformOs     string                `json:"platformOs"`
}

// GetConversationSummaryParams is the untagged params for the deprecated
// getConversationSummary request. Exactly one of the two shapes is present.
//
// Rust: v1::GetConversationSummaryParams, serde untagged with two variants:
//   - { "rolloutPath": <path> }
//   - { "conversationId": <ThreadId> }
type GetConversationSummaryParams struct {
	// RolloutPath selects the RolloutPath variant when non-nil.
	RolloutPath *string
	// ConversationID selects the ThreadId variant when non-nil.
	ConversationID *protocol.ThreadID
}

type getConversationSummaryRolloutPath struct {
	RolloutPath string `json:"rolloutPath"`
}

type getConversationSummaryThreadID struct {
	ConversationID protocol.ThreadID `json:"conversationId"`
}

// MarshalJSON emits the active untagged variant.
func (p GetConversationSummaryParams) MarshalJSON() ([]byte, error) {
	switch {
	case p.RolloutPath != nil:
		return json.Marshal(getConversationSummaryRolloutPath{RolloutPath: *p.RolloutPath})
	case p.ConversationID != nil:
		return json.Marshal(getConversationSummaryThreadID{ConversationID: *p.ConversationID})
	default:
		return nil, fmt.Errorf("appserverproto: GetConversationSummaryParams has no variant set")
	}
}

// UnmarshalJSON decodes whichever untagged variant is present, preferring
// rolloutPath (serde tries variants in declaration order).
func (p *GetConversationSummaryParams) UnmarshalJSON(data []byte) error {
	var rp getConversationSummaryRolloutPath
	if err := json.Unmarshal(data, &rp); err == nil && rp.RolloutPath != "" {
		path := rp.RolloutPath
		*p = GetConversationSummaryParams{RolloutPath: &path}
		return nil
	}
	var tid getConversationSummaryThreadID
	if err := json.Unmarshal(data, &tid); err == nil {
		id := tid.ConversationID
		*p = GetConversationSummaryParams{ConversationID: &id}
		return nil
	}
	return fmt.Errorf("appserverproto: GetConversationSummaryParams matched no variant: %s", string(data))
}

// GetConversationSummaryResponse wraps a ConversationSummary. Rust: camelCase.
type GetConversationSummaryResponse struct {
	Summary ConversationSummary `json:"summary"`
}

// ConversationSummary describes a stored conversation. Rust: camelCase.
//
// The `source` field carries a codex-protocol SessionSource (a complex
// externally-tagged enum that is not part of this package's scope); to stay
// wire-faithful without redefining it we pass it through as raw JSON.
type ConversationSummary struct {
	ConversationID protocol.ThreadID    `json:"conversationId"`
	Path           string               `json:"path"`
	Preview        string               `json:"preview"`
	Timestamp      *string              `json:"timestamp"`
	UpdatedAt      *string              `json:"updatedAt"`
	ModelProvider  string               `json:"modelProvider"`
	Cwd            string               `json:"cwd"`
	CliVersion     string               `json:"cliVersion"`
	Source         json.RawMessage      `json:"source"`
	GitInfo        *ConversationGitInfo `json:"gitInfo"`
}

// ConversationGitInfo carries git metadata. Rust: rename_all = "snake_case".
// All three fields are always emitted (null when absent).
type ConversationGitInfo struct {
	Sha       *string `json:"sha"`
	Branch    *string `json:"branch"`
	OriginURL *string `json:"origin_url"`
}

// LoginApiKeyParams carries an API key for login. Rust: camelCase.
type LoginApiKeyParams struct {
	ApiKey string `json:"apiKey"`
}

// GitDiffToRemoteResponse is the response to gitDiffToRemote. Rust: camelCase.
type GitDiffToRemoteResponse struct {
	Sha  GitSha `json:"sha"`
	Diff string `json:"diff"`
}

// GitDiffToRemoteParams is the gitDiffToRemote request. Rust: camelCase.
type GitDiffToRemoteParams struct {
	Cwd string `json:"cwd"`
}

// ApplyPatchApprovalParams requests approval of a patch (legacy turn flow).
// Rust: camelCase. `fileChanges` maps path -> FileChange (reusing the internal
// protocol FileChange type). The optional fields are always emitted (null).
type ApplyPatchApprovalParams struct {
	ConversationID protocol.ThreadID              `json:"conversationId"`
	CallID         string                         `json:"callId"`
	FileChanges    map[string]protocol.FileChange `json:"fileChanges"`
	Reason         *string                        `json:"reason"`
	GrantRoot      *string                        `json:"grantRoot"`
}

// ApplyPatchApprovalResponse carries the review decision. Rust: camelCase.
type ApplyPatchApprovalResponse struct {
	Decision protocol.ReviewDecision `json:"decision"`
}

// ExecCommandApprovalParams requests approval of a command (legacy turn flow).
// Rust: camelCase. Optional fields are always emitted (null).
type ExecCommandApprovalParams struct {
	ConversationID protocol.ThreadID        `json:"conversationId"`
	CallID         string                   `json:"callId"`
	ApprovalID     *string                  `json:"approvalId"`
	Command        []string                 `json:"command"`
	Cwd            string                   `json:"cwd"`
	Reason         *string                  `json:"reason"`
	ParsedCmd      []protocol.ParsedCommand `json:"parsedCmd"`
}

// ExecCommandApprovalResponse carries the review decision. The Rust struct has
// no rename_all, so the field is emitted as "decision" (already lowercase).
type ExecCommandApprovalResponse struct {
	Decision protocol.ReviewDecision `json:"decision"`
}

// GetAuthStatusParams is the deprecated getAuthStatus request. Rust: camelCase.
// Optional fields are always emitted (null).
type GetAuthStatusParams struct {
	IncludeToken *bool `json:"includeToken"`
	RefreshToken *bool `json:"refreshToken"`
}

// GetAuthStatusResponse is the deprecated getAuthStatus response. Rust: camelCase.
type GetAuthStatusResponse struct {
	AuthMethod         *AuthMode `json:"authMethod"`
	AuthToken          *string   `json:"authToken"`
	RequiresOpenaiAuth *bool     `json:"requiresOpenaiAuth"`
}

// ExecOneOffCommandParams runs a standalone command under the sandbox. Rust:
// camelCase. Optional fields are always emitted (null).
type ExecOneOffCommandParams struct {
	Command       []string                `json:"command"`
	TimeoutMs     *uint64                 `json:"timeoutMs"`
	Cwd           *string                 `json:"cwd"`
	SandboxPolicy *protocol.SandboxPolicy `json:"sandboxPolicy"`
}

// UserSavedConfig mirrors the persisted user config surface. Rust: camelCase.
// All fields are Option and always emitted (null when absent).
//
// `forcedChatgptWorkspaceId` carries the v2 ForcedChatgptWorkspaceIds untagged
// shape (string or []string); it is owned by the v2 config area, so it is
// passed through here as raw JSON to avoid a cross-area type collision.
type UserSavedConfig struct {
	ApprovalPolicy           *protocol.AskForApproval    `json:"approvalPolicy"`
	SandboxMode              *protocol.SandboxMode       `json:"sandboxMode"`
	SandboxSettings          *SandboxSettings            `json:"sandboxSettings"`
	ForcedChatgptWorkspaceID json.RawMessage             `json:"forcedChatgptWorkspaceId"`
	ForcedLoginMethod        *protocol.ForcedLoginMethod `json:"forcedLoginMethod"`
	Model                    *string                     `json:"model"`
	ModelReasoningEffort     *protocol.ReasoningEffort   `json:"modelReasoningEffort"`
	ModelReasoningSummary    *protocol.ReasoningSummary  `json:"modelReasoningSummary"`
	ModelVerbosity           *protocol.Verbosity         `json:"modelVerbosity"`
	Tools                    *Tools                      `json:"tools"`
}

// Tools toggles optional tool surfaces. Rust: camelCase, single Option field
// always emitted (null when absent).
type Tools struct {
	WebSearch *bool `json:"webSearch"`
}

// SandboxSettings mirrors the persisted sandbox config. Rust: camelCase.
// `writableRoots` is `#[serde(default)]` with no skip_serializing_if, so it is
// ALWAYS serialized as an array (empty -> []). The remaining fields are Option
// and always emitted (null when absent).
type SandboxSettings struct {
	WritableRoots       []protocol.AbsolutePath `json:"writableRoots"`
	NetworkAccess       *bool                   `json:"networkAccess"`
	ExcludeTmpdirEnvVar *bool                   `json:"excludeTmpdirEnvVar"`
	ExcludeSlashTmp     *bool                   `json:"excludeSlashTmp"`
}

// MarshalJSON ensures writableRoots always emits a JSON array (never null),
// matching Rust's always-serialized Vec. A nil Go slice would otherwise encode
// as null, diverging from the reference.
func (s SandboxSettings) MarshalJSON() ([]byte, error) {
	type alias SandboxSettings
	out := alias(s)
	if out.WritableRoots == nil {
		out.WritableRoots = []protocol.AbsolutePath{}
	}
	return json.Marshal(out)
}

// InterruptConversationResponse carries the abort reason for a legacy interrupt.
// Rust: camelCase. The abort reason reuses the internal protocol type.
type InterruptConversationResponse struct {
	AbortReason protocol.TurnAbortReason `json:"abortReason"`
}
