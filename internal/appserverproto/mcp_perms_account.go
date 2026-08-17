package appserverproto

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// This file ports the v2 app-server protocol surfaces for MCP servers
// (mcp.rs), permission profiles (permissions.rs), and accounts (account.rs),
// plus review/start (review.rs). It defines the wire types and registers the
// client requests, server requests, and notifications via init().
//
// Field serialization mirrors the Rust source exactly:
//   - A field with NO serde skip_serializing_if is always emitted (null when an
//     Option is None) => a Go pointer WITHOUT omitempty.
//   - A field with #[serde(default, skip_serializing_if = "Option::is_none")]
//     is omitted when None => a Go pointer WITH omitempty.
//   - The "_meta" key comes from #[serde(rename = "_meta")].
//
// Cross-area types owned by sibling agents (notably v2 Turn) are passed through
// as json.RawMessage to stay wire-faithful without redefining them, matching the
// established pattern in v1.go.

// ---------------------------------------------------------------------------
// MCP: enums
// ---------------------------------------------------------------------------

// McpAuthStatus is the v2 API mirror of codex-protocol McpAuthStatus. The v2
// wrapper renames to camelCase (the core enum is snake_case), so OAuth becomes
// "oAuth".
//
// Rust: v2::McpAuthStatus (via v2_enum_from_core!, rename_all = "camelCase").
type McpAuthStatus string

const (
	// McpAuthStatusUnsupported means the server does not support auth.
	McpAuthStatusUnsupported McpAuthStatus = "unsupported"
	// McpAuthStatusNotLoggedIn means auth is supported but not established.
	McpAuthStatusNotLoggedIn McpAuthStatus = "notLoggedIn"
	// McpAuthStatusBearerToken means the server uses a bearer token.
	McpAuthStatusBearerToken McpAuthStatus = "bearerToken"
	// McpAuthStatusOAuth means the server uses OAuth.
	McpAuthStatusOAuth McpAuthStatus = "oAuth"
)

// McpServerStatusDetail controls how much MCP inventory data to fetch.
//
// Rust: v2::McpServerStatusDetail, serde rename_all = "camelCase".
type McpServerStatusDetail string

const (
	// McpServerStatusDetailFull fetches all inventory data.
	McpServerStatusDetailFull McpServerStatusDetail = "full"
	// McpServerStatusDetailToolsAndAuthOnly fetches only tools and auth.
	McpServerStatusDetailToolsAndAuthOnly McpServerStatusDetail = "toolsAndAuthOnly"
)

// McpServerStartupState is the lifecycle state of an MCP server.
//
// Rust: v2::McpServerStartupState, serde rename_all = "camelCase".
type McpServerStartupState string

const (
	// McpServerStartupStateStarting means the server is starting up.
	McpServerStartupStateStarting McpServerStartupState = "starting"
	// McpServerStartupStateReady means the server is ready.
	McpServerStartupStateReady McpServerStartupState = "ready"
	// McpServerStartupStateFailed means the server failed to start.
	McpServerStartupStateFailed McpServerStartupState = "failed"
	// McpServerStartupStateCancelled means startup was cancelled.
	McpServerStartupStateCancelled McpServerStartupState = "cancelled"
)

// McpServerElicitationAction is the user's response to an elicitation request.
//
// Rust: v2::McpServerElicitationAction, serde rename_all = "camelCase".
type McpServerElicitationAction string

const (
	// McpServerElicitationActionAccept accepts the elicitation.
	McpServerElicitationActionAccept McpServerElicitationAction = "accept"
	// McpServerElicitationActionDecline declines the elicitation.
	McpServerElicitationActionDecline McpServerElicitationAction = "decline"
	// McpServerElicitationActionCancel cancels the elicitation.
	McpServerElicitationActionCancel McpServerElicitationAction = "cancel"
)

// ---------------------------------------------------------------------------
// MCP: mcpServerStatus/list
// ---------------------------------------------------------------------------

// ListMcpServerStatusParams requests the MCP server inventory. None of the
// optional fields carry skip_serializing_if, so all keys are always emitted
// (null when absent).
//
// Rust: v2::ListMcpServerStatusParams, camelCase.
type ListMcpServerStatusParams struct {
	Cursor   *string                `json:"cursor"`
	Limit    *uint32                `json:"limit"`
	Detail   *McpServerStatusDetail `json:"detail"`
	ThreadID *string                `json:"threadId"`
}

// McpServerStatus describes a single MCP server's inventory and auth state.
// `serverInfo` carries no skip_serializing_if (always emitted, null when none);
// `tools` is a map keyed by tool name. The MCP value types are reused from the
// internal protocol package.
//
// Rust: v2::McpServerStatus, camelCase.
type McpServerStatus struct {
	Name              string                      `json:"name"`
	ServerInfo        *protocol.McpServerInfo     `json:"serverInfo"`
	Tools             map[string]protocol.Tool    `json:"tools"`
	Resources         []protocol.Resource         `json:"resources"`
	ResourceTemplates []protocol.ResourceTemplate `json:"resourceTemplates"`
	AuthStatus        McpAuthStatus               `json:"authStatus"`
}

// MarshalJSON ensures the always-emitted collection fields render as JSON arrays
// and an object (never null), matching Rust's always-serialized Vec/HashMap.
func (m McpServerStatus) MarshalJSON() ([]byte, error) {
	type alias McpServerStatus
	out := alias(m)
	if out.Tools == nil {
		out.Tools = map[string]protocol.Tool{}
	}
	if out.Resources == nil {
		out.Resources = []protocol.Resource{}
	}
	if out.ResourceTemplates == nil {
		out.ResourceTemplates = []protocol.ResourceTemplate{}
	}
	return json.Marshal(out)
}

// ListMcpServerStatusResponse pages over MCP server statuses. `nextCursor` is
// always emitted (null when there are no more items).
//
// Rust: v2::ListMcpServerStatusResponse, camelCase.
type ListMcpServerStatusResponse struct {
	Data       []McpServerStatus `json:"data"`
	NextCursor *string           `json:"nextCursor"`
}

