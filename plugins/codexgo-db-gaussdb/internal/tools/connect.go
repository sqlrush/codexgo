package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// connectArgs are the connect arguments. The plugin holds the connection;
// codexgo passes the target (and, for prompt auth, the password it collected
// via request_user_input) — codexgo never holds a DB driver itself.
type connectArgs struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
	SSLMode  string `json:"sslmode"`
	Label    string `json:"label"`
}

func registerConnect(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "connect",
		Description: "Connect to a GaussDB instance (sets the active connection for subsequent tools). Args: host, port, user, password, database, optional sslmode (default disable) and label.",
		InputSchema: jsonObjSchema(map[string]any{
			"host":     strProp("GaussDB host"),
			"port":     intProp("GaussDB port (e.g. 8000)"),
			"user":     strProp("database user"),
			"password": strProp("database password"),
			"database": strProp("database name (default postgres)"),
			"sslmode":  strProp("ssl mode: disable (default) | require | verify-ca | verify-full"),
			"label":    strProp("display label for this connection (e.g. prod-gauss01)"),
		}, "host", "port", "user"),
	}

	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		var a connectArgs
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &a); err != nil {
				return mcp.CallToolResult{}, fmt.Errorf("invalid connect arguments: %w", err)
			}
		}
		label := a.Label
		if label == "" {
			label = fmt.Sprintf("%s:%d/%s", a.Host, a.Port, a.Database)
		}
		if err := conn.Connect(ctx, db.Target{
			Host: a.Host, Port: a.Port, User: a.User,
			Password: a.Password, Database: a.Database, SSLMode: a.SSLMode,
		}, label); err != nil {
			return mcp.CallToolResult{}, err
		}
		out, _ := json.Marshal(map[string]any{
			"connected": true,
			"label":     label,
		})
		return mcp.CallToolResult{Content: []mcp.ContentItem{mcp.TextContent(string(out))}}, nil
	})
}
