//go:build unix

package mcp

// DefaultStdioEnvVars is the allowlist of environment variables forwarded to a
// local stdio MCP server on Unix-like platforms. It mirrors the reference
// `DEFAULT_ENV_VARS` constant.
var DefaultStdioEnvVars = []string{
	"HOME",
	"LOGNAME",
	"PATH",
	"SHELL",
	"USER",
	"__CF_USER_TEXT_ENCODING",
	"LANG",
	"LC_ALL",
	"TERM",
	"TMPDIR",
	"TZ",
}
