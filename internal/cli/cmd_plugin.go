package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/plugins"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// runPluginSubcommand handles `codex plugin`: manage Codex plugins. It mirrors
// the Rust PluginSubcommand surface (add/list/remove/marketplace).
//
// The offline `list` action (enumerate plugins from configured marketplace
// snapshots) is wired against internal/plugins. The `add`/`remove` actions and
// the `marketplace` management sub-tree mutate config and fetch from remote
// marketplace sources; those write/network paths are deferred and emit a clear
// notice with a non-zero exit. The subcommand and its --help are always
// registered.
func runPluginSubcommand(_ context.Context, parsed ParsedCommandLine, streams Streams) int {
	if len(parsed.SubcommandArgs) == 0 {
		printPluginHelp(streams.Stdout)
		return 0
	}

	action := parsed.SubcommandArgs[0]
	rest := parsed.SubcommandArgs[1:]

	if action == "-h" || action == "--help" {
		printPluginHelp(streams.Stdout)
		return 0
	}

	switch action {
	case "list":
		return runPluginList(rest, streams)
	case "add":
		return pluginDeferred(streams, "add",
			"Installing a plugin fetches and unpacks a marketplace snapshot and edits config (deferred path).")
	case "remove":
		return pluginDeferred(streams, "remove",
			"Removing a plugin edits config and prunes the local cache (deferred path).")
	case "marketplace":
		return runPluginMarketplace(rest, streams)
	default:
		fmt.Fprintf(streams.Stderr, "error: unknown plugin command %q\n", action)
		printPluginHelp(streams.Stderr)
		return 2
	}
}

// runPluginList lists plugins available from configured marketplace snapshots.
func runPluginList(args []string, streams Streams) int {
	var marketplaceFilter string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			fmt.Fprintln(streams.Stdout, "List plugins available from configured marketplace snapshots")
			fmt.Fprintln(streams.Stdout)
			fmt.Fprintln(streams.Stdout, "Usage: codex plugin list [--marketplace <NAME>]")
			return 0
		case arg == "--marketplace":
			v, ni, err := takeValue(args, i, arg)
			if err != nil {
				fmt.Fprintf(streams.Stderr, "error: %v\n", err)
				return 2
			}
			marketplaceFilter, i = v, ni-1
		case strings.HasPrefix(arg, "--marketplace="):
			marketplaceFilter = strings.TrimPrefix(arg, "--marketplace=")
		default:
			fmt.Fprintf(streams.Stderr, "error: unexpected argument: %s\n", arg)
			return 2
		}
	}

	outcome, err := plugins.ListMarketplaces([]abspath.AbsolutePathBuf(nil))
	if err != nil {
		fmt.Fprintf(streams.Stderr, "codex plugin list: %v\n", err)
		return 1
	}

	printed := printPluginRows(outcome, marketplaceFilter, streams.Stdout)
	for _, e := range outcome.Errors {
		fmt.Fprintf(streams.Stderr, "warning: %s: %s\n", e.Path.String(), e.Message)
	}
	if !printed {
		fmt.Fprintln(streams.Stdout, "No plugins found in configured marketplaces.")
	}
	return 0
}

// printPluginRows writes one tab-separated row per plugin and returns whether any
// row was printed. Output is sorted by marketplace then plugin name for stable
// listings.
func printPluginRows(outcome plugins.MarketplaceListOutcome, marketplaceFilter string, w io.Writer) bool {
	type row struct {
		marketplace string
		name        string
		version     string
	}
	var rows []row
	for _, m := range outcome.Marketplaces {
		if marketplaceFilter != "" && m.Name != marketplaceFilter {
			continue
		}
		for _, p := range m.Plugins {
			version := "-"
			if p.LocalVersion != nil && *p.LocalVersion != "" {
				version = *p.LocalVersion
			}
			rows = append(rows, row{marketplace: m.Name, name: p.Name, version: version})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].marketplace != rows[j].marketplace {
			return rows[i].marketplace < rows[j].marketplace
		}
		return rows[i].name < rows[j].name
	})
	for _, r := range rows {
		fmt.Fprintf(w, "%s@%s\t%s\n", r.name, r.marketplace, r.version)
	}
	return len(rows) > 0
}

