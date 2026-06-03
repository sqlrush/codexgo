package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/appserverclient"
)

// Environment bundles the process I/O and engine dependencies an exec run needs.
// Injecting it keeps Run testable: tests supply in-memory streams and a mock
// assembly while production wires os.Stdin/Stdout/Stderr and a real assembly.
type Environment struct {
	// Stdin is the prompt input source.
	Stdin io.Reader
	// Stdout receives JSONL events / the final message.
	Stdout io.Writer
	// Stderr receives progress and warnings.
	Stderr io.Writer
	// StdinIsTerminal reports whether Stdin is an interactive terminal; when true
	// stdin is not consumed as a prompt.
	StdinIsTerminal bool

	// Assembly is the constructed Codex engine to run the turn against.
	Assembly *appserver.Assembly
	// Defaults are the per-session default settings.
	Defaults appserver.Defaults
}

// Run executes a full exec invocation: parse-validated CLI plus an Environment.
// It resolves the prompt, starts the in-process client, drives the session, and
// returns the process exit code (0 success, 1 on a fatal error or failed turn).
//
// Setup failures (bad output schema, missing prompt) are reported to stderr and
// mapped to exit code 1, matching codex exec's std::process::exit(1) call sites.
func Run(ctx context.Context, cli CLI, env Environment) int {
	outputSchema, err := loadOutputSchema(cli.OutputSchema)
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 1
	}

	text, resumeID, err := resolveInvocation(cli, env)
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 1
	}

	sink := newSink(cli, env)

	c := appserverclient.StartInProcess(ctx, appserverclient.InProcessStartArgs{
		Assembly: env.Assembly,
		Defaults: env.Defaults,
	})

	session := NewSession(c, sink)
	outcome, err := session.Run(ctx, SessionConfig{
		Input:          buildInput(cli.Images, text),
		OutputSchema:   outputSchema,
		ResumeThreadID: resumeID,
	})
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 1
	}
	if outcome.ErrorSeen {
		return 1
	}
	return 0
}

// resolveInvocation resolves the prompt text and (for resume) the thread id to
// resume, based on the subcommand. For review it builds the review instructions.
func resolveInvocation(cli CLI, env Environment) (text string, resumeID string, err error) {
	penv := promptEnv{stdin: env.Stdin, stdinIsTerminal: env.StdinIsTerminal, errOut: env.Stderr}

	switch cli.Subcommand {
	case SubcommandReview:
		text, err = buildReviewPrompt(cli, penv)
		return text, "", err
	case SubcommandResume:
		prompt := resumePromptArg(cli)
		text, err = resolvePrompt(penv, prompt)
		return text, cli.ResumeSessionID, err
	default:
		text, err = resolveRootPrompt(penv, cli.Prompt)
		return text, "", err
	}
}

// resumePromptArg picks the prompt argument for a resume, preferring the explicit
// prompt and falling back to the session id when --last is set (matching the
// Rust reinterpretation of the positional under --last).
func resumePromptArg(cli CLI) *string {
	if cli.Prompt != nil {
		return cli.Prompt
	}
	if cli.ResumeLast && cli.ResumeSessionID != "" {
		id := cli.ResumeSessionID
		return &id
	}
	return nil
}

// newSink selects the JSONL or human-text sink based on --json.
func newSink(cli CLI, env Environment) EventSink {
	if cli.JSON {
		return NewJSONLSink(env.Stdout, env.Stderr, cli.OutputLastMessage)
	}
	return NewTextSink(env.Stdout, env.Stderr, cli.OutputLastMessage)
}

// loadOutputSchema reads and validates the JSON Schema file, returning the raw
// schema JSON (nil when no path was given). An unreadable or invalid file is an
// error mapped to exit 1 by the caller, matching load_output_schema.
func loadOutputSchema(path string) (json.RawMessage, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("exec: failed to read output schema file %q: %w", path, err)
	}
	var probe any
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("exec: output schema file %q is not valid JSON: %w", path, err)
	}
	return json.RawMessage(data), nil
}

// buildReviewPrompt constructs the review instructions for the review subcommand
// from the selected target (uncommitted/base/commit/custom). It mirrors the
// shape of codex review targets while running review as a normal turn (the
// in-process engine has no dedicated review/start path in this build).
func buildReviewPrompt(cli CLI, penv promptEnv) (string, error) {
	switch {
	case cli.ReviewUncommitted:
		return reviewInstruction("the staged, unstaged, and untracked changes in the repository"), nil
	case cli.ReviewBase != "":
		return reviewInstruction(fmt.Sprintf("the changes relative to base branch %q", cli.ReviewBase)), nil
	case cli.ReviewCommit != "":
		target := fmt.Sprintf("the changes introduced by commit %q", cli.ReviewCommit)
		if cli.ReviewTitle != "" {
			target += fmt.Sprintf(" (%s)", cli.ReviewTitle)
		}
		return reviewInstruction(target), nil
	case cli.Prompt != nil:
		custom, err := resolvePrompt(penv, cli.Prompt)
		if err != nil {
			return "", err
		}
		if custom == "" {
			return "", fmt.Errorf("review prompt cannot be empty")
		}
		return custom, nil
	default:
		return "", fmt.Errorf("specify --uncommitted, --base, --commit, or provide custom review instructions")
	}
}

// reviewInstruction renders the default review prompt for a described target.
func reviewInstruction(target string) string {
	return "Review " + target + " and report any correctness, security, or quality issues you find."
}
