package mcp

import (
	"encoding/json"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// MCPProtocolVersion is the protocol version Codex advertises during the
// initialize handshake. It matches the MCP 2025-06-18 revision used by the
// reference rmcp client.
const MCPProtocolVersion = "2025-06-18"

// Method names for the MCP requests and notifications issued by the client.
// These mirror the JSON-RPC method strings the reference rmcp SDK sends.
const (
	MethodInitialize            = "initialize"
	MethodInitializedNotify     = "notifications/initialized"
	MethodToolsList             = "tools/list"
	MethodToolsCall             = "tools/call"
	MethodResourcesList         = "resources/list"
	MethodResourceTemplatesList = "resources/templates/list"
	MethodResourcesRead         = "resources/read"
	MethodPing                  = "ping"

	// MethodElicitationCreate is a server-initiated request asking the client
	// to gather input from the user.
	MethodElicitationCreate = "elicitation/create"
)

// Implementation describes a party (client or server) participating in the MCP
// session. Rust: rmcp `Implementation`. Wire keys are camelCase.
type Implementation struct {
	Name    string            `json:"name"`
	Title   *string           `json:"title,omitempty"`
	Version string            `json:"version"`
	Icons   []json.RawMessage `json:"icons,omitempty"`
	// WebsiteURL mirrors rmcp's optional websiteUrl field.
	WebsiteURL *string `json:"websiteUrl,omitempty"`
}

// ClientCapabilities advertises the optional features the client supports during
// the handshake. Only the fields Codex populates are modeled; unknown server
// capabilities are preserved as raw JSON on the server side.
type ClientCapabilities struct {
	// Elicitation, when non-nil, advertises that the client can satisfy
	// server-initiated elicitation/create requests. The value is an (empty)
	// capability object, matching rmcp's ElicitationCapability.
	Elicitation *struct{} `json:"elicitation,omitempty"`
	// Experimental and Roots are passed through verbatim when set.
	Experimental json.RawMessage `json:"experimental,omitempty"`
	Roots        json.RawMessage `json:"roots,omitempty"`
	Sampling     json.RawMessage `json:"sampling,omitempty"`
}

// InitializeParams is the payload of the initialize request. Wire keys are
// camelCase, matching rmcp's InitializeRequestParam.
type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      Implementation     `json:"clientInfo"`
}

// ServerCapabilities is the capability set advertised by the server in its
// initialize response. Capabilities Codex inspects are typed; the rest are kept
// as raw JSON so nothing is lost on round-trip.
type ServerCapabilities struct {
	Tools        json.RawMessage `json:"tools,omitempty"`
	Resources    json.RawMessage `json:"resources,omitempty"`
	Prompts      json.RawMessage `json:"prompts,omitempty"`
	Logging      json.RawMessage `json:"logging,omitempty"`
	Completions  json.RawMessage `json:"completions,omitempty"`
	Experimental json.RawMessage `json:"experimental,omitempty"`
}

// InitializeResult is the server's response to the initialize request. Wire keys
// are camelCase, matching rmcp's InitializeResult / ServerInfo.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      Implementation     `json:"serverInfo"`
	Instructions    *string            `json:"instructions,omitempty"`
	Meta            json.RawMessage    `json:"_meta,omitempty"`
}

// PaginatedParams carries the optional pagination cursor accepted by the list
// endpoints. The "cursor" key is omitted when nil, matching rmcp.
type PaginatedParams struct {
	Cursor *string `json:"cursor,omitempty"`
}

// ListToolsResult is the response to tools/list. Wire keys are camelCase.
type ListToolsResult struct {
	Tools      []protocol.Tool `json:"tools"`
	NextCursor *string         `json:"nextCursor,omitempty"`
	Meta       json.RawMessage `json:"_meta,omitempty"`
}

// ListResourcesResult is the response to resources/list. Wire keys are
// camelCase.
type ListResourcesResult struct {
	Resources  []protocol.Resource `json:"resources"`
	NextCursor *string             `json:"nextCursor,omitempty"`
	Meta       json.RawMessage     `json:"_meta,omitempty"`
}

// ListResourceTemplatesResult is the response to resources/templates/list. Wire
// keys are camelCase.
type ListResourceTemplatesResult struct {
	ResourceTemplates []protocol.ResourceTemplate `json:"resourceTemplates"`
	NextCursor        *string                     `json:"nextCursor,omitempty"`
	Meta              json.RawMessage             `json:"_meta,omitempty"`
}

// ReadResourceParams is the payload of resources/read. Wire keys are camelCase.
type ReadResourceParams struct {
	URI string `json:"uri"`
}

// ReadResourceResult is the response to resources/read. Wire keys are camelCase.
type ReadResourceResult struct {
	Contents []protocol.ResourceContent `json:"contents"`
	Meta     json.RawMessage            `json:"_meta,omitempty"`
}

// CallToolParams is the payload of tools/call. The arguments object is omitted
// when nil; "_meta" carries optional request metadata.
type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Meta      json.RawMessage `json:"_meta,omitempty"`
}

// ElicitationCreateParams is the payload of a server-initiated elicitation/create
// request. Codex forwards the message and requested schema to the UI verbatim.
type ElicitationCreateParams struct {
	Message         string          `json:"message"`
	RequestedSchema json.RawMessage `json:"requestedSchema,omitempty"`
	Meta            json.RawMessage `json:"_meta,omitempty"`
}

// ElicitationAction enumerates how the user responded to an elicitation request.
// Rust: rmcp ElicitationAction; wire form is lowercase.
type ElicitationAction string

// ElicitationAction values.
const (
	ElicitationActionAccept  ElicitationAction = "accept"
	ElicitationActionDecline ElicitationAction = "decline"
	ElicitationActionCancel  ElicitationAction = "cancel"
)

// ElicitationResult is the client's reply to elicitation/create. Wire keys are
// camelCase, matching rmcp's CreateElicitationResult.
type ElicitationResult struct {
	Action  ElicitationAction `json:"action"`
	Content json.RawMessage   `json:"content,omitempty"`
	Meta    json.RawMessage   `json:"_meta,omitempty"`
}
