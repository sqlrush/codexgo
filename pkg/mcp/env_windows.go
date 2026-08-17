//go:build windows

package mcp

// DefaultStdioEnvVars is the allowlist of environment variables forwarded to a
// local stdio MCP server on Windows. It mirrors the reference
// `shell_environment::WINDOWS_CORE_ENV_VARS` constant.
var DefaultStdioEnvVars = []string{
	// Core path resolution
	"PATH",
	"PATHEXT",
	// Shell and system roots
	"SHELL",
	"COMSPEC",
	"SYSTEMROOT",
	"SYSTEMDRIVE",
	// User context and profiles
	"USERNAME",
	"USERDOMAIN",
	"USERPROFILE",
	"HOMEDRIVE",
	"HOMEPATH",
	// Program locations
	"PROGRAMFILES",
	"PROGRAMFILES(X86)",
	"PROGRAMW6432",
	"PROGRAMDATA",
	// App data and caches
	"LOCALAPPDATA",
	"APPDATA",
	// Temp locations
	"TEMP",
	"TMP",
	"TMPDIR",
	// Common shells/pwsh hints
	"POWERSHELL",
	"PWSH",
}
