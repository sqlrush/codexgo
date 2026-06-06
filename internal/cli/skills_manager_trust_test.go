package cli

// Tests for the project-trust gate on project-layer skill loading: project
// `.codex/skills` roots must load only when the cwd's project is trusted,
// mirroring the Rust loader's git-trust ProjectTrustContext gate.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// writeProjectSkill creates a project `.codex/skills/<name>/SKILL.md` under root
// with a minimal valid front-matter so the loader picks it up.
func writeProjectSkill(t *testing.T, root, name, description string) {
	t.Helper()
	dir := filepath.Join(root, ".codex", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

// newProjectWithSkill creates a git project root containing one project-layer
// skill and returns its path.
func newProjectWithSkill(t *testing.T, skillName, description string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	writeProjectSkill(t, root, skillName, description)
	return root
}

// TestProjectSkillsLoadOnlyWhenTrusted verifies that the same project's
// `.codex/skills` are rendered when the trust gate reports trusted and omitted
// when it reports untrusted.
func TestProjectSkillsLoadOnlyWhenTrusted(t *testing.T) {
	home := t.TempDir()
	root := newProjectWithSkill(t, "project-helper", "A project-local helper skill.")

	// Untrusted: bundled disabled + untrusted gate => no project skills render.
	untrusted, err := newAssemblySkillsManagerWithTrust(home, false /* bundled */, func(abspath.AbsolutePathBuf) bool { return false })
	if err != nil {
		t.Fatalf("new untrusted manager: %v", err)
	}
	if text, ok := untrusted.RenderInitialSkillsInstructions(context.Background(), root, nil); ok {
		t.Errorf("expected no project skills when untrusted, got %.120q", text)
	}

	// Trusted: bundled disabled but trusted gate => project skills render.
	trusted, err := newAssemblySkillsManagerWithTrust(home, false /* bundled */, func(abspath.AbsolutePathBuf) bool { return true })
	if err != nil {
		t.Fatalf("new trusted manager: %v", err)
	}
	text, ok := trusted.RenderInitialSkillsInstructions(context.Background(), root, nil)
	if !ok {
		t.Fatal("expected project skills to render when trusted")
	}
	if !strings.Contains(text, "- project-helper: ") {
		t.Errorf("trusted render missing project skill:\n%.400s", text)
	}
}

// TestProjectSkillsNilGateKeepsProjectLayerOff verifies the conservative default:
// with no trust gate, project skills never load (the headless default before any
// trust resolution).
func TestProjectSkillsNilGateKeepsProjectLayerOff(t *testing.T) {
	home := t.TempDir()
	root := newProjectWithSkill(t, "project-helper", "A project-local helper skill.")

	manager, err := newAssemblySkillsManagerWithTrust(home, false /* bundled */, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if text, ok := manager.RenderInitialSkillsInstructions(context.Background(), root, nil); ok {
		t.Errorf("expected no project skills with nil gate, got %.120q", text)
	}
}

// TestProjectTrustGateFromConfig verifies the end-to-end wiring through the
// config trust resolver: a `[projects]` trusted entry enables project skills,
// while an untrusted/absent entry keeps them off — using the real
// config.IsProjectTrusted gate the assembly builds.
func TestProjectTrustGateFromConfig(t *testing.T) {
	home := t.TempDir()
	root := newProjectWithSkill(t, "project-helper", "A project-local helper skill.")

	rootPath, err := abspath.FromAbsolutePathChecked(root)
	if err != nil {
		t.Fatalf("abspath root: %v", err)
	}
	canonicalKey := config.ProjectTrustKey(rootPath)

	trustedGate := buildProjectTrustGate(loadedConfig{
		Merged: config.TomlValue(map[string]any{
			"projects": map[string]any{
				canonicalKey: map[string]any{"trust_level": "trusted"},
			},
		}),
	})
	manager, err := newAssemblySkillsManagerWithTrust(home, false /* bundled */, trustedGate)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	text, ok := manager.RenderInitialSkillsInstructions(context.Background(), root, nil)
	if !ok || !strings.Contains(text, "- project-helper: ") {
		t.Fatalf("expected trusted config to enable project skills, ok=%v text=%.200q", ok, text)
	}

	untrustedGate := buildProjectTrustGate(loadedConfig{
		Merged: config.TomlValue(map[string]any{
			"projects": map[string]any{
				canonicalKey: map[string]any{"trust_level": "untrusted"},
			},
		}),
	})
	manager2, err := newAssemblySkillsManagerWithTrust(home, false /* bundled */, untrustedGate)
	if err != nil {
		t.Fatalf("new manager2: %v", err)
	}
	if text, ok := manager2.RenderInitialSkillsInstructions(context.Background(), root, nil); ok {
		t.Errorf("expected untrusted config to keep project skills off, got %.120q", text)
	}
}
