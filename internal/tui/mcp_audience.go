package tui

import (
	"encoding/json"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// userAudienceMarkdown extracts the text of MCP result content blocks addressed
// to the user (standard MCP annotations.audience contains "user"), for direct
// rendering in the transcript. Returns "" when there is none — the common case —
// so ordinary tool results render nothing here and flow to the model unchanged.
//
// raw is the externally-tagged Result<CallToolResult,String> payload
// ({"Ok":{...}} on success); anything else yields "".
func userAudienceMarkdown(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var tagged struct {
		Ok *protocol.CallToolResult `json:"Ok"`
	}
	if err := json.Unmarshal(raw, &tagged); err != nil || tagged.Ok == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range tagged.Ok.Content {
		var item struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			Annotations *struct {
				Audience []string `json:"audience"`
			} `json:"annotations"`
		}
		if err := json.Unmarshal(c, &item); err != nil || item.Type != "text" {
			continue
		}
		if item.Annotations == nil || !audienceContains(item.Annotations.Audience, "user") {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(item.Text)
	}
	return b.String()
}

// audienceContains reports whether an audience list includes the given role.
func audienceContains(audience []string, role string) bool {
	for _, a := range audience {
		if a == role {
			return true
		}
	}
	return false
}
