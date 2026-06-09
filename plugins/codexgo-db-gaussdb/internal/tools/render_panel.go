package tools

import (
	"fmt"
	"sort"
	"strings"
)

// Shared rendering helpers for the monitoring tools (sessions / locks / longtx /
// …). All width-sensitive output goes through the runewidth-aligned primitives
// in render_ascii.go; the helpers here add severity marks (free-form lines
// only, never inside an aligned cell) and the CJK-safe ASCII blocking tree.

// sevMark returns a severity glyph for FREE-FORM lines (headings, notes). It must
// NOT be placed inside an aligned asciiTable cell — emoji width varies by
// terminal and would break column alignment (see render_ascii.go).
func sevMark(sev string) string {
	switch sev {
	case statusFail:
		return "🔴"
	case statusWarn:
		return "⚠️"
	default:
		return ""
	}
}

// kv is one "key : value" pair for a panel block.
type kv struct{ K, V string }

// kvBlock renders aligned "key : value" lines (key padded to the widest key's
// display width) — the panel style for the memory / WAL / replication tools.
func kvBlock(items []kv) string {
	kw := 0
	for _, it := range items {
		if w := dispWidth(it.K); w > kw {
			kw = w
		}
	}
	var b strings.Builder
	for _, it := range items {
		b.WriteString(padRight(it.K, kw) + " : " + it.V + "\n")
	}
	return b.String()
}

// humanSecs formats a duration in seconds as 0.5s / 12s / 5m / 1.2h.
func humanSecs(sec float64) string {
	switch {
	case sec <= 0:
		return "-"
	case sec < 1:
		return fmt.Sprintf("%.1fs", sec)
	case sec < 60:
		return fmt.Sprintf("%.0fs", sec)
	case sec < 3600:
		return fmt.Sprintf("%.0fm", sec/60)
	default:
		return fmt.Sprintf("%.1fh", sec/3600)
	}
}

// blockNode is one session in a lock blocking chain.
type blockNode struct {
	PID, User, Query, WaitEvent string
	Children                    []*blockNode
}

// buildBlockTree turns blocked→blocker pair rows into roots (the ultimate
// blockers, themselves un-blocked) + the count of distinct blocked sessions.
// Row layout: [blocked_pid, blocked_user, blocked_query, blocker_pid,
// blocker_user, blocker_query, wait_type, wait_event].
func buildBlockTree(rows [][]string) ([]*blockNode, int) {
	nodes := map[string]*blockNode{}
	blocked := map[string]bool{}
	get := func(pid, user, query string) *blockNode {
		n := nodes[pid]
		if n == nil {
			n = &blockNode{PID: pid, User: user, Query: query}
			nodes[pid] = n
		}
		return n
	}
	for _, r := range rows {
		if len(r) < 8 {
			continue
		}
		bd := get(r[0], r[1], r[2])
		bd.WaitEvent = r[7]
		br := get(r[3], r[4], r[5])
		br.Children = append(br.Children, bd)
		blocked[r[0]] = true
	}
	var roots []*blockNode
	for pid, n := range nodes {
		if !blocked[pid] {
			roots = append(roots, n)
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].PID < roots[j].PID })
	// Pure cycle (deadlock): every node is blocked, so there is no natural root.
	// Pick the lowest-pid node as the entry so the cycle still renders — the
	// seen-set in renderBlockTree marks where it loops back.
	if len(roots) == 0 && len(nodes) > 0 {
		var entry *blockNode
		for _, n := range nodes {
			if entry == nil || n.PID < entry.PID {
				entry = n
			}
		}
		roots = []*blockNode{entry}
	}
	return roots, len(blocked)
}

// renderBlockTree renders the chains as a CJK-safe ASCII tree (pure "+- "
// connectors, NEVER box-drawing — those are East-Asian-Ambiguous = 2 cells under
// a CJK locale and would misalign). A global seen-set prevents infinite loops on
// a lock cycle (deadlock).
func renderBlockTree(roots []*blockNode) string {
	var b strings.Builder
	seen := map[string]bool{}
	var walk func(n *blockNode, depth int, isBlocked bool)
	walk = func(n *blockNode, depth int, isBlocked bool) {
		indent := strings.Repeat("   ", depth)
		conn := ""
		if depth > 0 {
			conn = "+- "
		}
		line := indent + conn + "pid " + n.PID
		if n.User != "" && n.User != "-" {
			line += " (" + n.User + ")"
		}
		if isBlocked && n.WaitEvent != "" && n.WaitEvent != "-" {
			line += " 等待:" + n.WaitEvent
		}
		if q := strings.TrimSpace(n.Query); q != "" {
			line += " — " + truncDisp(q, 70)
		}
		b.WriteString(line + "\n")
		if seen[n.PID] {
			b.WriteString(indent + "   (循环引用,略)\n")
			return
		}
		seen[n.PID] = true
		for _, c := range n.Children {
			walk(c, depth+1, true)
		}
	}
	for _, r := range roots {
		walk(r, 0, false)
	}
	return b.String()
}
