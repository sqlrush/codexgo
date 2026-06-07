# codexgo-db-gaussdb

A standalone **MCP server** exposing GaussDB / openGauss health-check and
SQL-tuning tools to [codexgo](../../). It is an **independent Go module** —
codexgo never imports it; it runs as an external plugin process and is spoken to
over stdio JSON-RPC.

All tools are **read-only** and return **structured JSON** (codexgo renders the
UI). See [`docs/OPTIMIZATIONS-OVER-OPENDB.zh-CN.md`](../../docs/OPTIMIZATIONS-OVER-OPENDB.zh-CN.md)
for what this improves over opendb, and
[`docs/PLUGIN-DB-DESIGN.zh-CN.md`](../../docs/PLUGIN-DB-DESIGN.zh-CN.md) for the
architecture.

## Tools

| Tool | Purpose |
|------|---------|
| `connect` | Open the active connection (host/port/user required) |
| `health` | Weighted 0–100 health report (uptime, connections, cache hit, dead tuples, xid wraparound, replication) |
| `slowsql` | Slow SQL by avg elapsed (+ max_ms variance, per-SQL cache hit) |
| `topsql` | Top SQL by el/ae/ex/lr/rw |
| `explain` | Execution plan + Seq-Scan/Sort/Nested-Loop issue flags |
| `ash` | Active session wait distribution + session detail |
| `indexhealth` | Unused / invalid / duplicate / bloat-candidate indexes |
| `sqlfetch` | Resolve a unique SQL id to its text (history → statement) |
| `sqltune` | Tuning material: plan + plan_issues + gs_index_advise + 5-axis checklist |
| `planhistory` | Recent executions of one SQL id (spot plan regressions) |
| `wdr` | List WDR snapshots |
| `wdranalyze` | Generate a WDR report between two snapshots for analysis |

## Build

```sh
make build      # -> bin/codexgo-db-gaussdb
make test       # go test -race ./...
make smoke      # initialize + tools/list handshake (no DB needed)
```

The driver is HuaweiCloud's `gaussdb-go` (SCRAM-SHA256; incompatible with pgx),
registered as `gaussdb`.

## How codexgo launches it

`.codex-plugin/plugin.json` points `mcpServers` at `.mcp.json`, which launches
the built binary:

```json
{ "mcpServers": { "gaussdb": { "command": "${CODEX_PLUGIN_ROOT}/bin/codexgo-db-gaussdb" } } }
```

Run `make build` so the binary exists at `bin/` before codexgo starts the server.

## Layout

```
cmd/codexgo-db-gaussdb/main.go   entry point; MCP `instructions` (domain knowledge)
internal/mcp/                    minimal stdio JSON-RPC MCP server (stdlib only)
internal/db/                     gaussdb-go connection, DSN, structured QueryResult
internal/tools/                  one file per capability group; structured reports
```
