package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/dbaa"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// connectArgs are the connect arguments. With no explicit host, the connection
// is resolved from the opendb config (~/.dbaa/config.yaml) — by name when given,
// else the default GaussDB/openGauss target — so the user need not re-enter
// credentials. Explicit host/port/user/password override the config.
type connectArgs struct {
	Name     string `json:"name"`
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
		Description: "Connect to a GaussDB/openGauss instance (sets the active connection for subsequent tools). With no args, connects to the default connection from the opendb config (~/.dbaa/config.yaml); pass name to pick a configured connection by name; or pass host/port/user/password explicitly.",
		InputSchema: jsonObjSchema(map[string]any{
			"name":     strProp("name of a connection in ~/.dbaa/config.yaml (e.g. gauss_local); omit for the default"),
			"host":     strProp("GaussDB host (overrides config)"),
			"port":     intProp("GaussDB port (e.g. 8000)"),
			"user":     strProp("database user"),
			"password": strProp("database password"),
			"database": strProp("database name (default postgres)"),
			"sslmode":  strProp("ssl mode: disable (default) | require | verify-ca | verify-full"),
			"label":    strProp("display label for this connection"),
		}),
	}

	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		var a connectArgs
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}

		var target db.Target
		label := a.Label
		source := "args"

		// Start from the opendb config when a name is given or no explicit host is
		// provided; otherwise start from the explicit args.
		if a.Name != "" || a.Host == "" {
			t, lbl, err := dbaa.Resolve(a.Name)
			if err != nil {
				if a.Host == "" {
					return mcp.CallToolResult{}, fmt.Errorf("connect: %w (pass host/port/user explicitly, or define the connection in %s)", err, dbaa.ConfigPath())
				}
				// A name was given but not found, yet explicit fields exist: use them.
			} else {
				target = t
				source = "dbaa"
				if label == "" {
					label = lbl
				}
			}
		}

		// Overlay any explicit fields so callers can override the config — e.g.
		// supply the real password when the config value is stale:
		//   connect {"name":"gauss_local","password":"…"}
		if a.Host != "" {
			target.Host = a.Host
		}
		if a.Port != 0 {
			target.Port = a.Port
		}
		if a.User != "" {
			target.User = a.User
		}
		if a.Password != "" {
			target.Password = a.Password
		}
		if a.Database != "" {
			target.Database = a.Database
		}
		if a.SSLMode != "" {
			target.SSLMode = a.SSLMode
		}
		if label == "" {
			label = fmt.Sprintf("%s:%d/%s", target.Host, target.Port, target.Database)
		}

		if err := conn.Connect(ctx, target, label); err != nil {
			return mcp.CallToolResult{}, err
		}
		out, _ := json.Marshal(map[string]any{
			"connected": true,
			"label":     label,
			"source":    source,
		})
		return mcp.CallToolResult{Content: []mcp.ContentItem{mcp.TextContent(string(out))}}, nil
	})
}
