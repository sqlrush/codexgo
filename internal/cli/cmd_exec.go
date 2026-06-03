package cli

import (
	"context"
	"fmt"

	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/exec"
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

	asm, err := buildAssembly()
	if err != nil {
		fmt.Fprintln(streams.Stderr, "codex exec:", err)
		return 1
	}

	env := exec.Environment{
		Stdin:           streams.Stdin,
		Stdout:          streams.Stdout,
		Stderr:          streams.Stderr,
		StdinIsTerminal: streams.StdinIsTerminal,
		Assembly:        asm,
		Defaults: appserver.Defaults{
			Model:      "gpt-mock",
			ProviderID: "openai",
			Cwd:        resolveCwd(),
			UserAgent:  "codex-cli-go",
		},
	}

	return exec.Run(ctx, cli, env)
}
