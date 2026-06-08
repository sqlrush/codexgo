package tools

import (
	"regexp"
	"strings"
)

// Bind-variable backfill (sqltune parity #2): a normalized SQL from
// dbe_perf.statement carries placeholders (?, $N, :N) and cannot be EXPLAINed.
// substituteBinds replaces them with type-aware sample literals using
// deterministic, context-based rules (ported from opendb's
// placeholder_substituter) so EXPLAIN succeeds. The substituted values are only
// for plan shape — never treat them as real business values.

// BindFill describes one placeholder replacement.
type BindFill struct {
	Original string `json:"original"` // the placeholder token (?, $1, :2)
	Context  string `json:"context"`  // trimmed left context (column/operator)
	Value    string `json:"value"`    // the substituted literal
	Source   string `json:"source"`   // rule | default
}

type phPos struct{ start, end int }

// findPlaceholderPositions locates ?, $N, :N placeholders outside string
// literals and comments.
func findPlaceholderPositions(sql string) []phPos {
	var out []phPos
	n := len(sql)
	for i := 0; i < n; {
		c := sql[i]
		switch {
		case c == '\'':
			i++
			for i < n {
				if sql[i] == '\'' {
					if i+1 < n && sql[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
		case c == '"':
			i++
			for i < n && sql[i] != '"' {
				i++
			}
			if i < n {
				i++
			}
		case c == '-' && i+1 < n && sql[i+1] == '-':
			for i < n && sql[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && sql[i+1] == '*':
			i += 2
			for i+1 < n && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			i += 2
		case c == '?':
			out = append(out, phPos{i, i + 1})
			i++
		case (c == '$' || c == ':') && i+1 < n && sql[i+1] >= '0' && sql[i+1] <= '9':
			j := i + 1
			for j < n && sql[j] >= '0' && sql[j] <= '9' {
				j++
			}
			out = append(out, phPos{i, j})
			i = j
		default:
			i++
		}
	}
	return out
}

// substituteBinds returns the SQL with placeholders replaced and the list of
// substitutions applied. When there are no placeholders it returns the input.
func substituteBinds(sql string) (string, []BindFill) {
	pos := findPlaceholderPositions(sql)
	if len(pos) == 0 {
		return sql, nil
	}
	fills := make([]BindFill, len(pos))
	for i, p := range pos {
		left := extractLeftContext(sql, p.start, 60)
		val, src := chooseBindValue(left, fills, i)
		fills[i] = BindFill{
			Original: sql[p.start:p.end],
			Context:  strings.TrimSpace(left),
			Value:    val,
			Source:   src,
		}
	}
	out := []byte(sql)
	for i := len(pos) - 1; i >= 0; i-- {
		out = append(out[:pos[i].start], append([]byte(fills[i].Value), out[pos[i].end:]...)...)
	}
	return string(out), fills
}

func extractLeftContext(sql string, start, width int) string {
	from := start - width
	if from < 0 {
		from = 0
	}
	return sql[from:start]
}

var toCharFormatRE = regexp.MustCompile(`(?i)to_char\s*\(\s*[a-z_][a-z0-9_.]*\s*,\s*$`)

// chooseBindValue picks a substitution literal from the left context. Mirrors
// opendb's chooseSubstitution rules, plus the to_char-format followup (a value
// compared against a 'YYYY...' format string is a date).
func chooseBindValue(leftContext string, prev []BindFill, idx int) (string, string) {
	lower := strings.ToLower(leftContext)
	trimmed := strings.TrimRight(lower, " \t\n")

	switch {
	case strings.HasSuffix(trimmed, "limit"):
		return "100", "rule"
	case strings.HasSuffix(trimmed, "offset"):
		return "0", "rule"
	case strings.HasSuffix(trimmed, "interval"):
		return "'1 day'", "rule"
	case endsWithKeyword(trimmed, "like"), endsWithKeyword(trimmed, "ilike"):
		return "'%test%'", "rule"
	case toCharFormatRE.MatchString(leftContext):
		return "'YYYY-MM-DD'", "rule"
	case endsWithOp(trimmed, "="), endsWithOp(trimmed, "<>"), endsWithOp(trimmed, "!="):
		if looksLikeIntColumn(trimmed) {
			return "1", "rule"
		}
		if looksLikeDateColumn(trimmed) {
			return "'2024-01-01'", "rule"
		}
		// A value compared to the result of a to_char(date,'YYYY-...') is a date.
		if idx > 0 && strings.HasPrefix(prev[idx-1].Value, "'YYYY") {
			return "'2024-01-15'", "rule"
		}
		return "'test'", "rule"
	case endsWithOp(trimmed, "<="), endsWithOp(trimmed, ">="), endsWithOp(trimmed, "<"), endsWithOp(trimmed, ">"):
		if looksLikeDateColumn(trimmed) {
			return "'2024-01-01'", "rule"
		}
		return "50", "rule"
	case strings.Contains(trimmed, "in (") || strings.HasSuffix(trimmed, "in("):
		if looksLikeIntColumn(trimmed) {
			return "1", "rule"
		}
		return "'test'", "rule"
	case endsWithKeyword(trimmed, "between"), endsWithKeyword(trimmed, "and"):
		return "1", "rule"
	default:
		return "1", "default"
	}
}

func endsWithKeyword(s, kw string) bool {
	if !strings.HasSuffix(s, kw) {
		return false
	}
	if len(s) == len(kw) {
		return true
	}
	prev := s[len(s)-len(kw)-1]
	return prev == ' ' || prev == '\t' || prev == '\n' || prev == '(' || prev == ','
}

func endsWithOp(s, op string) bool {
	if !strings.HasSuffix(s, op) {
		return false
	}
	// A bare "=" must not match the two-char operators <=, >=, !=, <>.
	if op == "=" && len(s) >= 2 {
		if p := s[len(s)-2]; p == '<' || p == '>' || p == '!' {
			return false
		}
	}
	return true
}

var intColRE = regexp.MustCompile(`(?i)(^|[._])(id|count|cnt|num|no|qty|quantity|price|amount|age|seq|year|level|size)$`)
var dateColRE = regexp.MustCompile(`(?i)(date|time|_at|_on|timestamp|created|updated|modified)`)

// lastColumnToken returns the identifier just before the trailing operator.
func lastColumnToken(ctx string) string {
	tokens := strings.Fields(ctx)
	for i := len(tokens) - 1; i >= 0; i-- {
		t := strings.TrimRight(tokens[i], "=<>!,()")
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		// strip table qualifier: o.order_date -> order_date
		if dot := strings.LastIndexByte(t, '.'); dot >= 0 {
			t = t[dot+1:]
		}
		return t
	}
	return ""
}

func looksLikeIntColumn(ctx string) bool {
	col := lastColumnToken(ctx)
	return col != "" && intColRE.MatchString(col)
}

func looksLikeDateColumn(ctx string) bool {
	col := lastColumnToken(ctx)
	return col != "" && dateColRE.MatchString(col)
}
