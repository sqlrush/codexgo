package protocol

// This file ports `mcp_approval_meta.rs`: the string constants used as keys and
// values inside the `_meta` map of MCP approval requests/responses. They are
// reproduced verbatim so the on-the-wire metadata matches codex byte-for-byte.

const (
	// ApprovalKindKey is the `_meta` key naming the kind of approval.
	ApprovalKindKey = "codex_approval_kind"
	// ApprovalKindMcpToolCall marks an MCP tool-call approval.
	ApprovalKindMcpToolCall = "mcp_tool_call"
	// ApprovalKindToolSuggestion marks a tool-suggestion approval.
	ApprovalKindToolSuggestion = "tool_suggestion"

	// RequestTypeKey is the `_meta` key naming the request type.
	RequestTypeKey = "codex_request_type"
	// RequestTypeApprovalRequest marks an approval request.
	RequestTypeApprovalRequest = "approval_request"

	// ApprovalsReviewerKey is the `_meta` key naming the approvals reviewer.
	ApprovalsReviewerKey = "approvals_reviewer"

	// PersistKey is the `_meta` key controlling approval persistence.
	PersistKey = "persist"
	// PersistSession persists the approval for the current session only.
	PersistSession = "session"
	// PersistAlways persists the approval indefinitely.
	PersistAlways = "always"

	// SourceKey is the `_meta` key naming the approval source.
	SourceKey = "source"
	// SourceConnector marks a connector-sourced approval.
	SourceConnector = "connector"

	// ConnectorIDKey is the `_meta` key carrying the connector id.
	ConnectorIDKey = "connector_id"
	// ConnectorNameKey is the `_meta` key carrying the connector name.
	ConnectorNameKey = "connector_name"
	// ConnectorDescriptionKey is the `_meta` key carrying the connector
	// description.
	ConnectorDescriptionKey = "connector_description"

	// ToolNameKey is the `_meta` key carrying the tool name.
	ToolNameKey = "tool_name"
	// ToolTitleKey is the `_meta` key carrying the tool title.
	ToolTitleKey = "tool_title"
	// ToolDescriptionKey is the `_meta` key carrying the tool description.
	ToolDescriptionKey = "tool_description"
	// ToolParamsKey is the `_meta` key carrying the tool params.
	ToolParamsKey = "tool_params"
	// ToolParamsDisplayKey is the `_meta` key carrying the display form of the
	// tool params.
	ToolParamsDisplayKey = "tool_params_display"
)
