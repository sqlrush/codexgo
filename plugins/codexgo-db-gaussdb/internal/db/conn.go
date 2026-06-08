// Package db owns the GaussDB connection: the gaussdb-go driver, DSN building,
// and a thread-safe "current connection" the tools query against. GaussDB uses
// its OWN driver (HuaweiCloudDeveloper/gaussdb-go, registered as "gaussdb"),
// NOT the pg/pgx driver — its SCRAM-SHA256 auth is incompatible with stock pgx.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/HuaweiCloudDeveloper/gaussdb-go/stdlib" // registers "gaussdb"
)

// Target describes a connection target. Mirrors the fields opendb resolves,
// reduced to what the DSN needs.
type Target struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
	SSLMode  string `json:"sslmode"` // default "disable"
}

// Conn holds the active *sql.DB plus the target label for display.
type Conn struct {
	mu     sync.RWMutex
	db     *sql.DB
	label  string
	dbName string
}

// New returns an empty Conn (no active connection yet).
func New() *Conn { return &Conn{} }

// buildDSN builds the gaussdb keyword/value DSN (not URL form, because the
// password may contain '@' or '/'). GaussDB uses `database=` (not `dbname=`)
// and defaults to simple_protocol to avoid xid-type codec mismatches on
// catalog views — both faithful to opendb's gaussdb driver.
func buildDSN(t Target) string {
	database := t.Database
	if database == "" {
		database = "postgres"
	}
	sslmode := t.SSLMode
	if sslmode == "" {
		sslmode = "disable"
	}
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s database=%s sslmode=%s "+
			"application_name=codexgo default_query_exec_mode=simple_protocol connect_timeout=15",
		t.Host, t.Port, t.User, t.Password, database, sslmode,
	)
}

// Connect opens (and pings) a connection to target, replacing any existing one.
func (c *Conn) Connect(ctx context.Context, t Target, label string) error {
	if t.Host == "" || t.Port <= 0 || t.Port > 65535 || t.User == "" {
		return fmt.Errorf("invalid connection target: host/port/user required (port 1-65535)")
	}
	handle, err := sql.Open("gaussdb", buildDSN(t))
	if err != nil {
		return fmt.Errorf("open gaussdb: %w", err)
	}
	handle.SetConnMaxLifetime(30 * time.Minute)
	// Single connection so a session-level SET (search_path) persists across the
	// serial tool queries — codexgo issues DB tool calls one at a time.
	handle.SetMaxOpenConns(1)
	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := handle.PingContext(pingCtx); err != nil {
		_ = handle.Close()
		return fmt.Errorf("connect to %s: %w", label, err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db != nil {
		_ = c.db.Close()
	}
	c.db = handle
	c.label = label
	c.dbName = t.Database
	return nil
}

// Label returns the current connection label, or "" when not connected.
func (c *Conn) Label() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.label
}

// IsConnected reports whether a connection is active.
func (c *Conn) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.db != nil
}

// QueryResult is the structured result of a read query: column names + rows of
// stringified cells. Returning STRUCTURED data (not pre-rendered text) is the
// key optimization over opendb — codexgo renders the UI from this.
type QueryResult struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

// Query runs a read query against the active connection.
func (c *Conn) Query(ctx context.Context, query string, args ...any) (*QueryResult, error) {
	c.mu.RLock()
	handle := c.db
	c.mu.RUnlock()
	if handle == nil {
		return nil, fmt.Errorf("no active database connection — connect first")
	}
	rows, err := handle.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read columns: %w", err)
	}
	out := &QueryResult{Columns: cols}
	for rows.Next() {
		cells := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		strCells := make([]string, len(cols))
		for i, v := range cells {
			strCells[i] = cellToString(v)
		}
		out.Rows = append(out.Rows, strCells)
	}
	return out, rows.Err()
}

// SetSearchPath pins the session search_path so unqualified table names in a SQL
// resolve to the intended schema — making EXPLAIN agree with the catalog
// evidence the tuner collects (resolves cross-schema same-name ambiguity, e.g.
// public.orders vs sqltune_demo.orders). schema must be a bare identifier.
func (c *Conn) SetSearchPath(ctx context.Context, schema string) error {
	c.mu.RLock()
	handle := c.db
	c.mu.RUnlock()
	if handle == nil {
		return fmt.Errorf("no active database connection")
	}
	q := `"` + strings.ReplaceAll(schema, `"`, `""`) + `"`
	if _, err := handle.ExecContext(ctx, "SET search_path TO "+q+", public"); err != nil {
		return fmt.Errorf("set search_path: %w", err)
	}
	return nil
}

// QueryScalar runs a query expected to return a single value.
func (c *Conn) QueryScalar(ctx context.Context, query string, args ...any) (string, error) {
	res, err := c.Query(ctx, query, args...)
	if err != nil {
		return "", err
	}
	if len(res.Rows) == 0 || len(res.Rows[0]) == 0 {
		return "", nil
	}
	return res.Rows[0][0], nil
}

// cellToString normalizes a scanned cell into a display string.
func cellToString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	case string:
		return x
	case time.Time:
		return x.Format(time.RFC3339)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", x))
	}
}

// Close closes the active connection.
func (c *Conn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db != nil {
		_ = c.db.Close()
		c.db = nil
		c.label = ""
	}
}