// MarshalJSON ensures `data` always emits a JSON array (never null).
func (r ListMcpServerStatusResponse) MarshalJSON() ([]byte, error) {
	type alias ListMcpServerStatusResponse
	out := alias(r)
	if out.Data == nil {
		out.Data = []McpServerStatus{}
	}
	return json.Marshal(out)
}

// ---------------------------------------------------------------------------
// MCP: mcpServer/resource/read
// ---------------------------------------------------------------------------

// McpResourceReadParams reads a resource from an MCP server. `threadId` carries
// no skip_serializing_if, so it is always emitted (null when absent).
//
// Rust: v2::McpResourceReadParams, camelCase.
type McpResourceReadParams struct {
	ThreadID *string `json:"threadId"`
	Server   string  `json:"server"`
	URI      string  `json:"uri"`
}

// McpResourceReadResponse carries the read resource contents.
//
// Rust: v2::McpResourceReadResponse, camelCase. ResourceContent is reused from
// the internal protocol package.
type McpResourceReadResponse struct {
	Contents []protocol.ResourceContent `json:"contents"`
}

// MarshalJSON ensures `contents` always emits a JSON array (never null).
func (r McpResourceReadResponse) MarshalJSON() ([]byte, error) {
	type alias McpResourceReadResponse
	out := alias(r)
	if out.Contents == nil {
		out.Contents = []protocol.ResourceContent{}
	}
	return json.Marshal(out)
}

// ---------------------------------------------------------------------------
// MCP: mcpServer/tool/call
// ---------------------------------------------------------------------------

// McpServerToolCallParams invokes a tool on an MCP server. `arguments` and
// `_meta` use skip_serializing_if (omitted when absent).
//
// Rust: v2::McpServerToolCallParams, camelCase, meta renamed to "_meta".
type McpServerToolCallParams struct {
	ThreadID  string           `json:"threadId"`
	Server    string           `json:"server"`
	Tool      string           `json:"tool"`
	Arguments *json.RawMessage `json:"arguments,omitempty"`
	Meta      *json.RawMessage `json:"_meta,omitempty"`
}

// McpServerToolCallResponse is the result of an MCP tool call. `structuredContent`,
// `isError`, and `_meta` use skip_serializing_if; `content` is always emitted.
//
// Rust: v2::McpServerToolCallResponse, camelCase, meta renamed to "_meta".
type McpServerToolCallResponse struct {
	Content           []json.RawMessage `json:"content"`
	StructuredContent *json.RawMessage  `json:"structuredContent,omitempty"`
	IsError           *bool             `json:"isError,omitempty"`
	Meta              *json.RawMessage  `json:"_meta,omitempty"`
}

// MarshalJSON ensures `content` always emits a JSON array (never null).
func (r McpServerToolCallResponse) MarshalJSON() ([]byte, error) {
	type alias McpServerToolCallResponse
	out := alias(r)
	if out.Content == nil {
		out.Content = []json.RawMessage{}
	}
	return json.Marshal(out)
}

// ---------------------------------------------------------------------------
// MCP: config/mcpServer/reload
// ---------------------------------------------------------------------------

// McpServerRefreshResponse is the empty response to a registry reload.
//
// Rust: v2::McpServerRefreshResponse (empty struct -> {}).
type McpServerRefreshResponse struct{}

// ---------------------------------------------------------------------------
// MCP: mcpServer/oauth/login
// ---------------------------------------------------------------------------

// McpServerOauthLoginParams begins an OAuth login for a named MCP server.
// `scopes` and `timeoutSecs` use skip_serializing_if (omitted when absent).
//
// Rust: v2::McpServerOauthLoginParams, camelCase.
type McpServerOauthLoginParams struct {
	Name        string    `json:"name"`
	Scopes      *[]string `json:"scopes,omitempty"`
	TimeoutSecs *int64    `json:"timeoutSecs,omitempty"`
}

// McpServerOauthLoginResponse returns the authorization URL to open.
//
// Rust: v2::McpServerOauthLoginResponse, camelCase.
type McpServerOauthLoginResponse struct {
	AuthorizationURL string `json:"authorizationUrl"`
}

// ---------------------------------------------------------------------------
// MCP: notifications
// ---------------------------------------------------------------------------

// McpToolCallProgressNotification reports streaming progress for an MCP tool
// call. Method: "mcpServer/toolCall/progress" is NOT registered here; this type
// exists for completeness and is wired by its owning area. (See mcp.rs.)
//
// Rust: v2::McpToolCallProgressNotification, camelCase.
type McpToolCallProgressNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Message  string `json:"message"`
}

// McpServerOauthLoginCompletedNotification signals OAuth login completion.
// `error` uses skip_serializing_if (omitted when absent).
//
// Rust: v2::McpServerOauthLoginCompletedNotification, camelCase.
// Method: "mcpServer/oauthLogin/completed".
type McpServerOauthLoginCompletedNotification struct {
	Name    string  `json:"name"`
	Success bool    `json:"success"`
	Error   *string `json:"error,omitempty"`
}

// McpServerStatusUpdatedNotification reports an MCP server lifecycle change.
// `error` carries no skip_serializing_if (always emitted, null when absent).
//
// Rust: v2::McpServerStatusUpdatedNotification, camelCase.
// Method: "mcpServer/startupStatus/updated".
type McpServerStatusUpdatedNotification struct {
	Name   string                `json:"name"`
	Status McpServerStartupState `json:"status"`
	Error  *string               `json:"error"`
}

// ---------------------------------------------------------------------------
// MCP: mcpServer/elicitation/request (server -> client request)
// ---------------------------------------------------------------------------

// McpServerElicitationRequestParams is a server->client elicitation request.
// The request body is flattened (#[serde(flatten)]) so the "mode" tag and its
// fields appear at the top level alongside threadId/turnId/serverName.
//
// Rust: v2::McpServerElicitationRequestParams, camelCase. `turnId` carries no
// skip_serializing_if (always emitted, null when absent).
type McpServerElicitationRequestParams struct {
	ThreadID   string                      `json:"threadId"`
	TurnID     *string                     `json:"turnId"`
	ServerName string                      `json:"serverName"`
	Request    McpServerElicitationRequest `json:"-"`
}

