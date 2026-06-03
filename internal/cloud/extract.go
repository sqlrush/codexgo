package cloud

import "encoding/json"

// extractAssistantMessagesFromBody pulls assistant text out of the raw task
// details body's current_assistant_turn.worklog.messages. It mirrors the Rust
// `extract_assistant_messages_from_body`.
func extractAssistantMessagesFromBody(body string) []string {
	var full map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &full) != nil {
		return nil
	}
	turn := objectField(full, "current_assistant_turn")
	if turn == nil {
		return nil
	}
	worklog := objectField(turn, "worklog")
	if worklog == nil {
		return nil
	}
	arr := arrayField(worklog, "messages")
	if arr == nil {
		return nil
	}

	var msgs []string
	for _, raw := range arr {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		author := objectField(m, "author")
		if author == nil {
			continue
		}
		if strField(author, "role") != "assistant" {
			continue
		}
		content := objectField(m, "content")
		if content == nil {
			continue
		}
		parts := arrayField(content, "parts")
		for _, partRaw := range parts {
			var s string
			if json.Unmarshal(partRaw, &s) == nil {
				if s != "" {
					msgs = append(msgs, s)
				}
				continue
			}
			var obj map[string]json.RawMessage
			if json.Unmarshal(partRaw, &obj) != nil {
				continue
			}
			if strField(obj, "content_type") == "text" {
				if txt := strField(obj, "text"); txt != "" {
					msgs = append(msgs, txt)
				}
			}
		}
	}
	return msgs
}

// turnAttemptFromMap builds a TurnAttempt from a sibling-turn map. It mirrors
// the Rust `turn_attempt_from_map` (returns nil when there is no id).
func turnAttemptFromMap(turn map[string]json.RawMessage) *TurnAttempt {
	turnID := strField(turn, "id")
	if turnID == "" {
		// Distinguish "no id field" from "empty id". The Rust code returns
		// None only when id is absent or not a string; an empty string id is
		// not produced by the backend, so treat missing as nil.
		if _, ok := turn["id"]; !ok {
			return nil
		}
	}
	var placement *int64
	if raw, ok := turn["attempt_placement"]; ok {
		var v int64
		if json.Unmarshal(raw, &v) == nil {
			placement = &v
		}
	}
	createdAt := parseTimestampValue(turn["created_at"])
	status := attemptStatusFromStr(strField(turn, "turn_status"))
	diff := extractDiffFromTurn(turn)
	messages := extractAssistantMessagesFromTurn(turn)
	return &TurnAttempt{
		TurnID:           turnID,
		AttemptPlacement: placement,
		CreatedAt:        createdAt,
		Status:           status,
		Diff:             diff,
		Messages:         messages,
	}
}

// extractDiffFromTurn pulls a unified diff from a turn's output_items. It mirrors
// the Rust `extract_diff_from_turn`.
func extractDiffFromTurn(turn map[string]json.RawMessage) *string {
	items := arrayField(turn, "output_items")
	for _, raw := range items {
		var item map[string]json.RawMessage
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		switch strField(item, "type") {
		case "output_diff":
			if diff := strField(item, "diff"); diff != "" {
				return &diff
			}
		case "pr":
			od := objectField(item, "output_diff")
			if od != nil {
				if diff := strField(od, "diff"); diff != "" {
					return &diff
				}
			}
		}
	}
	return nil
}

// extractAssistantMessagesFromTurn pulls assistant text from a turn's
// output_items. It mirrors the Rust `extract_assistant_messages_from_turn`.
func extractAssistantMessagesFromTurn(turn map[string]json.RawMessage) []string {
	var msgs []string
	items := arrayField(turn, "output_items")
	for _, raw := range items {
		var item map[string]json.RawMessage
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		if strField(item, "type") != "message" {
			continue
		}
		content := arrayField(item, "content")
		for _, partRaw := range content {
			var part map[string]json.RawMessage
			if json.Unmarshal(partRaw, &part) != nil {
				continue
			}
			if strField(part, "content_type") == "text" {
				if txt := strField(part, "text"); txt != "" {
					msgs = append(msgs, txt)
				}
			}
		}
	}
	return msgs
}

func objectField(m map[string]json.RawMessage, key string) map[string]json.RawMessage {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	return obj
}

func arrayField(m map[string]json.RawMessage, key string) []json.RawMessage {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil {
		return nil
	}
	return arr
}

func strField(m map[string]json.RawMessage, key string) string {
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}
