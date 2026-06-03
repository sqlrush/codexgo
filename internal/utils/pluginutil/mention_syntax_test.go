package pluginutil

import "testing"

func TestMentionSigils(t *testing.T) {
	t.Parallel()

	if ToolMentionSigil != '$' {
		t.Fatalf("ToolMentionSigil = %q, want %q", ToolMentionSigil, '$')
	}
	if PluginTextMentionSigil != '@' {
		t.Fatalf("PluginTextMentionSigil = %q, want %q", PluginTextMentionSigil, '@')
	}
}