// MarshalJSON emits the fixed fields plus the flattened request body.
func (p McpServerElicitationRequestParams) MarshalJSON() ([]byte, error) {
	reqBytes, err := json.Marshal(p.Request)
	if err != nil {
		return nil, fmt.Errorf("appserverproto: marshal elicitation request: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(reqBytes, &fields); err != nil {
		return nil, fmt.Errorf("appserverproto: flatten elicitation request: %w", err)
	}
	fields["threadId"], _ = json.Marshal(p.ThreadID)
	fields["turnId"], _ = json.Marshal(p.TurnID)
	fields["serverName"], _ = json.Marshal(p.ServerName)
	return json.Marshal(fields)
}

// UnmarshalJSON decodes the fixed fields and the flattened request body.
func (p *McpServerElicitationRequestParams) UnmarshalJSON(data []byte) error {
	var head struct {
		ThreadID   string  `json:"threadId"`
		TurnID     *string `json:"turnId"`
		ServerName string  `json:"serverName"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return err
	}
	var req McpServerElicitationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return err
	}
	*p = McpServerElicitationRequestParams{
		ThreadID:   head.ThreadID,
		TurnID:     head.TurnID,
		ServerName: head.ServerName,
		Request:    req,
	}
	return nil
}

// McpServerElicitationRequestMode tags the elicitation request shape.
type McpServerElicitationRequestMode string

const (
	// McpServerElicitationModeForm is the form-schema elicitation mode.
	McpServerElicitationModeForm McpServerElicitationRequestMode = "form"
	// McpServerElicitationModeURL is the URL elicitation mode.
	McpServerElicitationModeURL McpServerElicitationRequestMode = "url"
)

// McpServerElicitationRequest is the internally-tagged ("mode") elicitation
// request body. Form carries a requested schema; Url carries a URL and id.
// `_meta` carries no skip_serializing_if (always emitted, null when absent).
//
// Rust: v2::McpServerElicitationRequest, serde tag = "mode", camelCase.
type McpServerElicitationRequest struct {
	Mode McpServerElicitationRequestMode

	// Meta is the "_meta" field, present for both variants.
	Meta *json.RawMessage
	// Message is present for both variants.
	Message string

	// RequestedSchema is populated only for the Form variant.
	RequestedSchema *McpElicitationSchema
	// URL is populated only for the Url variant.
	URL string
	// ElicitationID is populated only for the Url variant.
	ElicitationID string
}

type elicitationFormBody struct {
	Mode            McpServerElicitationRequestMode `json:"mode"`
	Meta            *json.RawMessage                `json:"_meta"`
	Message         string                          `json:"message"`
	RequestedSchema McpElicitationSchema            `json:"requestedSchema"`
}

type elicitationURLBody struct {
	Mode          McpServerElicitationRequestMode `json:"mode"`
	Meta          *json.RawMessage                `json:"_meta"`
	Message       string                          `json:"message"`
	URL           string                          `json:"url"`
	ElicitationID string                          `json:"elicitationId"`
}

// MarshalJSON encodes the internally-tagged elicitation request.
func (r McpServerElicitationRequest) MarshalJSON() ([]byte, error) {
	switch r.Mode {
	case McpServerElicitationModeForm:
		schema := McpElicitationSchema{}
		if r.RequestedSchema != nil {
			schema = *r.RequestedSchema
		}
		return json.Marshal(elicitationFormBody{
			Mode:            McpServerElicitationModeForm,
			Meta:            r.Meta,
			Message:         r.Message,
			RequestedSchema: schema,
		})
	case McpServerElicitationModeURL:
		return json.Marshal(elicitationURLBody{
			Mode:          McpServerElicitationModeURL,
			Meta:          r.Meta,
			Message:       r.Message,
			URL:           r.URL,
			ElicitationID: r.ElicitationID,
		})
	default:
		return nil, fmt.Errorf("appserverproto: unknown McpServerElicitationRequest mode: %q", r.Mode)
	}
}

// UnmarshalJSON decodes the internally-tagged elicitation request.
func (r *McpServerElicitationRequest) UnmarshalJSON(data []byte) error {
	var probe struct {
		Mode McpServerElicitationRequestMode `json:"mode"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	switch probe.Mode {
	case McpServerElicitationModeForm:
		var body elicitationFormBody
		if err := json.Unmarshal(data, &body); err != nil {
			return err
		}
		schema := body.RequestedSchema
		*r = McpServerElicitationRequest{
			Mode:            McpServerElicitationModeForm,
			Meta:            body.Meta,
			Message:         body.Message,
			RequestedSchema: &schema,
		}
		return nil
	case McpServerElicitationModeURL:
		var body elicitationURLBody
		if err := json.Unmarshal(data, &body); err != nil {
			return err
		}
		*r = McpServerElicitationRequest{
			Mode:          McpServerElicitationModeURL,
			Meta:          body.Meta,
			Message:       body.Message,
			URL:           body.URL,
			ElicitationID: body.ElicitationID,
		}
		return nil
	default:
		return fmt.Errorf("appserverproto: unknown McpServerElicitationRequest mode: %q", probe.Mode)
	}
}

// McpServerElicitationRequestResponse is the client's elicitation reply.
// `content` and `_meta` carry no skip_serializing_if (always emitted, null when
// absent).
//
// Rust: v2::McpServerElicitationRequestResponse, camelCase, meta renamed "_meta".
type McpServerElicitationRequestResponse struct {
	Action  McpServerElicitationAction `json:"action"`
	Content *json.RawMessage           `json:"content"`
	Meta    *json.RawMessage           `json:"_meta"`
}

// ---------------------------------------------------------------------------
// Permissions: permissionProfile/list
// ---------------------------------------------------------------------------

// PermissionProfileListParams pages over available permission profiles. None of
// the optional fields carry skip_serializing_if, so all keys are always emitted
// (null when absent).
//
// Rust: v2::PermissionProfileListParams, camelCase.
type PermissionProfileListParams struct {
	Cursor *string `json:"cursor"`
	Limit  *uint32 `json:"limit"`
	Cwd    *string `json:"cwd"`
}

// PermissionProfileSummary is a single available permission profile. `description`
// carries no skip_serializing_if (always emitted, null when absent).
//
// Rust: v2::PermissionProfileSummary, camelCase.
type PermissionProfileSummary struct {
	ID          string  `json:"id"`
	Description *string `json:"description"`
}

// PermissionProfileListResponse pages over permission profiles. `nextCursor` is
// always emitted (null when there are no more items).
//
// Rust: v2::PermissionProfileListResponse, camelCase.
type PermissionProfileListResponse struct {
	Data       []PermissionProfileSummary `json:"data"`
	NextCursor *string                    `json:"nextCursor"`
}

// MarshalJSON ensures `data` always emits a JSON array (never null).
func (r PermissionProfileListResponse) MarshalJSON() ([]byte, error) {
	type alias PermissionProfileListResponse
	out := alias(r)
	if out.Data == nil {
		out.Data = []PermissionProfileSummary{}
	}
	return json.Marshal(out)
}

// ---------------------------------------------------------------------------
// Account: enums
// ---------------------------------------------------------------------------

// ChatgptAuthTokensRefreshReason explains why a token refresh was requested.
//
// Rust: v2::ChatgptAuthTokensRefreshReason, camelCase.
type ChatgptAuthTokensRefreshReason string

const (
	// ChatgptAuthTokensRefreshReasonUnauthorized: a backend request returned 401.
	ChatgptAuthTokensRefreshReasonUnauthorized ChatgptAuthTokensRefreshReason = "unauthorized"
)

// CancelLoginAccountStatus reports the outcome of a login cancellation.
//
// Rust: v2::CancelLoginAccountStatus, camelCase.
type CancelLoginAccountStatus string

const (
	// CancelLoginAccountStatusCanceled means the login was canceled.
	CancelLoginAccountStatusCanceled CancelLoginAccountStatus = "canceled"
	// CancelLoginAccountStatusNotFound means no matching login existed.
	CancelLoginAccountStatusNotFound CancelLoginAccountStatus = "notFound"
)

// AddCreditsNudgeCreditType selects the credit type for a nudge email.
//
// Rust: v2::AddCreditsNudgeCreditType, serde rename_all = "snake_case".
type AddCreditsNudgeCreditType string

const (
	// AddCreditsNudgeCreditTypeCredits nudges about credits.
	AddCreditsNudgeCreditTypeCredits AddCreditsNudgeCreditType = "credits"
	// AddCreditsNudgeCreditTypeUsageLimit nudges about usage limits.
	AddCreditsNudgeCreditTypeUsageLimit AddCreditsNudgeCreditType = "usage_limit"
)

// AddCreditsNudgeEmailStatus reports the outcome of a nudge email send.
//
// Rust: v2::AddCreditsNudgeEmailStatus, serde rename_all = "snake_case".
type AddCreditsNudgeEmailStatus string

const (
	// AddCreditsNudgeEmailStatusSent means the email was sent.
	AddCreditsNudgeEmailStatusSent AddCreditsNudgeEmailStatus = "sent"
	// AddCreditsNudgeEmailStatusCooldownActive means a cooldown blocked sending.
	AddCreditsNudgeEmailStatusCooldownActive AddCreditsNudgeEmailStatus = "cooldown_active"
)

// RateLimitReachedType identifies which rate-limit boundary was reached.
//
// Rust: v2::RateLimitReachedType, serde rename_all = "snake_case".
type RateLimitReachedType string

const (
	// RateLimitReachedTypeRateLimitReached: a rate limit was reached.
	RateLimitReachedTypeRateLimitReached RateLimitReachedType = "rate_limit_reached"
	// RateLimitReachedTypeWorkspaceOwnerCreditsDepleted: owner credits depleted.
	RateLimitReachedTypeWorkspaceOwnerCreditsDepleted RateLimitReachedType = "workspace_owner_credits_depleted"
	// RateLimitReachedTypeWorkspaceMemberCreditsDepleted: member credits depleted.
	RateLimitReachedTypeWorkspaceMemberCreditsDepleted RateLimitReachedType = "workspace_member_credits_depleted"
	// RateLimitReachedTypeWorkspaceOwnerUsageLimitReached: owner usage limit hit.
	RateLimitReachedTypeWorkspaceOwnerUsageLimitReached RateLimitReachedType = "workspace_owner_usage_limit_reached"
	// RateLimitReachedTypeWorkspaceMemberUsageLimitReached: member usage limit hit.
	RateLimitReachedTypeWorkspaceMemberUsageLimitReached RateLimitReachedType = "workspace_member_usage_limit_reached"
)

// ---------------------------------------------------------------------------
// Account: Account union (account/read result member)
// ---------------------------------------------------------------------------

// AccountKind discriminates the internally-tagged Account union.
type AccountKind string

const (
	// AccountKindApiKey is the API-key account.
	AccountKindApiKey AccountKind = "apiKey"
	// AccountKindChatgpt is the ChatGPT-managed account.
	AccountKindChatgpt AccountKind = "chatgpt"
	// AccountKindAmazonBedrock is the Amazon Bedrock account.
	AccountKindAmazonBedrock AccountKind = "amazonBedrock"
)

// Account is the internally-tagged (tag = "type") account union. Only the
// Chatgpt variant carries fields (email, planType).
//
// Rust: v2::Account, serde tag = "type", camelCase.
type Account struct {
	Type AccountKind

	// Email is populated only for the Chatgpt variant.
	Email string
	// PlanType is populated only for the Chatgpt variant.
	PlanType protocol.PlanType
}

type accountChatgptBody struct {
	Type     AccountKind       `json:"type"`
	Email    string            `json:"email"`
	PlanType protocol.PlanType `json:"planType"`
}

type accountTagOnly struct {
	Type AccountKind `json:"type"`
}

// MarshalJSON encodes the internally-tagged account.
func (a Account) MarshalJSON() ([]byte, error) {
	switch a.Type {
	case AccountKindApiKey, AccountKindAmazonBedrock:
		return json.Marshal(accountTagOnly{Type: a.Type})
	case AccountKindChatgpt:
		return json.Marshal(accountChatgptBody{
			Type:     AccountKindChatgpt,
			Email:    a.Email,
			PlanType: a.PlanType,
		})
	default:
		return nil, fmt.Errorf("appserverproto: unknown Account type: %q", a.Type)
	}
}

// UnmarshalJSON decodes the internally-tagged account.
func (a *Account) UnmarshalJSON(data []byte) error {
	var tag accountTagOnly
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	switch tag.Type {
	case AccountKindApiKey, AccountKindAmazonBedrock:
		*a = Account{Type: tag.Type}
		return nil
	case AccountKindChatgpt:
		var body accountChatgptBody
		if err := json.Unmarshal(data, &body); err != nil {
			return err
		}
		*a = Account{Type: AccountKindChatgpt, Email: body.Email, PlanType: body.PlanType}
		return nil
	default:
		return fmt.Errorf("appserverproto: unknown Account type: %q", tag.Type)
	}
}

// ---------------------------------------------------------------------------
// Account: account/login/start
// ---------------------------------------------------------------------------

// LoginAccountKind discriminates the internally-tagged LoginAccountParams union.
type LoginAccountKind string

const (
	// LoginAccountKindApiKey logs in with an API key.
	LoginAccountKindApiKey LoginAccountKind = "apiKey"
	// LoginAccountKindChatgpt logs in via ChatGPT OAuth.
	LoginAccountKindChatgpt LoginAccountKind = "chatgpt"
	// LoginAccountKindChatgptDeviceCode logs in via ChatGPT device code.
	LoginAccountKindChatgptDeviceCode LoginAccountKind = "chatgptDeviceCode"
	// LoginAccountKindChatgptAuthTokens logs in with caller-supplied tokens
	// (experimental: account/login/start.chatgptAuthTokens).
	LoginAccountKindChatgptAuthTokens LoginAccountKind = "chatgptAuthTokens"
)

// LoginAccountParams is the internally-tagged (tag = "type") login request.
//
// Rust: v2::LoginAccountParams, serde tag = "type". The ChatgptAuthTokens
// variant is experimental.
type LoginAccountParams struct {
	Type LoginAccountKind

	// APIKey is populated only for the ApiKey variant.
	APIKey string
	// CodexStreamlinedLogin is the Chatgpt variant flag. It uses
	// skip_serializing_if = "std::ops::Not::not" so a false value is omitted.
	CodexStreamlinedLogin bool

	// AccessToken is populated only for the ChatgptAuthTokens variant.
	AccessToken string
	// ChatgptAccountID is populated only for the ChatgptAuthTokens variant.
	ChatgptAccountID string
	// ChatgptPlanType carries no skip_serializing_if for the ChatgptAuthTokens
	// variant (always emitted, null when absent).
	ChatgptPlanType *string
}

type loginAPIKeyBody struct {
	Type   LoginAccountKind `json:"type"`
	APIKey string           `json:"apiKey"`
}

type loginChatgptBody struct {
	Type                  LoginAccountKind `json:"type"`
	CodexStreamlinedLogin bool             `json:"codexStreamlinedLogin,omitempty"`
}

type loginAuthTokensBody struct {
	Type             LoginAccountKind `json:"type"`
	AccessToken      string           `json:"accessToken"`
	ChatgptAccountID string           `json:"chatgptAccountId"`
	ChatgptPlanType  *string          `json:"chatgptPlanType"`
}

// MarshalJSON encodes the internally-tagged login params.
func (p LoginAccountParams) MarshalJSON() ([]byte, error) {
	switch p.Type {
	case LoginAccountKindApiKey:
		return json.Marshal(loginAPIKeyBody{Type: p.Type, APIKey: p.APIKey})
	case LoginAccountKindChatgpt:
		return json.Marshal(loginChatgptBody{Type: p.Type, CodexStreamlinedLogin: p.CodexStreamlinedLogin})
	case LoginAccountKindChatgptDeviceCode:
		return json.Marshal(accountTagOnly{Type: AccountKind(p.Type)})
	case LoginAccountKindChatgptAuthTokens:
		return json.Marshal(loginAuthTokensBody{
			Type:             p.Type,
			AccessToken:      p.AccessToken,
			ChatgptAccountID: p.ChatgptAccountID,
			ChatgptPlanType:  p.ChatgptPlanType,
		})
	default:
		return nil, fmt.Errorf("appserverproto: unknown LoginAccountParams type: %q", p.Type)
	}
}

// UnmarshalJSON decodes the internally-tagged login params.
func (p *LoginAccountParams) UnmarshalJSON(data []byte) error {
	var tag accountTagOnly
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	kind := LoginAccountKind(tag.Type)
	switch kind {
	case LoginAccountKindApiKey:
		var body loginAPIKeyBody
		if err := json.Unmarshal(data, &body); err != nil {
			return err
		}
		*p = LoginAccountParams{Type: kind, APIKey: body.APIKey}
		return nil
	case LoginAccountKindChatgpt:
		var body loginChatgptBody
		if err := json.Unmarshal(data, &body); err != nil {
			return err
		}
		*p = LoginAccountParams{Type: kind, CodexStreamlinedLogin: body.CodexStreamlinedLogin}
		return nil
	case LoginAccountKindChatgptDeviceCode:
		*p = LoginAccountParams{Type: kind}
		return nil
	case LoginAccountKindChatgptAuthTokens:
		var body loginAuthTokensBody
		if err := json.Unmarshal(data, &body); err != nil {
			return err
		}
		*p = LoginAccountParams{
			Type:             kind,
			AccessToken:      body.AccessToken,
			ChatgptAccountID: body.ChatgptAccountID,
			ChatgptPlanType:  body.ChatgptPlanType,
		}
		return nil
	default:
		return fmt.Errorf("appserverproto: unknown LoginAccountParams type: %q", kind)
	}
}

// LoginAccountResponseKind discriminates the LoginAccountResponse union.
type LoginAccountResponseKind string

const (
	// LoginAccountResponseKindApiKey acknowledges an API-key login.
	LoginAccountResponseKindApiKey LoginAccountResponseKind = "apiKey"
	// LoginAccountResponseKindChatgpt returns OAuth flow details.
	LoginAccountResponseKindChatgpt LoginAccountResponseKind = "chatgpt"
	// LoginAccountResponseKindChatgptDeviceCode returns device-code details.
	LoginAccountResponseKindChatgptDeviceCode LoginAccountResponseKind = "chatgptDeviceCode"
	// LoginAccountResponseKindChatgptAuthTokens acknowledges a token login.
	LoginAccountResponseKindChatgptAuthTokens LoginAccountResponseKind = "chatgptAuthTokens"
)

// LoginAccountResponse is the internally-tagged (tag = "type") login response.
//
// Rust: v2::LoginAccountResponse, serde tag = "type", camelCase.
type LoginAccountResponse struct {
	Type LoginAccountResponseKind

	// LoginID is populated for the Chatgpt and ChatgptDeviceCode variants.
	LoginID string
	// AuthURL is populated only for the Chatgpt variant.
	AuthURL string
	// VerificationURL is populated only for the ChatgptDeviceCode variant.
	VerificationURL string
	// UserCode is populated only for the ChatgptDeviceCode variant.
	UserCode string
}

type loginRespChatgptBody struct {
	Type    LoginAccountResponseKind `json:"type"`
	LoginID string                   `json:"loginId"`
	AuthURL string                   `json:"authUrl"`
}

type loginRespDeviceCodeBody struct {
	Type            LoginAccountResponseKind `json:"type"`
	LoginID         string                   `json:"loginId"`
	VerificationURL string                   `json:"verificationUrl"`
	UserCode        string                   `json:"userCode"`
}

// MarshalJSON encodes the internally-tagged login response.
func (r LoginAccountResponse) MarshalJSON() ([]byte, error) {
	switch r.Type {
	case LoginAccountResponseKindApiKey, LoginAccountResponseKindChatgptAuthTokens:
		return json.Marshal(accountTagOnly{Type: AccountKind(r.Type)})
	case LoginAccountResponseKindChatgpt:
		return json.Marshal(loginRespChatgptBody{Type: r.Type, LoginID: r.LoginID, AuthURL: r.AuthURL})
	case LoginAccountResponseKindChatgptDeviceCode:
		return json.Marshal(loginRespDeviceCodeBody{
			Type:            r.Type,
			LoginID:         r.LoginID,
			VerificationURL: r.VerificationURL,
			UserCode:        r.UserCode,
		})
	default:
		return nil, fmt.Errorf("appserverproto: unknown LoginAccountResponse type: %q", r.Type)
	}
}

// UnmarshalJSON decodes the internally-tagged login response.
func (r *LoginAccountResponse) UnmarshalJSON(data []byte) error {
	var tag accountTagOnly
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	kind := LoginAccountResponseKind(tag.Type)
	switch kind {
	case LoginAccountResponseKindApiKey, LoginAccountResponseKindChatgptAuthTokens:
		*r = LoginAccountResponse{Type: kind}
		return nil
	case LoginAccountResponseKindChatgpt:
		var body loginRespChatgptBody
		if err := json.Unmarshal(data, &body); err != nil {
			return err
		}
		*r = LoginAccountResponse{Type: kind, LoginID: body.LoginID, AuthURL: body.AuthURL}
		return nil
	case LoginAccountResponseKindChatgptDeviceCode:
		var body loginRespDeviceCodeBody
		if err := json.Unmarshal(data, &body); err != nil {
			return err
		}
		*r = LoginAccountResponse{
			Type:            kind,
			LoginID:         body.LoginID,
			VerificationURL: body.VerificationURL,
			UserCode:        body.UserCode,
		}
		return nil
	default:
		return fmt.Errorf("appserverproto: unknown LoginAccountResponse type: %q", kind)
	}
}

// ---------------------------------------------------------------------------
// Account: remaining request/response/notification structs
// ---------------------------------------------------------------------------

// CancelLoginAccountParams cancels an in-flight login by id.
//
// Rust: v2::CancelLoginAccountParams, camelCase.
type CancelLoginAccountParams struct {
	LoginID string `json:"loginId"`
}

// CancelLoginAccountResponse reports the cancellation status.
//
// Rust: v2::CancelLoginAccountResponse, camelCase.
type CancelLoginAccountResponse struct {
	Status CancelLoginAccountStatus `json:"status"`
}

// LogoutAccountResponse is the empty response to account/logout.
//
// Rust: v2::LogoutAccountResponse (empty struct -> {}).
type LogoutAccountResponse struct{}

// ChatgptAuthTokensRefreshParams requests fresh ChatGPT auth tokens (server ->
// client request). `previousAccountId` uses no skip_serializing_if at struct
// level; it is `#[ts(optional = nullable)]` and always emitted (null when
// absent).
//
// Rust: v2::ChatgptAuthTokensRefreshParams, camelCase.
type ChatgptAuthTokensRefreshParams struct {
	Reason            ChatgptAuthTokensRefreshReason `json:"reason"`
	PreviousAccountID *string                        `json:"previousAccountId"`
}

// ChatgptAuthTokensRefreshResponse returns refreshed tokens. `chatgptPlanType`
// carries no skip_serializing_if (always emitted, null when absent).
//
// Rust: v2::ChatgptAuthTokensRefreshResponse, camelCase.
type ChatgptAuthTokensRefreshResponse struct {
	AccessToken      string  `json:"accessToken"`
	ChatgptAccountID string  `json:"chatgptAccountId"`
	ChatgptPlanType  *string `json:"chatgptPlanType"`
}

// SendAddCreditsNudgeEmailParams requests a credits nudge email.
//
// Rust: v2::SendAddCreditsNudgeEmailParams, camelCase.
type SendAddCreditsNudgeEmailParams struct {
	CreditType AddCreditsNudgeCreditType `json:"creditType"`
}

// SendAddCreditsNudgeEmailResponse reports the nudge email status.
//
// Rust: v2::SendAddCreditsNudgeEmailResponse, camelCase.
type SendAddCreditsNudgeEmailResponse struct {
	Status AddCreditsNudgeEmailStatus `json:"status"`
}

// GetAccountParams reads the current account. `refreshToken` uses
// skip_serializing_if = "std::ops::Not::not" (omitted when false).
//
// Rust: v2::GetAccountParams, camelCase.
type GetAccountParams struct {
	RefreshToken bool `json:"refreshToken,omitempty"`
}

// GetAccountResponse returns the current account, if any. `account` carries no
// skip_serializing_if (always emitted, null when absent).
//
// Rust: v2::GetAccountResponse, camelCase.
type GetAccountResponse struct {
	Account            *Account `json:"account"`
	RequiresOpenaiAuth bool     `json:"requiresOpenaiAuth"`
}

// GetAccountRateLimitsResponse returns rate-limit snapshots. `rateLimitsByLimitId`
// carries no skip_serializing_if (always emitted, null when absent).
//
// Rust: v2::GetAccountRateLimitsResponse, camelCase.
type GetAccountRateLimitsResponse struct {
	RateLimits          RateLimitSnapshot            `json:"rateLimits"`
	RateLimitsByLimitID map[string]RateLimitSnapshot `json:"rateLimitsByLimitId"`
}

// RateLimitSnapshot is a backward-compatible rate-limit view. Every optional
// field carries no skip_serializing_if (always emitted, null when absent).
//
// Rust: v2::RateLimitSnapshot, camelCase.
type RateLimitSnapshot struct {
	LimitID              *string               `json:"limitId"`
	LimitName            *string               `json:"limitName"`
	Primary              *RateLimitWindow      `json:"primary"`
	Secondary            *RateLimitWindow      `json:"secondary"`
	Credits              *CreditsSnapshot      `json:"credits"`
	PlanType             *protocol.PlanType    `json:"planType"`
	RateLimitReachedType *RateLimitReachedType `json:"rateLimitReachedType"`
}

// RateLimitWindow describes one rate-limit window. `windowDurationMins` and
// `resetsAt` carry no skip_serializing_if (always emitted, null when absent).
//
// Rust: v2::RateLimitWindow, camelCase.
type RateLimitWindow struct {
	UsedPercent        int32  `json:"usedPercent"`
	WindowDurationMins *int64 `json:"windowDurationMins"`
	ResetsAt           *int64 `json:"resetsAt"`
}

// CreditsSnapshot describes account credits. `balance` carries no
// skip_serializing_if (always emitted, null when absent).
//
// Rust: v2::CreditsSnapshot, camelCase.
type CreditsSnapshot struct {
	HasCredits bool    `json:"hasCredits"`
	Unlimited  bool    `json:"unlimited"`
	Balance    *string `json:"balance"`
}

// AccountUpdatedNotification reports an account/auth change. Both fields carry no
// skip_serializing_if (always emitted, null when absent).
//
// Rust: v2::AccountUpdatedNotification, camelCase. Method: "account/updated".
type AccountUpdatedNotification struct {
	AuthMode *AuthMode          `json:"authMode"`
	PlanType *protocol.PlanType `json:"planType"`
}

// AccountRateLimitsUpdatedNotification reports new rate-limit snapshots.
//
// Rust: v2::AccountRateLimitsUpdatedNotification, camelCase.
// Method: "account/rateLimits/updated".
type AccountRateLimitsUpdatedNotification struct {
	RateLimits RateLimitSnapshot `json:"rateLimits"`
}

// AccountLoginCompletedNotification signals login completion. All optional fields
// carry no skip_serializing_if (always emitted, null when absent).
//
// Rust: v2::AccountLoginCompletedNotification, camelCase.
type AccountLoginCompletedNotification struct {
	LoginID *string `json:"loginId"`
	Success bool    `json:"success"`
	Error   *string `json:"error"`
}

// ---------------------------------------------------------------------------
// Review: review/start
// ---------------------------------------------------------------------------

// ReviewDelivery selects where a review runs.
//
// Rust: v2::ReviewDelivery (via v2_enum_from_core!, rename_all = "camelCase").
type ReviewDelivery string

const (
	// ReviewDeliveryInline runs the review on the current thread.
	ReviewDeliveryInline ReviewDelivery = "inline"
	// ReviewDeliveryDetached runs the review on a new thread.
	ReviewDeliveryDetached ReviewDelivery = "detached"
)

// ReviewTargetKind discriminates the internally-tagged ReviewTarget union.
type ReviewTargetKind string

const (
	// ReviewTargetKindUncommittedChanges reviews the working tree.
	ReviewTargetKindUncommittedChanges ReviewTargetKind = "uncommittedChanges"
	// ReviewTargetKindBaseBranch reviews against a base branch.
	ReviewTargetKindBaseBranch ReviewTargetKind = "baseBranch"
	// ReviewTargetKindCommit reviews a specific commit.
	ReviewTargetKindCommit ReviewTargetKind = "commit"
	// ReviewTargetKindCustom reviews via free-form instructions.
	ReviewTargetKindCustom ReviewTargetKind = "custom"
)

// ReviewTarget is the internally-tagged (tag = "type") review target union.
//
// Rust: v2::ReviewTarget, serde tag = "type", camelCase.
type ReviewTarget struct {
	Type ReviewTargetKind

	// Branch is populated only for the BaseBranch variant.
	Branch string
	// Sha is populated only for the Commit variant.
	Sha string
	// Title carries no skip_serializing_if for the Commit variant (always
	// emitted, null when absent).
	Title *string
	// Instructions is populated only for the Custom variant.
	Instructions string
}

type reviewTargetBaseBranchBody struct {
	Type   ReviewTargetKind `json:"type"`
	Branch string           `json:"branch"`
}

type reviewTargetCommitBody struct {
	Type  ReviewTargetKind `json:"type"`
	Sha   string           `json:"sha"`
	Title *string          `json:"title"`
}

type reviewTargetCustomBody struct {
	Type         ReviewTargetKind `json:"type"`
	Instructions string           `json:"instructions"`
}

// MarshalJSON encodes the internally-tagged review target.
func (t ReviewTarget) MarshalJSON() ([]byte, error) {
	switch t.Type {
	case ReviewTargetKindUncommittedChanges:
		return json.Marshal(accountTagOnly{Type: AccountKind(t.Type)})
	case ReviewTargetKindBaseBranch:
		return json.Marshal(reviewTargetBaseBranchBody{Type: t.Type, Branch: t.Branch})
	case ReviewTargetKindCommit:
		return json.Marshal(reviewTargetCommitBody{Type: t.Type, Sha: t.Sha, Title: t.Title})
	case ReviewTargetKindCustom:
		return json.Marshal(reviewTargetCustomBody{Type: t.Type, Instructions: t.Instructions})
	default:
		return nil, fmt.Errorf("appserverproto: unknown ReviewTarget type: %q", t.Type)
	}
}

// UnmarshalJSON decodes the internally-tagged review target.
func (t *ReviewTarget) UnmarshalJSON(data []byte) error {
	var tag accountTagOnly
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	kind := ReviewTargetKind(tag.Type)
	switch kind {
	case ReviewTargetKindUncommittedChanges:
		*t = ReviewTarget{Type: kind}
		return nil
	case ReviewTargetKindBaseBranch:
		var body reviewTargetBaseBranchBody
		if err := json.Unmarshal(data, &body); err != nil {
			return err
		}
		*t = ReviewTarget{Type: kind, Branch: body.Branch}
		return nil
	case ReviewTargetKindCommit:
		var body reviewTargetCommitBody
		if err := json.Unmarshal(data, &body); err != nil {
			return err
		}
		*t = ReviewTarget{Type: kind, Sha: body.Sha, Title: body.Title}
		return nil
	case ReviewTargetKindCustom:
		var body reviewTargetCustomBody
		if err := json.Unmarshal(data, &body); err != nil {
			return err
		}
		*t = ReviewTarget{Type: kind, Instructions: body.Instructions}
		return nil
	default:
		return fmt.Errorf("appserverproto: unknown ReviewTarget type: %q", kind)
	}
}

// ReviewStartParams starts a review on a thread. `delivery` carries no
// skip_serializing_if (always emitted, null when absent).
//
// Rust: v2::ReviewStartParams, camelCase.
type ReviewStartParams struct {
	ThreadID string          `json:"threadId"`
	Target   ReviewTarget    `json:"target"`
	Delivery *ReviewDelivery `json:"delivery"`
}

// ReviewStartResponse returns the review turn and its thread id. `turn` is the
// v2 Turn type, owned by a sibling area, so it is passed through as raw JSON to
// stay wire-faithful without redefining it.
//
// Rust: v2::ReviewStartResponse, camelCase.
type ReviewStartResponse struct {
	Turn           json.RawMessage `json:"turn"`
	ReviewThreadID string          `json:"reviewThreadId"`
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

// newEmptyOptionalParams returns a decode target for methods whose params are
// Option<()> (i.e. the params key may be absent). A *json.RawMessage accepts the
// JSON null that the registry substitutes for absent params.
func newEmptyOptionalParams() any { return new(json.RawMessage) }

func init() {
	// MCP client requests.
	Register(MethodSpec{
		Method:    "mcpServerStatus/list",
		NewParams: func() any { return new(ListMcpServerStatusParams) },
		NewResult: func() any { return new(ListMcpServerStatusResponse) },
	})
	Register(MethodSpec{
		Method:    "mcpServer/resource/read",
		NewParams: func() any { return new(McpResourceReadParams) },
		NewResult: func() any { return new(McpResourceReadResponse) },
	})
	Register(MethodSpec{
		Method:    "mcpServer/tool/call",
		NewParams: func() any { return new(McpServerToolCallParams) },
		NewResult: func() any { return new(McpServerToolCallResponse) },
	})
	Register(MethodSpec{
		Method:    "config/mcpServer/reload",
		NewParams: newEmptyOptionalParams,
		NewResult: func() any { return new(McpServerRefreshResponse) },
	})
	Register(MethodSpec{
		Method:    "mcpServer/oauth/login",
		NewParams: func() any { return new(McpServerOauthLoginParams) },
		NewResult: func() any { return new(McpServerOauthLoginResponse) },
	})

	// Permissions client request.
	Register(MethodSpec{
		Method:    "permissionProfile/list",
		NewParams: func() any { return new(PermissionProfileListParams) },
		NewResult: func() any { return new(PermissionProfileListResponse) },
	})

	// Account client requests.
	Register(MethodSpec{
		Method:    "account/login/start",
		NewParams: func() any { return new(LoginAccountParams) },
		NewResult: func() any { return new(LoginAccountResponse) },
	})
	Register(MethodSpec{
		Method:    "account/login/cancel",
		NewParams: func() any { return new(CancelLoginAccountParams) },
		NewResult: func() any { return new(CancelLoginAccountResponse) },
	})
	Register(MethodSpec{
		Method:    "account/logout",
		NewParams: newEmptyOptionalParams,
		NewResult: func() any { return new(LogoutAccountResponse) },
	})
	Register(MethodSpec{
		Method:    "account/rateLimits/read",
		NewParams: newEmptyOptionalParams,
		NewResult: func() any { return new(GetAccountRateLimitsResponse) },
	})
	Register(MethodSpec{
		Method:    "account/sendAddCreditsNudgeEmail",
		NewParams: func() any { return new(SendAddCreditsNudgeEmailParams) },
		NewResult: func() any { return new(SendAddCreditsNudgeEmailResponse) },
	})
	Register(MethodSpec{
		Method:    "account/read",
		NewParams: func() any { return new(GetAccountParams) },
		NewResult: func() any { return new(GetAccountResponse) },
	})

	// Review client request.
	Register(MethodSpec{
		Method:    "review/start",
		NewParams: func() any { return new(ReviewStartParams) },
		NewResult: func() any { return new(ReviewStartResponse) },
	})

	// Server -> client requests.
	RegisterServerRequest(ServerRequestSpec{
		Method:    "mcpServer/elicitation/request",
		NewParams: func() any { return new(McpServerElicitationRequestParams) },
		NewResult: func() any { return new(McpServerElicitationRequestResponse) },
	})
	RegisterServerRequest(ServerRequestSpec{
		Method:    "account/chatgptAuthTokens/refresh",
		NewParams: func() any { return new(ChatgptAuthTokensRefreshParams) },
		NewResult: func() any { return new(ChatgptAuthTokensRefreshResponse) },
	})

	// Server -> client notifications.
	RegisterNotification(NotificationSpec{
		Method:    "mcpServer/oauthLogin/completed",
		NewParams: func() any { return new(McpServerOauthLoginCompletedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "mcpServer/startupStatus/updated",
		NewParams: func() any { return new(McpServerStatusUpdatedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "account/updated",
		NewParams: func() any { return new(AccountUpdatedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "account/rateLimits/updated",
		NewParams: func() any { return new(AccountRateLimitsUpdatedNotification) },
	})
}
