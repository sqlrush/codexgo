// Package shell ports the turn-running subset of codex_core::shell /
// codex-shell: classifying a shell binary (ShellType), resolving the user's
// default interactive shell (DefaultUserShell) and deriving the argv used to
// run a command through it. It has no third-party dependencies; the bash
// tokenizer, URL and escalation detection live in the sibling package shellcmd
// (which mirrors codex-shell-command and pulls in mvdan.cc/sh).
package shell

import "path/filepath"

// ShellType identifies a recognized shell program. Mirrors the Rust
// `ShellType` enum in shell_detect.rs.
type ShellType int

const (
	// ShellTypeUnknown indicates the shell could not be classified.
	ShellTypeUnknown ShellType = iota
	// ShellTypeZsh is the Z shell.
	ShellTypeZsh
	// ShellTypeBash is the Bourne Again shell.
	ShellTypeBash
	// ShellTypePowerShell is Windows PowerShell or PowerShell Core.
	ShellTypePowerShell
	// ShellTypeSh is the POSIX shell.
	ShellTypeSh
	// ShellTypeCmd is the Windows command interpreter.
	ShellTypeCmd
)

// String returns the canonical lowercase shell name, or "unknown".
func (s ShellType) String() string {
	switch s {
	case ShellTypeZsh:
		return "zsh"
	case ShellTypeBash:
		return "bash"
	case ShellTypePowerShell:
		return "powershell"
	case ShellTypeSh:
		return "sh"
	case ShellTypeCmd:
		return "cmd"
	default:
		return "unknown"
	}
}

// DetectShellType classifies a shell from its path. It first matches the exact
// path against known bare names, then falls back to the file stem (the file name
// without its extension), recursing once on the stem. Returns ShellTypeUnknown
// (false) when the path does not name a recognized shell.
//
// This mirrors detect_shell_type in shell_detect.rs, including its recursion on
// the file stem so that paths like "/bin/bash" or "powershell.exe" resolve.
func DetectShellType(shellPath string) (ShellType, bool) {
	switch shellPath {
	case "zsh":
		return ShellTypeZsh, true
	case "sh":
		return ShellTypeSh, true
	case "cmd":
		return ShellTypeCmd, true
	case "bash":
		return ShellTypeBash, true
	case "pwsh":
		return ShellTypePowerShell, true
	case "powershell":
		return ShellTypePowerShell, true
	}

	// Fall back to the file stem (file name without final extension). Rust uses
	// Path::file_stem(), which strips the last extension and any directory
	// components. Recurse only when the stem differs from the original path to
	// avoid infinite recursion on bare names.
	stem := fileStem(shellPath)
	if stem == "" {
		return ShellTypeUnknown, false
	}
	if stem != shellPath {
		return DetectShellType(stem)
	}
	return ShellTypeUnknown, false
}

// fileStem returns the final path component with its last extension removed,
// matching Rust's Path::file_stem semantics closely enough for shell detection.
// A leading-dot name with no other dot (e.g. ".bashrc") keeps its full name.
func fileStem(path string) string {
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) || base == "" {
		return ""
	}
	ext := filepath.Ext(base)
	// filepath.Ext(".bashrc") == ".bashrc"; Rust treats a leading dot file with
	// no interior dot as having an empty extension, so the stem is the whole
	// name. Detect that case: ext starts at index 0.
	if ext != "" && ext != base {
		return base[:len(base)-len(ext)]
	}
	return base
}
