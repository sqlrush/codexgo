package tools

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/sqlrush/codexgo-db-gaussdb/internal/db"
	"github.com/sqlrush/codexgo-db-gaussdb/internal/mcp"
)

// /users — roles/users with login, privilege and password-expiry info, for a
// quick security pass. Uses pg_roles (readable by all; a view over pg_authid),
// flagging expired passwords and superusers.

const usersSQL = `SELECT
  rolname,
  rolcanlogin,
  rolsuper,
  rolcreaterole,
  rolcreatedb,
  COALESCE(rolvaliduntil::text, '') AS valid_until,
  CASE WHEN rolvaliduntil IS NOT NULL
       THEN EXTRACT(DAY FROM rolvaliduntil - now())::int
       ELSE NULL END AS days_left
FROM pg_roles
WHERE rolname NOT LIKE 'pg_%'
ORDER BY rolvaliduntil NULLS LAST`

type userRow struct {
	Name, ValidUntil     string
	Login, Super         bool
	CreateRole, CreateDB bool
	DaysLeft             string
	Expired              bool
}

type usersData struct {
	Target   string
	Rows     []userRow
	Supers   int
	Expired  int
	Warnings []string
}

func registerUsers(s *mcp.Server, conn *db.Conn) {
	tool := mcp.Tool{
		Name:        "users",
		Description: "List GaussDB/openGauss roles/users (pg_roles) with login ability, superuser/create privileges and password expiry, ordered by expiry. Renders a table directly to the user, flagging expired passwords and superusers (a quick security pass). Optional arg pattern to filter by name. Read-only.",
		InputSchema: jsonObjSchema(map[string]any{"pattern": strProp("filter role names containing this substring")}),
	}
	s.Register(tool, func(ctx context.Context, raw json.RawMessage) (mcp.CallToolResult, error) {
		if err := ensureConn(ctx, conn); err != nil {
			return mcp.CallToolResult{}, err
		}
		var a struct {
			Pattern string `json:"pattern"`
		}
		if err := decodeArgs(raw, &a); err != nil {
			return mcp.CallToolResult{}, err
		}
		d := collectUsers(ctx, conn, strings.TrimSpace(a.Pattern))
		return mcp.CallToolResult{Content: []mcp.ContentItem{
			mcp.TextContentFor(renderUsers(d), "user"),
			mcp.TextContentFor(monDigest("用户/角色", len(d.Rows), "勿复述;可点评超级用户数量与过期账号"), "assistant"),
		}}, nil
	})
}

func collectUsers(ctx context.Context, conn *db.Conn, pattern string) *usersData {
	d := &usersData{Target: conn.Label()}
	res, err := conn.Query(ctx, usersSQL)
	if err != nil {
		d.Warnings = append(d.Warnings, "用户采集失败: "+firstLine(err.Error()))
		return d
	}
	for _, r := range res.Rows {
		if len(r) < 7 {
			continue
		}
		if pattern != "" && !strings.Contains(strings.ToLower(r[0]), strings.ToLower(pattern)) {
			continue
		}
		row := userRow{
			Name: r[0], Login: truthy(r[1]), Super: truthy(r[2]),
			CreateRole: truthy(r[3]), CreateDB: truthy(r[4]),
			ValidUntil: r[5], DaysLeft: r[6],
		}
		if row.Super {
			d.Supers++
		}
		if dl := strings.TrimSpace(r[6]); dl != "" && atof(dl) < 0 {
			row.Expired = true
			d.Expired++
		}
		d.Rows = append(d.Rows, row)
	}
	return d
}

func renderUsers(d *usersData) string {
	var b strings.Builder
	b.WriteString("# 👤 用户 / 角色 · " + d.Target + "\n\n")
	if len(d.Rows) == 0 {
		b.WriteString("无匹配角色。\n")
		if len(d.Warnings) > 0 {
			b.WriteString("\n> 采集告警:" + strings.Join(d.Warnings, " · ") + "\n")
		}
		return b.String()
	}
	b.WriteString("```\n")
	cols := []tableColumn{
		{Header: "角色", Max: 24},
		{Header: "登录"},
		{Header: "超级"},
		{Header: "建角色"},
		{Header: "建库"},
		{Header: "有效期至", Max: 20},
		{Header: "剩余天", Right: true},
	}
	var rows [][]string
	for _, r := range d.Rows {
		vu := r.ValidUntil
		if vu == "" {
			vu = "永久"
		}
		dl := r.DaysLeft
		if strings.TrimSpace(dl) == "" {
			dl = "-"
		}
		if r.Expired {
			dl += "(已过期)"
		}
		rows = append(rows, []string{r.Name, yesNo(r.Login), yesNo(r.Super), yesNo(r.CreateRole), yesNo(r.CreateDB), vu, dl})
	}
	b.WriteString(asciiTable(cols, rows))
	b.WriteString("```\n\n")
	b.WriteString("> 超级用户 " + strconv.Itoa(d.Supers) + " 个")
	if d.Expired > 0 {
		b.WriteString(" · ⚠️ " + strconv.Itoa(d.Expired) + " 个密码已过期")
	}
	b.WriteString(";精简超级用户、轮换过期账号。\n")
	if len(d.Warnings) > 0 {
		b.WriteString("\n> 采集告警:" + strings.Join(d.Warnings, " · ") + "\n")
	}
	return b.String()
}

// truthy interprets a stringified boolean cell ('t'/'true'/'1').
func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "t", "true", "1", "yes":
		return true
	}
	return false
}

// yesNo is an ASCII boolean cell ("是"/"-") — emoji-free for table alignment.
func yesNo(b bool) string {
	if b {
		return "是"
	}
	return "-"
}
