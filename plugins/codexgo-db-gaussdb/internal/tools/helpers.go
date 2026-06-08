package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/dbaa"
)

// boolProp builds a JSON-Schema boolean property.
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

// ensureConn guarantees an active connection: if none is open, it auto-connects
// to the default GaussDB/openGauss target defined in the opendb config
// (~/.dbaa/config.yaml), so commands like /health work out of the box. When no
// config target is available it returns a clear, actionable error. Use the
// connect tool to target a specific named connection.
func ensureConn(ctx context.Context, conn *db.Conn) error {
	if conn.IsConnected() {
		return nil
	}
	target, label, err := dbaa.Resolve("")
	if err != nil {
		return fmt.Errorf("no active database connection — run connect, or define one in %s (%v)", dbaa.ConfigPath(), err)
	}
	if cerr := conn.Connect(ctx, target, "dbaa:"+label); cerr != nil {
		return fmt.Errorf("auto-connect to %s failed: %w", label, cerr)
	}
	return nil
}

// decodeArgs unmarshals raw tool arguments into dst, tolerating empty input.
func decodeArgs(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	return nil
}

// clampLimit bounds a user-supplied row limit to a sane range.
func clampLimit(v, def, max int) int {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}
