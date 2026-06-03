package cli

// subcommandAliases maps each accepted subcommand token (canonical name or
// visible alias) to its canonical name. It mirrors the Subcommand enum in
// main.rs including the visible aliases: `e` for exec, `a` for apply, and the
// `cloud-tasks` alias. Only subcommands this port wires are included.
var subcommandAliases = map[string]string{
	"exec":           "exec",
	"e":              "exec",
	"review":         "review",
	"login":          "login",
	"logout":         "logout",
	"mcp":            "mcp",
	"plugin":         "plugin",
	"mcp-server":     "mcp-server",
	"app-server":     "app-server",
	"remote-control": "remote-control",
	"app":            "app",
	"apply":          "apply",
	"a":              "apply",
	"resume":         "resume",
	"fork":           "fork",
	"archive":        "archive",
	"unarchive":      "unarchive",
	"cloud":          "cloud",
	"cloud-tasks":    "cloud",
	"exec-server":    "exec-server",
	"features":       "features",
	"completion":     "completion",
	"update":         "update",
	"sandbox":        "sandbox",
	"doctor":         "doctor",
	"debug":          "debug",
	"help":           "help",
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
	{"review", "Run a code review non-interactively"},
	{"login", "Manage login"},
	{"logout", "Remove stored authentication credentials"},
	{"mcp", "Manage external MCP servers for Codex"},
	{"plugin", "Manage Codex plugins"},
	{"mcp-server", "Start Codex as an MCP server (stdio)"},
	{"app-server", "Run the app server (stdio)"},
	{"remote-control", "Manage the app-server daemon with remote control enabled"},
	{"app", "Launch the Codex desktop app (macOS/Windows)"},
	{"completion", "Generate shell completion scripts"},
	{"update", "Update Codex to the latest version"},
	{"doctor", "Diagnose local Codex installation, config, auth, and runtime health"},
	{"sandbox", "Run a command within a Codex-provided sandbox"},
	{"debug", "Debugging tools"},
	{"apply", "Apply the latest agent diff via `git apply` (alias: a)"},
	{"resume", "Resume a previous session"},
	{"archive", "Archive a saved session by id or name"},
	{"unarchive", "Unarchive a saved session by id or name"},
	{"fork", "Fork a previous session"},
	{"cloud", "Browse tasks from Codex Cloud and apply changes locally (alias: cloud-tasks)"},
	{"exec-server", "Run the standalone exec-server service"},
	{"features", "Inspect and toggle feature flags"},
	{"help", "Print this message or the help of the given subcommand(s)"},
}
