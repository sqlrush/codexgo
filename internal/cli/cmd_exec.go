package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/sqlrush/codexgo/internal/exec"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// runExecSubcommand handles `codex exec` (alias `e`): non-interactive agent runs.
// It forwards the subcommand args to the exec package's own parser and runs the
// turn against the assembled engine, mirroring the codex exec dispatch in main.rs.
func runExecSubcommand(ctx context.Context, parsed ParsedCommandLine, streams Streams) int {
	cli, err := exec.ParseArgs(parsed.SubcommandArgs)
	if err != nil {
		fmt.Fprintln(streams.Stderr, err)
		return 2
	}

	asm, defaults, err := buildAssemblyWithDefaults()
	if err != nil {
		fmt.Fprintln(streams.Stderr, "codex exec:", err)
		return 1
	}

	// `codex exec` is non-interactive: its default approval policy is `never`
	// (exec/lib.rs), unlike the interactive on-request default. This drives both
	// the rendered permissions instructions and turn execution.
	defaults.ApprovalPolicy = protocol.AskForApproval{Kind: protocol.AskForApprovalNever}

	// Honor the `-C/--cd` working directory: commands and apply_patch resolve
	// relative paths against it, matching codex exec. A relative value is resolved
	// against the process cwd.
	if cli.Cwd != "" {
		cwd := cli.Cwd
		if !filepath.IsAbs(cwd) {
			cwd = filepath.Join(resolveCwd(), cwd)
		}
		defaults.Cwd = cwd
	}

	env := exec.Environment{
		Stdin:           streams.Stdin,
		Stdout:          streams.Stdout,
		Stderr:          streams.Stderr,
		StdinIsTerminal: streams.StdinIsTerminal,
		Assembly:        asm,
		Defaults:        defaults,
	}

	return exec.Run(ctx, cli, env)
}
