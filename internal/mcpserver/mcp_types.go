package mcpserver

import (
	"encoding/json"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// mcpClientInfo identifies the connecting MCP client (rmcp Implementation).
type mcpClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

// mcpInitializeParams is the MCP initialize request payload (rmcp
// InitializeRequestParams). Only the fields the server reads are modeled.
type mcpInitializeParams struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    json.RawMessage `json:"capabilities,omitempty"`
	ClientInfo      mcpClientInfo   `json:"clientInfo"`
}

// callToolParams is the tools/call request payload (rmcp CallToolRequestParams).
type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// cancelledParams is the notifications/cancelled payload (rmcp
// CancelledNotificationParam). The reason is ignored.
type cancelledParams struct {
	RequestID *RequestID `json:"requestId"`
	Reason    string     `json:"reason,omitempty"`
}

// appserverInitializeParams builds the app-server initialize params used to
// advance the shared handshake. The experimentalApi capability is enabled so the
// full v2 method surface is accepted.
func appserverInitializeParams(name, version string) appserverproto.InitializeParams {
	experimental := true
	return appserverproto.InitializeParams{
		ClientInfo: appserverproto.ClientInfo{Name: name, Version: version},
		Capabilities: &appserverproto.InitializeCapabilities{
			ExperimentalApi: experimental,
		},
	}
}
