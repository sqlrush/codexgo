// Package tools registers the GaussDB MCP tools (connect, health, …) on
// the MCP server. Each tool returns STRUCTURED JSON (not pre-rendered text) so
// codexgo renders the UI — the core optimization over opendb's server-side
// pre-rendered strings (see OPTIMIZATIONS-OVER-OPENDB).
package tools

import (
	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// Register wires all tools onto the server, sharing one connection handle.
func Register(s *mcp.Server, conn *db.Conn) {
	// connection + open-ended diagnosis (single-pass: evidence + model analysis)
	registerConnect(s, conn)
	registerHealth(s, conn)
	// monitoring — session & lock (batch 1)
	registerSessions(s, conn)
	registerLocks(s, conn)
	registerLWLocks(s, conn)
	registerLongTx(s, conn)
	// monitoring — MVCC / space (batch 2)
	registerVacuum(s, conn)
	registerXID(s, conn)
	registerBloat(s, conn)
	registerSpace(s, conn)
	registerTempUsage(s, conn)
	registerHotKey(s, conn)
	// monitoring — memory / WAL / replication (batch 3)
	registerGSMem(s, conn)
	registerWAL(s, conn)
	registerReplication(s, conn)
	registerBgWorker(s, conn)
	// statement-view tools: slowsql, topsql, sqlfetch, planhistory
	registerQuery(s, conn)
	// plan + sessions + indexes
	registerExplain(s, conn)
	registerASH(s, conn)
	registerIndexHealth(s, conn)
	// tuning (pass 1 evidence + pass 2 verify) + WDR
	registerSQLTune(s, conn)
	registerSQLTuneVerify(s, conn)
	registerWDR(s, conn)
	// command catalog (no connection required)
	registerHelp(s)
}

// jsonObjSchema is a small helper to build a JSON-Schema object for inputSchema.
func jsonObjSchema(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
