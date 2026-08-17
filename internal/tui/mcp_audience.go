package tui

import (
	"encoding/json"
	"strings"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// splitUserAudience separates a tool result's user-addressed text by intent,
// using the standard MCP annotations.audience:
//
//	autoShow — blocks addressed to BOTH "user" and "assistant". These are the
//	  deliberately-invoked report tools (health / sqltune / wdranalyze evidence,
//	  which a user asks for explicitly and which don't flood). They render
//	  immediately and are never gated behind @show, so a result never silently
//	  vanishes because the model forgot to declare it.
//
//	gated — blocks addressed to "user" only. These are the per-call monitoring
//	  tables (space / sessions / wait / …) the model may fan out over while
//	  exploring; they are stashed and rendered only if the model declares them
//	  via "@show:" (the relevance gate), so the transcript isn't flooded.
//
// raw is the externally-tagged Result<CallToolResult,String> payload
// ({"Ok":{...}} on success); anything else yields "","".
func splitUserAudience(raw json.RawMessage) (autoShow, gated string) {
	if len(raw) == 0 {
		return "", ""
	}
	var tagged struct {
		Ok *protocol.CallToolResult `json:"Ok"`
	}
	if err := json.Unmarshal(raw, &tagged); err != nil || tagged.Ok == nil {
		return "", ""
	}
	var ab, gb strings.Builder
	appendLine := func(b *strings.Builder, s string) {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(s)
	}
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
		if audienceContains(item.Annotations.Audience, "assistant") {
			appendLine(&ab, item.Text)
		} else {
			appendLine(&gb, item.Text)
		}
	}
	return ab.String(), gb.String()
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
