package cli

import (
	"context"
	"fmt"

	"github.com/sqlrush/codexgo/internal/appserverclient"
	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/tui"
)

// runDefaultNoSubcommand handles `codex` with no subcommand: launch the
// interactive TUI against an in-process engine, mirroring codex's default
// behavior. The TUI requires an interactive terminal; in non-interactive
// contexts (pipes/CI) it prints a clear notice and exits non-zero so wrappers
// and scripts can detect that no interactive session ran.
func runDefaultNoSubcommand(ctx context.Context, _ ParsedCommandLine, streams Streams) int {
	if !streams.StdinIsTerminal || !streams.StderrIsTerminal {
		fmt.Fprintf(streams.Stderr, "codex %s — parity target: codex 0.136.0\n", Version)
		fmt.Fprintln(streams.Stderr, "the interactive TUI requires a terminal; use `codex exec` for non-interactive runs")
		return 1
	}

	asm, defaults, err := buildAssemblyWithDefaults()
	if err != nil {
		fmt.Fprintln(streams.Stderr, "codex:", err)
		return 1
	}
	// Keep the versioned interactive user-agent; the model + provider id come from
	// the resolved configuration so the TUI honors a custom `model_provider`.
	defaults.UserAgent = "codex-cli-go/" + Version
	client := appserverclient.StartInProcess(ctx, appserverclient.InProcessStartArgs{
		Assembly: asm,
		Defaults: defaults,
	})
	defer client.Shutdown(context.WithoutCancel(ctx))

	// /model picker wiring: the entry list combines the bundled catalog with
	// the configured custom-provider models; selections persist into
	// config.toml's `model` key.
	pickerCfg, havePickerCfg := loadProviderConfig()
	persistModel := func(slug string) error {
		home := pickerCfg.CodexHome
		if home == "" {
			resolved, err := config.FindCodexHome()
			if err != nil {
				return err
			}
			home = resolved
		}
		return persistModelSelection(home, slug)
	}

	if err := tui.Run(ctx, tui.RunConfig{
		Client:  client,
		Workdir: resolveCwd(),
		Model:   defaults.Model,
		// The welcome card now shows codexgo's OWN product name + version
		// (cli.Version, ldflags-stamped). This is the first branding deviation
		// from the upstream "OpenAI Codex (v0.136.0)" the parity frames assert;
		// the title row is masked in the parity frame tests.
		Version:               Version,
		Models:                buildModelPickerEntries(pickerCfg, havePickerCfg),
		PersistModelSelection: persistModel,
	}); err != nil {
		fmt.Fprintln(streams.Stderr, "codex:", err)
		return 1
	}
	return 0
}
