package memories

import (
	"encoding/json"
	"fmt"
)

// SearchMatchModeKind discriminates the SearchMatchMode variants.
type SearchMatchModeKind string

const (
	// MatchAny matches a line containing any query.
	MatchAny SearchMatchModeKind = "any"
	// MatchAllOnSameLine matches a line containing all queries.
	MatchAllOnSameLine SearchMatchModeKind = "all_on_same_line"
	// MatchAllWithinLines matches all queries within a window of lines.
	MatchAllWithinLines SearchMatchModeKind = "all_within_lines"
)

// SearchMatchMode is an internally-tagged enum (serde tag = "type", snake_case)
// describing how query substrings must co-occur. It mirrors SearchMatchMode. The
// AllWithinLines variant carries a positive LineCount.
type SearchMatchMode struct {
	Kind SearchMatchModeKind
	// LineCount is meaningful only when Kind == MatchAllWithinLines.
	LineCount int
}

// AnyMode returns the Any match mode.
func AnyMode() SearchMatchMode { return SearchMatchMode{Kind: MatchAny} }

// AllOnSameLineMode returns the AllOnSameLine match mode.
func AllOnSameLineMode() SearchMatchMode { return SearchMatchMode{Kind: MatchAllOnSameLine} }

// AllWithinLinesMode returns the AllWithinLines match mode with the given window.
func AllWithinLinesMode(lineCount int) SearchMatchMode {
	return SearchMatchMode{Kind: MatchAllWithinLines, LineCount: lineCount}
}

// MarshalJSON emits the internally-tagged representation: {"type":"any"},
// {"type":"all_on_same_line"}, or {"type":"all_within_lines","line_count":N}.
func (m SearchMatchMode) MarshalJSON() ([]byte, error) {
	switch m.Kind {
	case MatchAny, MatchAllOnSameLine:
		return json.Marshal(struct {
			Type SearchMatchModeKind `json:"type"`
		}{Type: m.Kind})
	case MatchAllWithinLines:
		return json.Marshal(struct {
			Type      SearchMatchModeKind `json:"type"`
			LineCount int                 `json:"line_count"`
		}{Type: m.Kind, LineCount: m.LineCount})
	default:
		return nil, fmt.Errorf("memories: unknown search match mode %q", m.Kind)
	}
}

// UnmarshalJSON decodes the internally-tagged representation.
func (m *SearchMatchMode) UnmarshalJSON(data []byte) error {
	var head struct {
		Type      SearchMatchModeKind `json:"type"`
		LineCount *int                `json:"line_count"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return fmt.Errorf("memories: decode search match mode: %w", err)
	}
	switch head.Type {
	case MatchAny:
		*m = SearchMatchMode{Kind: MatchAny}
	case MatchAllOnSameLine:
		*m = SearchMatchMode{Kind: MatchAllOnSameLine}
	case MatchAllWithinLines:
		lineCount := 0
		if head.LineCount != nil {
			lineCount = *head.LineCount
		}
		*m = SearchMatchMode{Kind: MatchAllWithinLines, LineCount: lineCount}
	default:
		return fmt.Errorf("memories: unknown search match mode %q", head.Type)
	}
	return nil
}
