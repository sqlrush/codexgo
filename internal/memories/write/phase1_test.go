package write

import (
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func inputText(text string) protocol.ContentItem {
	return protocol.ContentItem{Type: protocol.ContentItemKindInputText, Text: text}
}

func TestClassifiesMemoryExcludedFragments(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"# AGENTS.md instructions for /tmp\n\n<INSTRUCTIONS>\nbody\n</INSTRUCTIONS>", true},
		{"<skill>\n<name>demo</name>\n<path>skills/demo/SKILL.md</path>\nbody\n</skill>", true},
		{"<environment_context>\n<cwd>/tmp</cwd>\n</environment_context>", false},
		{"<subagent_notification>{\"agent_id\":\"a\",\"status\":\"completed\"}</subagent_notification>", false},
	}
	for _, tc := range cases {
		got := isMemoryExcludedContextualUserFragment(inputText(tc.text))
		if got != tc.want {
			t.Errorf("isMemoryExcludedContextualUserFragment(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestMatchesMarkedFragmentCaseInsensitiveAndTrimmed(t *testing.T) {
	text := "   <SKILL>\nbody\n</Skill>  \n"
	if !matchesMarkedFragment(text, "<skill>", "</skill>") {
		t.Fatal("expected case-insensitive, whitespace-trimmed match")
	}
	if matchesMarkedFragment("<skill>", "<skill>", "</skill>") {
		t.Fatal("text shorter than end marker should not match")
	}
}

func TestSanitizeExcludesDeveloperMessages(t *testing.T) {
	developer := protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "developer",
		Content: []protocol.ContentItem{inputText("policy")},
	}
	if _, ok := SanitizeResponseItemForMemories(developer); ok {
		t.Fatal("developer-role messages must be excluded")
	}
}

func TestSanitizeKeepsAssistantMessageVerbatim(t *testing.T) {
	assistant := protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "assistant",
		Content: []protocol.ContentItem{{Type: protocol.ContentItemKindOutputText, Text: "answer"}},
	}
	got, ok := SanitizeResponseItemForMemories(assistant)
	if !ok {
		t.Fatal("assistant messages must be kept")
	}
	if got.Role != "assistant" || len(got.Content) != 1 {
		t.Fatalf("assistant message altered: %#v", got)
	}
}

func TestSanitizeStripsContextualUserFragments(t *testing.T) {
	user := protocol.ResponseItem{
		Type: protocol.ResponseItemKindMessage,
		Role: "user",
		Content: []protocol.ContentItem{
			inputText("<skill>\nbody\n</skill>"),
			inputText("real question"),
		},
	}
	got, ok := SanitizeResponseItemForMemories(user)
	if !ok {
		t.Fatal("user message with real content must be kept")
	}
	if len(got.Content) != 1 || got.Content[0].Text != "real question" {
		t.Fatalf("contextual fragment not stripped: %#v", got.Content)
	}
}

func TestSanitizeDropsUserMessageWithOnlyContextualFragments(t *testing.T) {
	user := protocol.ResponseItem{
		Type: protocol.ResponseItemKindMessage,
		Role: "user",
		Content: []protocol.ContentItem{
			inputText("# AGENTS.md instructions for /tmp\n<INSTRUCTIONS>\nx\n</INSTRUCTIONS>"),
		},
	}
	if _, ok := SanitizeResponseItemForMemories(user); ok {
		t.Fatal("user message with only contextual fragments must be dropped")
	}
}

func TestSerializeFilteredRolloutResponseItemsUsesRedactor(t *testing.T) {
	calls := 0
	redact := func(s string) string {
		calls++
		return s + "/*redacted*/"
	}
	out, err := SerializeFilteredRolloutResponseItems(nil, redact)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if calls != 1 {
		t.Fatalf("redactor calls = %d, want 1", calls)
	}
	if out != "[]/*redacted*/" {
		t.Fatalf("out = %q", out)
	}
}
