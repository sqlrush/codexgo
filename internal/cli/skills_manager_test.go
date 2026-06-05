package cli

// Tests for the assembly skills manager: the headless host glue that installs
// the embedded system skills under CODEX_HOME and renders the
// <skills_instructions> developer section for a new thread.

import (
	"context"
	"strings"
	"testing"
)

// TestAssemblySkillsManagerRendersSystemSkills installs the embedded system
// skills into a fresh home and renders the tagged instructions block listing
// all five of them.
func TestAssemblySkillsManagerRendersSystemSkills(t *testing.T) {
	home := t.TempDir()
	manager, err := newAssemblySkillsManager(home)
	if err != nil {
		t.Fatalf("new skills manager: %v", err)
	}

	text, ok := manager.RenderInitialSkillsInstructions(context.Background(), t.TempDir(), nil)
	if !ok {
		t.Fatal("expected skills instructions to render for the embedded system skills")
	}
	if !strings.HasPrefix(text, "<skills_instructions>\n## Skills") {
		t.Errorf("text head = %.60q, want tagged ## Skills block", text)
	}
	if !strings.HasSuffix(text, "\n</skills_instructions>") {
		t.Errorf("text tail = %.60q, want closing tag", text[len(text)-60:])
	}
	for _, name := range []string{"imagegen", "openai-docs", "plugin-creator", "skill-creator", "skill-installer"} {
		if !strings.Contains(text, "- "+name+": ") {
			t.Errorf("rendered skills missing %q", name)
		}
	}
	// The system skills materialize under CODEX_HOME/skills/.system and the
	// rendered lines carry their absolute SKILL.md paths.
	if !strings.Contains(text, "skills/.system/imagegen/SKILL.md") {
		t.Errorf("rendered skills missing the .system SKILL.md path:\n%.400s", text)
	}
}

// TestAssemblySkillsManagerEmptyWhenNoSkills reports ok=false when nothing is
// eligible (bundled skills disabled and no user skills).
func TestAssemblySkillsManagerEmptyWhenNoSkills(t *testing.T) {
	home := t.TempDir()
	manager, err := newAssemblySkillsManagerWithBundled(home, false)
	if err != nil {
		t.Fatalf("new skills manager: %v", err)
	}
	if text, ok := manager.RenderInitialSkillsInstructions(context.Background(), t.TempDir(), nil); ok {
		t.Errorf("expected no skills instructions, got %.80q", text)
	}
}
