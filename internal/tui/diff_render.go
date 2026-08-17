package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// DiffRenderer renders unified diffs and structured [protocol.FileChange] maps
// into styled transcript [Line]s.
//
// It is the Go analogue of codex-rs/tui/src/diff_render.rs, reduced to the
// behavior the chat surface needs: per-line gutter signs (+/-/space), add/delete
// foreground coloring, and a per-file header. The Rust renderer additionally
// tints line backgrounds based on terminal lightness and applies syntax
// highlighting inside hunks; this port colors the gutter/text by change kind
// (a documented cosmetic deviation) which preserves the add/delete signal across
// all color depths.
type DiffRenderer struct {
	theme Theme
	add   Style
	del   Style
	ctx   Style
	hunk  Style
	head  Style
}

// NewDiffRenderer builds a diff renderer bound to a theme.
func NewDiffRenderer(theme Theme) DiffRenderer {
	return DiffRenderer{
		theme: theme,
		add:   Style{Fg: theme.Success},
		del:   Style{Fg: theme.Error},
		ctx:   Style{Fg: theme.Foreground},
		hunk:  Style{Fg: theme.Info, Bold: true},
		head:  Style{Fg: theme.Primary, Bold: true},
	}
}

// RenderUnifiedDiff renders a raw unified-diff string (as produced by `git diff`
// or [protocol.TurnDiffEvent]) into styled lines.
func (r DiffRenderer) RenderUnifiedDiff(diff string) []Line {
	if strings.TrimSpace(diff) == "" {
		return []Line{{Spans: []Span{{Text: "(no changes)", Style: Style{Fg: r.theme.Dim}}}}}
	}
	var out []Line
	for _, raw := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		out = append(out, r.diffLine(raw))
	}
	return out
}

// diffLine styles a single unified-diff line by its leading marker.
func (r DiffRenderer) diffLine(raw string) Line {
	switch {
	case strings.HasPrefix(raw, "+++") || strings.HasPrefix(raw, "---") ||
		strings.HasPrefix(raw, "diff ") || strings.HasPrefix(raw, "index "):
		return Line{Spans: []Span{{Text: raw, Style: r.head}}}
	case strings.HasPrefix(raw, "@@"):
		return Line{Spans: []Span{{Text: raw, Style: r.hunk}}}
	case strings.HasPrefix(raw, "+"):
		return Line{Spans: []Span{{Text: raw, Style: r.add}}}
	case strings.HasPrefix(raw, "-"):
		return Line{Spans: []Span{{Text: raw, Style: r.del}}}
	default:
		return Line{Spans: []Span{{Text: raw, Style: r.ctx}}}
	}
}

// RenderFileChanges renders a structured map of path -> [protocol.FileChange],
// producing a header per file followed by its change body. Paths are rendered in
// sorted order for stable output.
//
// Port of diff_render.rs create_diff_summary's per-file rendering.
func (r DiffRenderer) RenderFileChanges(changes map[string]protocol.FileChange) []Line {
	if len(changes) == 0 {
		return []Line{{Spans: []Span{{Text: "(no changes)", Style: Style{Fg: r.theme.Dim}}}}}
	}
	paths := make([]string, 0, len(changes))
	for p := range changes {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var out []Line
	for i, p := range paths {
		if i > 0 {
			out = append(out, Line{})
		}
		out = append(out, r.fileChangeBlock(p, changes[p])...)
	}
	return out
}

// fileChangeBlock renders one file's change block.
func (r DiffRenderer) fileChangeBlock(path string, change protocol.FileChange) []Line {
	var out []Line
	switch change.Kind {
	case protocol.FileChangeKindAdd:
		out = append(out, r.fileHeader("added", path, countLines(change.Content), 0))
		for _, l := range strings.Split(strings.TrimRight(change.Content, "\n"), "\n") {
			out = append(out, Line{Spans: []Span{{Text: "+" + l, Style: r.add}}})
		}
	case protocol.FileChangeKindDelete:
		out = append(out, r.fileHeader("deleted", path, 0, countLines(change.Content)))
		for _, l := range strings.Split(strings.TrimRight(change.Content, "\n"), "\n") {
			out = append(out, Line{Spans: []Span{{Text: "-" + l, Style: r.del}}})
		}
	case protocol.FileChangeKindUpdate:
		display := path
		if change.MovePath != nil && *change.MovePath != "" {
			display = fmt.Sprintf("%s -> %s", path, *change.MovePath)
		}
		add, del := countDiffStats(change.UnifiedDiff)
		out = append(out, r.fileHeader("modified", display, add, del))
		out = append(out, r.RenderUnifiedDiff(change.UnifiedDiff)...)
	default:
		out = append(out, r.fileHeader(string(change.Kind), path, 0, 0))
	}
	return out
}

// fileHeader renders a file header line: "kind path (+a -d)".
func (r DiffRenderer) fileHeader(kind, path string, added, removed int) Line {
	spans := []Span{
		{Text: kind + " ", Style: Style{Fg: r.theme.Dim}},
		{Text: path, Style: r.head},
	}
	if added > 0 || removed > 0 {
		stat := fmt.Sprintf("  (+%d -%d)", added, removed)
		spans = append(spans, Span{Text: stat, Style: Style{Fg: r.theme.Dim}})
	}
	return Line{Spans: spans}
}

// countLines counts the non-empty trailing-normalized line count of content.
func countLines(content string) int {
	if content == "" {
		return 0
	}
	return len(strings.Split(strings.TrimRight(content, "\n"), "\n"))
}

// countDiffStats counts added and removed lines in a unified diff.
func countDiffStats(diff string) (added, removed int) {
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---"):
			continue
		case strings.HasPrefix(l, "+"):
			added++
		case strings.HasPrefix(l, "-"):
			removed++
		}
	}
	return added, removed
}

// compile-time use of lipgloss to keep the import meaningful even if the theme
// fields are all nil colors.
var _ = lipgloss.NoColor{}
