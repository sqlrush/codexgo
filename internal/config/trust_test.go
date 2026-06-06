package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// mergedWithProjects builds a merged-config TomlValue whose `[projects]` table
// maps each path to a trust_level, matching the shape readTomlFile/MergeToml
// produce (nested map[string]any).
func mergedWithProjects(entries map[string]string) TomlValue {
	projects := make(map[string]any, len(entries))
	for path, level := range entries {
		projects[path] = map[string]any{"trust_level": level}
	}
	return TomlValue(map[string]any{"projects": projects})
}

// absPath constructs an AbsolutePathBuf for a temp path, failing the test on
// error.
func absPath(t *testing.T, path string) abspath.AbsolutePathBuf {
	t.Helper()
	p, err := abspath.FromAbsolutePathChecked(path)
	if err != nil {
		t.Fatalf("abspath %q: %v", path, err)
	}
	return p
}

// makeGitRepo creates dir with a `.git` directory marker so it reads as a
// project/repo root.
func makeGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
}

func TestIsProjectTrustedTrustedEntry(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root)
	cwd := absPath(t, root)
	canonical := canonicalString(t, root)

	merged := mergedWithProjects(map[string]string{canonical: "trusted"})
	if !IsProjectTrusted(merged, cwd, nil) {
		t.Fatalf("expected %q to be trusted", canonical)
	}
}

func TestIsProjectTrustedUntrustedEntry(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root)
	cwd := absPath(t, root)
	canonical := canonicalString(t, root)

	merged := mergedWithProjects(map[string]string{canonical: "untrusted"})
	if IsProjectTrusted(merged, cwd, nil) {
		t.Fatalf("expected %q to be untrusted", canonical)
	}

	ctx := BuildProjectTrustContext(merged, cwd, DefaultProjectRootMarkers())
	decision := ctx.DecisionForCwd()
	if !decision.IsUntrusted() {
		t.Fatalf("expected explicit untrusted decision, got %+v", decision)
	}
}

func TestIsProjectTrustedNoEntry(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root)
	cwd := absPath(t, root)

	merged := mergedWithProjects(map[string]string{"/some/other/project": "trusted"})
	if IsProjectTrusted(merged, cwd, nil) {
		t.Fatalf("expected untrusted when no entry matches cwd")
	}

	ctx := BuildProjectTrustContext(merged, cwd, DefaultProjectRootMarkers())
	decision := ctx.DecisionForCwd()
	if decision.TrustLevel != nil {
		t.Fatalf("expected nil trust level for no-entry, got %+v", decision)
	}
	// TrustKey falls back to the project/repo root key.
	if decision.TrustKey == "" {
		t.Fatalf("expected a fallback trust key")
	}
}

func TestIsProjectTrustedEmptyProjectsTable(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root)
	cwd := absPath(t, root)

	if IsProjectTrusted(TomlValue(map[string]any{}), cwd, nil) {
		t.Fatalf("expected untrusted with no projects table")
	}
}

// TestIsProjectTrustedAncestorProjectRoot verifies that when cwd is a
// subdirectory of the project root, the project-root entry governs trust (cwd
// itself has no entry but its repo/project root does).
func TestIsProjectTrustedAncestorProjectRoot(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root)
	sub := filepath.Join(root, "nested", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	cwd := absPath(t, sub)
	canonicalRoot := canonicalString(t, root)

	merged := mergedWithProjects(map[string]string{canonicalRoot: "trusted"})
	if !IsProjectTrusted(merged, cwd, nil) {
		t.Fatalf("expected nested cwd to inherit project-root trust")
	}
}

// TestDecisionForDirPrecedence verifies that an exact directory entry wins over
// the project-root entry (the directory key is consulted before the root key).
func TestDecisionForDirPrecedence(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root)
	sub := filepath.Join(root, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	cwd := absPath(t, sub)
	canonicalRoot := canonicalString(t, root)
	canonicalSub := canonicalString(t, sub)

	// Root trusted, but the specific dir is untrusted: the dir entry wins.
	merged := mergedWithProjects(map[string]string{
		canonicalRoot: "trusted",
		canonicalSub:  "untrusted",
	})
	ctx := BuildProjectTrustContext(merged, cwd, DefaultProjectRootMarkers())
	decision := ctx.DecisionForDir(cwd)
	if !decision.IsUntrusted() {
		t.Fatalf("expected dir-specific untrusted to win, got %+v", decision)
	}
}

// TestDecisionForDirDisabledMarkers verifies that empty project-root markers
// disable root detection (project root == cwd).
func TestDecisionForDirDisabledMarkers(t *testing.T) {
	root := t.TempDir()
	makeGitRepo(t, root)
	sub := filepath.Join(root, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	cwd := absPath(t, sub)
	canonicalRoot := canonicalString(t, root)

	merged := mergedWithProjects(map[string]string{canonicalRoot: "trusted"})
	// With markers disabled, the project root is the cwd, which has no entry,
	// but the repo root (via git resolution) still matches the root entry.
	ctx := BuildProjectTrustContext(merged, cwd, []string{})
	if ctx.projectRoot.String() != cwd.String() {
		t.Fatalf("expected project root == cwd with empty markers, got %q", ctx.projectRoot.String())
	}
	// Repo root still resolves via git and grants trust.
	if !ctx.DecisionForCwd().IsTrusted() {
		t.Fatalf("expected repo-root trust to apply via git resolution")
	}
}

// canonicalString returns the symlink-resolved, platform-normalized form of
// path used as the projects-table key.
func canonicalString(t *testing.T, path string) string {
	t.Helper()
	p := absPath(t, path)
	keys := normalizedProjectTrustKeys(p)
	return keys[0]
}
