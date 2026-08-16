// Package shellcmd ports the subset of codex-shell-command and
// codex-shell-escalation that codexgo relies on: tokenizing bash scripts into
// word-only command sequences (via mvdan.cc/sh instead of tree-sitter-bash),
// extracting apply_patch heredocs, extracting URLs from command arguments, and
// detecting privilege-escalation wrappers (sudo, su, doas).
//
// Shell classification and the user's default shell live in the dependency-free
// sibling package shell (mirroring codex-shell); the historical
// shellcmd.ShellType / DefaultUserShell spellings remain as aliases.
//
// Faithful port of codex 0.136.0. Where the Rust implementation uses
// tree-sitter-bash, this port uses mvdan.cc/sh's syntax package; the observable
// facts (which scripts are accepted as word-only sequences, and what argv each
// command splits into) match codex.
package shellcmd

import "github.com/sqlrush/codexgo/internal/shell"

// ShellType identifies a recognized shell program. See [shell.ShellType].
type ShellType = shell.ShellType

// Shell type values, re-exported from package shell.
const (
	ShellTypeUnknown    = shell.ShellTypeUnknown
	ShellTypeZsh        = shell.ShellTypeZsh
	ShellTypeBash       = shell.ShellTypeBash
	ShellTypeSh         = shell.ShellTypeSh
	ShellTypePowerShell = shell.ShellTypePowerShell
	ShellTypeCmd        = shell.ShellTypeCmd
)

// UserShell is a resolved interactive shell. See [shell.UserShell].
type UserShell = shell.UserShell

// DetectShellType classifies a shell binary path. See [shell.DetectShellType].
func DetectShellType(shellPath string) (ShellType, bool) { return shell.DetectShellType(shellPath) }

// DefaultUserShell resolves the user's default shell. See [shell.DefaultUserShell].
func DefaultUserShell() UserShell { return shell.DefaultUserShell() }