// runPluginMarketplace handles `codex plugin marketplace <add|list|remove|upgrade>`.
// The read-only `list` is wired; mutating actions are deferred with a notice.
func runPluginMarketplace(args []string, streams Streams) int {
	if len(args) == 0 {
		printPluginMarketplaceHelp(streams.Stdout)
		return 0
	}
	switch args[0] {
	case "-h", "--help":
		printPluginMarketplaceHelp(streams.Stdout)
		return 0
	case "list":
		outcome, err := plugins.ListMarketplaces([]abspath.AbsolutePathBuf(nil))
		if err != nil {
			fmt.Fprintf(streams.Stderr, "codex plugin marketplace list: %v\n", err)
			return 1
		}
		if len(outcome.Marketplaces) == 0 {
			fmt.Fprintln(streams.Stdout, "No marketplaces configured.")
		}
		for _, m := range outcome.Marketplaces {
			fmt.Fprintf(streams.Stdout, "%s\t%s\n", m.Name, m.Path.String())
		}
		for _, e := range outcome.Errors {
			fmt.Fprintf(streams.Stderr, "warning: %s: %s\n", e.Path.String(), e.Message)
		}
		return 0
	case "upgrade":
		return runPluginMarketplaceUpgrade(args[1:], streams)
	case "add", "remove":
		return pluginDeferred(streams, "marketplace "+args[0],
			"Managing marketplaces edits config and fetches remote sources (deferred path).")
	default:
		fmt.Fprintf(streams.Stderr, "error: unknown marketplace command %q\n", args[0])
		printPluginMarketplaceHelp(streams.Stderr)
		return 2
	}
}

// runPluginMarketplaceUpgrade handles `codex plugin marketplace upgrade [NAME]`:
// it refreshes the local clones of configured git marketplaces, mirroring the
// Rust `run_upgrade`. With NAME, only that marketplace is upgraded. Per-
// marketplace failures are reported but do not abort the rest; the exit code is
// non-zero when any marketplace failed.
func runPluginMarketplaceUpgrade(args []string, streams Streams) int {
	var marketplaceName string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			fmt.Fprintln(streams.Stdout, "Upgrade configured git marketplaces to their latest revision")
			fmt.Fprintln(streams.Stdout)
			fmt.Fprintln(streams.Stdout, "Usage: codex plugin marketplace upgrade [MARKETPLACE_NAME]")
			return 0
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(streams.Stderr, "error: unexpected argument: %s\n", arg)
			return 2
		case marketplaceName == "":
			marketplaceName = arg
		default:
			fmt.Fprintf(streams.Stderr, "error: unexpected argument: %s\n", arg)
			return 2
		}
	}

	cfg, err := loadConfig(RootOptions{})
	if err != nil {
		fmt.Fprintf(streams.Stderr, "codex plugin marketplace upgrade: %v\n", err)
		return 1
	}

	outcome := plugins.UpgradeConfiguredGitMarketplaces(cfg.CodexHome, cfg.Marketplaces, marketplaceName)
	if len(outcome.SelectedMarketplaces) == 0 {
		if marketplaceName != "" {
			fmt.Fprintf(streams.Stdout, "No configured git marketplace named %q.\n", marketplaceName)
		} else {
			fmt.Fprintln(streams.Stdout, "No configured git marketplaces to upgrade.")
		}
		return 0
	}
	for _, root := range outcome.UpgradedRoots {
		fmt.Fprintf(streams.Stdout, "Upgraded marketplace at %s\n", root.String())
	}
	if len(outcome.UpgradedRoots) == 0 && outcome.AllSucceeded() {
		fmt.Fprintln(streams.Stdout, "All configured git marketplaces are already up to date.")
	}
	for _, e := range outcome.Errors {
		fmt.Fprintf(streams.Stderr, "error: failed to upgrade marketplace %q: %s\n", e.MarketplaceName, e.Message)
	}
	if !outcome.AllSucceeded() {
		return 1
	}
	return 0
}

// pluginDeferred prints a clear notice for an unimplemented mutating plugin path
// and returns a non-zero exit code (never silent).
func pluginDeferred(streams Streams, action, notice string) int {
	fmt.Fprintf(streams.Stderr, "`codex plugin %s` is not available in this build: %s\n", action, notice)
	return 1
}

func printPluginHelp(w io.Writer) {
	fmt.Fprintln(w, "Manage Codex plugins")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex plugin <COMMAND>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  add            Install a plugin from a configured marketplace snapshot")
	fmt.Fprintln(w, "  list           List plugins available from configured marketplace snapshots")
	fmt.Fprintln(w, "  marketplace    Add, list, upgrade, or remove configured plugin marketplaces")
	fmt.Fprintln(w, "  remove         Remove an installed plugin from local config and cache")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  -h, --help     Print help")
}

func printPluginMarketplaceHelp(w io.Writer) {
	fmt.Fprintln(w, "Add, list, upgrade, or remove configured plugin marketplaces")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex plugin marketplace <COMMAND>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  add            Add a marketplace")
	fmt.Fprintln(w, "  list           List configured marketplaces")
	fmt.Fprintln(w, "  remove         Remove a marketplace")
	fmt.Fprintln(w, "  upgrade        Upgrade marketplace snapshots")
}
