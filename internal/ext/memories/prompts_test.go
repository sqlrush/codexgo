package memories

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

func TestBuildMemoryToolDeveloperInstructionsRendersTemplate(t *testing.T) {
	home := t.TempDir()
	memoriesDir := filepath.Join(home, "memories")
	if err := os.MkdirAll(memoriesDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoriesDir, "memory_summary.md"), []byte("Short memory summary for tests."), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	codexHome := abspath.ResolvePathAgainstBase(home, "/")
	instructions, ok, err := BuildMemoryToolDeveloperInstructions(codexHome)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected instructions to be produced")
	}
	wantLine := "- " + memoriesDir + "/memory_summary.md (already provided below; do NOT open again)"
	if !strings.Contains(instructions, wantLine) {
		t.Fatalf("instructions missing base path line:\n%s", instructions)
	}
	if !strings.Contains(instructions, "Short memory summary for tests.") {
		t.Fatalf("instructions missing summary text")
	}
	if got := strings.Count(instructions, "========= MEMORY_SUMMARY BEGINS ========="); got != 1 {
		t.Fatalf("MEMORY_SUMMARY begins count = %d, want 1", got)
	}
}

func TestBuildMemoryToolDeveloperInstructionsMissingSummary(t *testing.T) {
	home := t.TempDir()
	codexHome := abspath.ResolvePathAgainstBase(home, "/")
	_, ok, err := BuildMemoryToolDeveloperInstructions(codexHome)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected no instructions when summary missing")
	}
}

func TestBuildMemoryToolDeveloperInstructionsEmptySummary(t *testing.T) {
	home := t.TempDir()
	memoriesDir := filepath.Join(home, "memories")
	if err := os.MkdirAll(memoriesDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoriesDir, "memory_summary.md"), []byte("   \n  \n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	codexHome := abspath.ResolvePathAgainstBase(home, "/")
	_, ok, err := BuildMemoryToolDeveloperInstructions(codexHome)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected no instructions when summary blank")
	}
}
