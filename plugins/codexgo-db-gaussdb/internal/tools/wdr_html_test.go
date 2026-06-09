package tools

import (
	"strings"
	"testing"
)

func TestParseWDRHTML(t *testing.T) {
	html := `<h3 class="wdr" id="x" onclick="msg()">-Database Stat</h3>
<table border="0" class="tdiff" summary="s"><tr>
<th class="wdrbg">DB Name</th><th class="wdrbg">Backends</th><th class="wdrbg">Xact Commit</th></tr>
<td class="wdrnc">postgres</td><td class="wdrnc">23</td><td class="wdrnc">4311</td></tr>
<td class="wdrc">omm</td><td class="wdrc">12</td><td class="wdrc">2621</td></tr>
</table>`
	secs := parseWDRHTML(html)
	if len(secs) != 1 {
		t.Fatalf("want 1 section, got %d", len(secs))
	}
	s := secs[0]
	if s.Title != "Database Stat" {
		t.Errorf("title = %q (leading +/- not stripped?)", s.Title)
	}
	if len(s.Columns) != 3 || s.Columns[0] != "DB Name" {
		t.Errorf("columns = %v", s.Columns)
	}
	if len(s.Rows) != 2 || s.Rows[0][0] != "postgres" || s.Rows[1][2] != "2621" {
		t.Errorf("rows = %v", s.Rows)
	}
	// cleanHTML strips tags + entities
	if got := cleanHTML(`<td>a &amp; b<br/>c</td>`); got != "a & b c" {
		t.Errorf("cleanHTML = %q", got)
	}
	// wide table → key:value block render; narrow → table
	out := renderWDRSections(secs)
	if !strings.Contains(out, "## Database Stat") {
		t.Errorf("missing section heading:\n%s", out)
	}
}
