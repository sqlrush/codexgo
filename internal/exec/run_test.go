package exec

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/appserver"
)

// runEnv builds an Environment over a mock assembly producing the given reply.
func runEnv(t *testing.T, reply string, stdin string, stdinIsTerminal bool) (Environment, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := Environment{
		Stdin:           strings.NewReader(stdin),
		Stdout:          &stdout,
		Stderr:          &stderr,
		StdinIsTerminal: stdinIsTerminal,
		Assembly:        mockAssembly(t, reply),
		Defaults:        appserver.Defaults{Model: "gpt-test", ProviderID: "openai", Cwd: "/work", UserAgent: "exec-test"},
	}
	return env, &stdout, &stderr
}

// TestRunWritesLastMessageFile verifies --output-last-message persists the final
// agent message on a successful turn.
func TestRunWritesLastMessageFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last.txt")
	env, _, stderr := runEnv(t, "persisted answer", "", true)

	cli := CLI{Subcommand: SubcommandRun, JSON: true, OutputLastMessage: path, Prompt: strPtr("hi")}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if code := Run(ctx, cli, env); code != 0 {
		t.Fatalf("exit code %d; stderr=%s", code, stderr.String())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read last message file: %v", err)
	}
	if string(data) != "persisted answer" {
		t.Fatalf("last message file: got %q", string(data))
	}
}

// TestRunResumeIssuesResumeRequest verifies the resume subcommand drives the
// resume code path (thread/resume) rather than thread/start. The bare in-process
// assembly used in tests has no rollout storage, so resuming by id alone is
// rejected by the engine; the test asserts the resume error is surfaced (exit 1)
// — proving the resume request was issued.
func TestRunResumeIssuesResumeRequest(t *testing.T) {
	env, _, stderr := runEnv(t, "resumed reply", "", true)
	cli := CLI{Subcommand: SubcommandResume, ResumeSessionID: "sess-1", Prompt: strPtr("continue")}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if code := Run(ctx, cli, env); code != 1 {
		t.Fatalf("expected exit 1 from unsupported resume, got %d; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "thread/resume") {
		t.Fatalf("expected a thread/resume error, got %q", stderr.String())
	}
}

// TestRunReviewUncommitted verifies the review subcommand builds an instruction
// prompt and runs a turn without requiring a positional prompt.
func TestRunReviewUncommitted(t *testing.T) {
	env, stdout, stderr := runEnv(t, "review done", "", true)
	cli := CLI{Subcommand: SubcommandReview, ReviewUncommitted: true}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if code := Run(ctx, cli, env); code != 0 {
		t.Fatalf("exit code %d; stderr=%s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "review done" {
		t.Fatalf("stdout: got %q", got)
	}
}

// TestRunReviewRequiresTarget verifies review with no target/prompt fails fast.
func TestRunReviewRequiresTarget(t *testing.T) {
	env, _, stderr := runEnv(t, "x", "", true)
	cli := CLI{Subcommand: SubcommandReview}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if code := Run(ctx, cli, env); code != 1 {
		t.Fatalf("expected exit 1 for review with no target, got %d", code)
	}
	if !strings.Contains(stderr.String(), "uncommitted") {
		t.Fatalf("expected guidance on review targets, got %q", stderr.String())
	}
}

// TestRunMissingPromptErrors verifies a missing prompt on a terminal exits 1.
func TestRunMissingPromptErrors(t *testing.T) {
	env, _, stderr := runEnv(t, "x", "", true)
	cli := CLI{Subcommand: SubcommandRun}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if code := Run(ctx, cli, env); code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("expected an error message on stderr")
	}
}

// TestRunInvalidOutputSchema verifies a malformed schema file exits 1 before any
// turn runs.
func TestRunInvalidOutputSchema(t *testing.T) {
	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schema, []byte("not json"), 0o644); err != nil {
		t.Fatalf("seed schema: %v", err)
	}
	env, _, stderr := runEnv(t, "x", "", true)
	cli := CLI{Subcommand: SubcommandRun, OutputSchema: schema, Prompt: strPtr("hi")}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if code := Run(ctx, cli, env); code != 1 {
		t.Fatalf("expected exit 1 for bad schema, got %d", code)
	}
	if !strings.Contains(stderr.String(), "schema") {
		t.Fatalf("expected schema error, got %q", stderr.String())
	}
}

// TestLoadOutputSchemaValid verifies a valid schema file is loaded as raw JSON.
func TestLoadOutputSchemaValid(t *testing.T) {
	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schema, []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatalf("seed schema: %v", err)
	}
	raw, err := loadOutputSchema(schema)
	if err != nil {
		t.Fatalf("loadOutputSchema: %v", err)
	}
	if string(raw) != `{"type":"object"}` {
		t.Fatalf("schema raw: got %q", string(raw))
	}
	if empty, err := loadOutputSchema(""); err != nil || empty != nil {
		t.Fatalf("empty path should yield nil schema, got %v err %v", empty, err)
	}
}
