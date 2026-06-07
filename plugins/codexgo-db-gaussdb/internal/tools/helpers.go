package tools

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
)

// boolProp builds a JSON-Schema boolean property.
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

// requireConn returns an error if no connection is active. Every query tool
// guards with this so the model gets a clear "connect first" message.
func requireConn(conn *db.Conn) error {
	if !conn.IsConnected() {
		return fmt.Errorf("no active database connection — run connect first")
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
