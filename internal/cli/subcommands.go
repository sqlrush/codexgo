package cli

// subcommandAliases maps each accepted subcommand token (canonical name or
// visible alias) to its canonical name. It mirrors the Subcommand enum in
// main.rs including the visible aliases: `e` for exec, `a` for apply, and the
// `cloud-tasks` alias. Only subcommands this port wires are included.
var subcommandAliases = map[string]string{
	"exec":       "exec",
	"e":          "exec",
	"login":      "login",
	"logout":     "logout",
	"mcp":        "mcp",
	"mcp-server": "mcp-server",
	"app-server": "app-server",
	"apply":      "apply",
	"a":          "apply",
	"resume":     "resume",
	"fork":       "fork",
	"archive":    "archive",
	"unarchive":  "unarchive",
	"features":   "features",
	"completion": "completion",
	"sandbox":    "sandbox",
	"doctor":     "doctor",
	"debug":      "debug",
}

// canonicalSubcommand resolves a token to its canonical subcommand name. The
// second return value reports whether the token names a known subcommand.
func canonicalSubcommand(token string) (string, bool) {
	name, ok := subcommandAliases[token]
	return name, ok
}

// subcommandSummaries lists the subcommands and their one-line descriptions for
// the top-level help text, in display order matching the Rust help.
var subcommandSummaries = []struct {
	Name    string
	Summary string
}{
	{"exec", "Run Codex non-interactively (alias: e)"},
	{"login", "Manage login"},
	{"logout", "Remove stored authentication credentials"},
	{"mcp", "Manage external MCP servers for Codex"},
	{"mcp-server", "Start Codex as an MCP server (stdio)"},
	{"app-server", "Run the app server (stdio)"},
	{"apply", "Apply the latest agent diff via `git apply` (alias: a)"},
	{"resume", "Resume a previous session"},
	{"fork", "Fork a previous session"},
	{"archive", "Archive a saved session by id or name"},
	{"unarchive", "Unarchive a saved session by id or name"},
	{"features", "Inspect and toggle feature flags"},
	{"completion", "Generate shell completion scripts"},
	{"sandbox", "Run a command within a Codex-provided sandbox"},
	{"doctor", "Diagnose local Codex installation, config, auth, and runtime health"},
	{"debug", "Debugging tools"},
}
