package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/pkg/config"
)

// runMcpSubcommand handles `codex mcp <list|get|add|remove|login|logout>`,
// mirroring the McpCli surface in main.rs. The read-only views (list, get) are
// wired against the configured mcp_servers; the config-mutating and OAuth flows
// (add, remove, login, logout) are owned by the mcp management area and report
// clear guidance here.
func runMcpSubcommand(_ context.Context, parsed ParsedCommandLine, streams Streams) int {
	args := parsed.SubcommandArgs
	if len(args) == 0 {
		fmt.Fprintln(streams.Stderr, "error: a subcommand is required: list, get, add, remove, login, or logout")
		return 1
	}
	if args[0] == "-h" || args[0] == "--help" {
		printMcpHelp(streams.Stdout)
		return 0
	}

	switch args[0] {
	case "list":
		return runMcpList(parsed, streams, args[1:])
	case "get":
		return runMcpGet(parsed, streams, args[1:])
	case "add", "remove", "login", "logout":
		fmt.Fprintf(streams.Stderr,
			"`codex mcp %s` (config-mutating / OAuth flow) is owned by the MCP management area and is not yet wired in this build.\n",
			args[0])
		return 1
	default:
		fmt.Fprintf(streams.Stderr, "error: unknown mcp subcommand %q\n", args[0])
		return 1
	}
}

// runMcpList lists the configured MCP servers, mirroring run_list.
func runMcpList(parsed ParsedCommandLine, streams Streams, args []string) int {
	jsonOut := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOut = true
		default:
			fmt.Fprintf(streams.Stderr, "error: unexpected argument: %s\n", arg)
			return 1
		}
	}

	servers, code := loadMcpServers(parsed, streams)
	if code != 0 {
		return code
	}

	names := sortedServerNames(servers)
	if jsonOut {
		return renderMcpListJSON(streams, servers, names)
	}
	if len(names) == 0 {
		fmt.Fprintln(streams.Stdout, "No MCP servers configured yet. Try `codex mcp add my-tool -- my-command`.")
		return 0
	}
	for _, name := range names {
		renderMcpServerText(streams.Stdout, name, servers[name])
	}
	return 0
}

// runMcpGet shows a single configured MCP server, mirroring run_get.
func runMcpGet(parsed ParsedCommandLine, streams Streams, args []string) int {
	jsonOut := false
	target := ""
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOut = true
		default:
			if strings.HasPrefix(arg, "-") {
				fmt.Fprintf(streams.Stderr, "error: unexpected flag: %s\n", arg)
				return 1
			}
			target = arg
		}
	}
	if target == "" {
		fmt.Fprintln(streams.Stderr, "error: get requires a server NAME")
		return 1
	}

	servers, code := loadMcpServers(parsed, streams)
	if code != 0 {
		return code
	}
	server, ok := servers[target]
	if !ok {
		fmt.Fprintf(streams.Stderr, "No MCP server named '%s' found.\n", target)
		return 1
	}
	if jsonOut {
		data, err := json.MarshalIndent(map[string]any{"name": target, "config": server}, "", "  ")
		if err != nil {
			fmt.Fprintf(streams.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Fprintln(streams.Stdout, string(data))
		return 0
	}
	renderMcpServerText(streams.Stdout, target, server)
	return 0
}

// loadMcpServers loads the configured mcp_servers map honoring -c overrides and
// the selected profile.
func loadMcpServers(parsed ParsedCommandLine, streams Streams) (map[string]config.McpServerConfig, int) {
	overrides, err := parsed.Root.Overrides.Parse()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return nil, 1
	}
	result, err := config.Load(config.LoadOptions{
		Profile:      parsed.Root.Profile,
		CliOverrides: overrides,
		StrictConfig: parsed.Root.StrictConfig,
	})
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return nil, 1
	}
	return result.Config.McpServers, 0
}

// sortedServerNames returns the server names in lexical order.
func sortedServerNames(servers map[string]config.McpServerConfig) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// renderMcpListJSON emits the configured servers as a JSON array, mirroring the
// --json branch of run_list (each entry carries the name plus its config).
func renderMcpListJSON(streams Streams, servers map[string]config.McpServerConfig, names []string) int {
	entries := make([]map[string]any, 0, len(names))
	for _, name := range names {
		entries = append(entries, map[string]any{"name": name, "config": servers[name]})
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintln(streams.Stdout, string(data))
	return 0
}

// renderMcpServerText prints a single server's config in the human format,
// following the layout of run_get.
func renderMcpServerText(w io.Writer, name string, cfg config.McpServerConfig) {
	fmt.Fprintln(w, name)
	fmt.Fprintf(w, "  enabled: %t\n", cfg.Enabled)
	switch cfg.Transport.Kind {
	case config.McpTransportStdio:
		fmt.Fprintln(w, "  transport: stdio")
		fmt.Fprintf(w, "  command: %s\n", cfg.Transport.Command)
		if len(cfg.Transport.Args) > 0 {
			fmt.Fprintf(w, "  args: %s\n", strings.Join(cfg.Transport.Args, " "))
		}
		if cfg.Transport.Cwd != nil {
			fmt.Fprintf(w, "  cwd: %s\n", *cfg.Transport.Cwd)
		}
	case config.McpTransportStreamableHTTP:
		fmt.Fprintln(w, "  transport: streamable_http")
		fmt.Fprintf(w, "  url: %s\n", cfg.Transport.URL)
		if cfg.Transport.BearerTokenEnvVar != nil {
			fmt.Fprintf(w, "  bearer_token_env_var: %s\n", *cfg.Transport.BearerTokenEnvVar)
		}
	}
}

func printMcpHelp(w io.Writer) {
	fmt.Fprintln(w, "Manage external MCP servers for Codex")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex mcp <COMMAND>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  list            List configured servers (with --json)")
	fmt.Fprintln(w, "  get <NAME>      Show a single configured server (with --json)")
	fmt.Fprintln(w, "  add             Add a server entry to config.toml")
	fmt.Fprintln(w, "  remove          Remove a server entry")
	fmt.Fprintln(w, "  login           Authenticate with an MCP server using OAuth")
	fmt.Fprintln(w, "  logout          Remove OAuth credentials for an MCP server")
}
