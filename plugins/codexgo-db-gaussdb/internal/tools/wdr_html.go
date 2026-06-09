package tools

import (
	"regexp"
	"strings"
)

// openGauss generate_wdr_report() returns an HTML document. Dumping that raw is
// unreadable, so we parse it into sections (heading + table) and render aligned
// text: narrow tables horizontally, wide tables (e.g. 18-column Database Stat)
// as one key:value block per row. stdlib-only (regexp), tolerant of openGauss's
// slightly irregular <tr> markup (cells are regrouped by the header column count).

type wdrSection struct {
	Title   string
	Columns []string
	Rows    [][]string
}

var (
	wdrHeadRe  = regexp.MustCompile(`(?s)<h[23][^>]*>([^<]*)</h[23]>`)
	wdrTableRe = regexp.MustCompile(`(?s)<table[^>]*>(.*?)</table>`)
	wdrThRe    = regexp.MustCompile(`(?s)<th[^>]*>(.*?)</th>`)
	wdrTdRe    = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
	wdrTagRe   = regexp.MustCompile(`(?s)<[^>]*>`)
	wdrEntity  = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&nbsp;", " ", "&quot;", `"`, "&#39;", "'")
)

// parseWDRHTML extracts the heading+table sections from a WDR HTML report.
func parseWDRHTML(htmlStr string) []wdrSection {
	type mark struct {
		pos   int
		title string
	}
	var heads []mark
	for _, m := range wdrHeadRe.FindAllStringSubmatchIndex(htmlStr, -1) {
		title := strings.TrimSpace(strings.TrimLeft(cleanHTML(htmlStr[m[2]:m[3]]), "+-"))
		if title != "" {
			heads = append(heads, mark{m[0], title})
		}
	}
	titleFor := func(pos int) string {
		t := ""
		for _, h := range heads {
			if h.pos < pos {
				t = h.title
			} else {
				break
			}
		}
		return t
	}

	var secs []wdrSection
	for _, m := range wdrTableRe.FindAllStringSubmatchIndex(htmlStr, -1) {
		body := htmlStr[m[2]:m[3]]
		var cols []string
		for _, th := range wdrThRe.FindAllStringSubmatch(body, -1) {
			cols = append(cols, cleanHTML(th[1]))
		}
		if len(cols) == 0 {
			continue
		}
		var cells []string
		for _, td := range wdrTdRe.FindAllStringSubmatch(body, -1) {
			cells = append(cells, cleanHTML(td[1]))
		}
		var rows [][]string
		for i := 0; i+len(cols) <= len(cells); i += len(cols) {
			rows = append(rows, cells[i:i+len(cols)])
		}
		secs = append(secs, wdrSection{Title: titleFor(m[0]), Columns: cols, Rows: rows})
	}
	return secs
}

// cleanHTML replaces tags with a space (so <br>/block tags become separators),
// decodes the common entities, and collapses whitespace.
func cleanHTML(s string) string {
	s = wdrTagRe.ReplaceAllString(s, " ")
	s = wdrEntity.Replace(s)
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// renderWDRSections renders parsed WDR sections as readable markdown.
func renderWDRSections(secs []wdrSection) string {
	var b strings.Builder
	for _, s := range secs {
		if len(s.Rows) == 0 {
			continue
		}
		if s.Title != "" {
			b.WriteString("## " + s.Title + "\n\n")
		}
		b.WriteString(renderWDRTable(s))
	}
	return b.String()
}

func renderWDRTable(s wdrSection) string {
	var b strings.Builder
	b.WriteString("```\n")
	if len(s.Columns) <= 6 {
		// narrow → horizontal table
		cols := make([]tableColumn, len(s.Columns))
		for i, c := range s.Columns {
			cols[i] = tableColumn{Header: c, Max: 28, Right: i > 0}
		}
		b.WriteString(asciiTable(cols, s.Rows))
	} else {
		// wide → one key:value block per row (first column is the record label)
		kw := 0
		for _, c := range s.Columns[1:] {
			if w := dispWidth(c); w > kw {
				kw = w
			}
		}
		for _, row := range s.Rows {
			if len(row) > 0 {
				b.WriteString(row[0] + "\n")
			}
			for i := 1; i < len(s.Columns) && i < len(row); i++ {
				if strings.TrimSpace(row[i]) == "" {
					continue
				}
				b.WriteString("  " + padRight(s.Columns[i], kw) + " : " + row[i] + "\n")
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("```\n\n")
	return b.String()
}
