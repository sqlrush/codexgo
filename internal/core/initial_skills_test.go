package core

// Tests for the skills_instructions injection into the initial context: the
// first developer message bundles the permissions part plus (when a renderer
// supplies one) the <skills_instructions> part, mirroring codex's
// build_initial_context developer-section assembly.

import (
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// TestInitialContextWithSkillsInstructions asserts the developer message gains
// the skills part as a SECOND content item of the SAME message.
func TestInitialContextWithSkillsInstructions(t *testing.T) {
	cfg := SessionConfiguration{Cwd: t.TempDir()}
	skills := "<skills_instructions>\n## Skills\nbody\n</skills_instructions>"

	items := buildSessionInitialContext(cfg, &skills)

	if len(items) != 2 {
		t.Fatalf("items = %d, want 2 (developer + user)", len(items))
	}
	dev := items[0]
	if dev.Role != "developer" {
		t.Fatalf("items[0].Role = %q, want developer", dev.Role)
	}
	if len(dev.Content) != 2 {
		t.Fatalf("developer content parts = %d, want 2", len(dev.Content))
	}
	if !strings.HasPrefix(dev.Content[0].Text, "<permissions instructions>") {
		t.Errorf("part[0] = %.60q, want permissions instructions", dev.Content[0].Text)
	}
	if dev.Content[1].Text != skills {
		t.Errorf("part[1] = %.60q, want the skills text", dev.Content[1].Text)
	}
	for _, part := range dev.Content {
		if part.Type != protocol.ContentItemKindInputText {
			t.Errorf("content type = %v, want input_text", part.Type)
		}
	}
}

// TestInitialContextWithoutSkillsInstructions keeps the single-part developer
// message when no skills render.
func TestInitialContextWithoutSkillsInstructions(t *testing.T) {
	cfg := SessionConfiguration{Cwd: t.TempDir()}

	items := buildSessionInitialContext(cfg, nil)

	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if got := len(items[0].Content); got != 1 {
		t.Fatalf("developer content parts = %d, want 1", got)
	}
}
