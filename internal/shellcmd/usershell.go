package shellcmd

import (
	"bufio"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
	"strings"
)

// UserShell is a resolved interactive shell: its classified type and the
// absolute path to its binary. It is the Go analogue of the turn-running subset
// of codex_core::shell::Shell (the shell-snapshot field is omitted).
type UserShell struct {
	// Type is the classified shell program.
	Type ShellType
	// Path is the absolute path to the shell binary.
	Path string
}

// DeriveExecArgs renders the argv used to run command through this shell,
// mirroring codex_core::shell::Shell::derive_exec_args. For zsh/bash/sh it is
// [path, "-lc"|"-c", command]; for PowerShell it is [path, ("-NoProfile"?),
// "-Command", command]; for cmd it is [path, "/c", command].
func (s UserShell) DeriveExecArgs(command string, useLoginShell bool) []string {
	switch s.Type {
	case ShellTypeZsh, ShellTypeBash, ShellTypeSh:
		flag := "-c"
		if useLoginShell {
			flag = "-lc"
		}
		return []string{s.Path, flag, command}
	case ShellTypePowerShell:
		args := []string{s.Path}
		if !useLoginShell {
			args = append(args, "-NoProfile")
		}
		return append(args, "-Command", command)
	case ShellTypeCmd:
		return []string{s.Path, "/c", command}
	default:
		// Treat an unclassified shell as a POSIX shell so the command still runs.
		flag := "-c"
		if useLoginShell {
			flag = "-lc"
		}
		return []string{s.Path, flag, command}
	}
}

// DefaultUserShell resolves the user's default interactive shell, mirroring
// codex_core::shell::default_user_shell. It reads the operating-system account
// shell (the passwd pw_shell field) and classifies it; when that is missing or
// unrecognized it falls back to the platform defaults (macOS: zsh, then bash,
// then sh; other unix: bash, then sh; Windows: PowerShell, then cmd). The final
// fallback is /bin/sh on unix and cmd.exe on Windows, matching the Rust
// ultimate_fallback_shell.
func DefaultUserShell() UserShell {
	if runtime.GOOS == "windows" {
		if sh, ok := resolveShell(ShellTypePowerShell); ok {
			return sh
		}
		if sh, ok := resolveShell(ShellTypeCmd); ok {
			return sh
		}
		return UserShell{Type: ShellTypeCmd, Path: "cmd.exe"}
	}

	if path := accountShellPath(); path != "" {
		if st, ok := DetectShellType(path); ok {
			if sh, ok := resolveShell(st); ok {
				return sh
			}
		}
	}

	order := []ShellType{ShellTypeBash, ShellTypeSh}
	if runtime.GOOS == "darwin" {
		order = []ShellType{ShellTypeZsh, ShellTypeBash, ShellTypeSh}
	}
	for _, st := range order {
		if sh, ok := resolveShell(st); ok {
			return sh
		}
	}
	return UserShell{Type: ShellTypeSh, Path: "/bin/sh"}
}

// resolveShell resolves the absolute path for a shell type using, in order: the
// account shell when it matches the requested type, a PATH lookup of the binary
// name, then the platform fallback paths. It returns ok == false when no path
// for the requested type can be found.
func resolveShell(st ShellType) (UserShell, bool) {
	if path := accountShellPath(); path != "" {
		if t, ok := DetectShellType(path); ok && t == st && fileExists(path) {
			return UserShell{Type: st, Path: path}, true
		}
	}
	for _, name := range binaryNames(st) {
		if path, err := exec.LookPath(name); err == nil {
			return UserShell{Type: st, Path: path}, true
		}
	}
	for _, path := range fallbackPaths(st) {
		if fileExists(path) {
			return UserShell{Type: st, Path: path}, true
		}
	}
	return UserShell{}, false
}

// binaryNames returns the PATH lookup names for a shell type.
func binaryNames(st ShellType) []string {
	switch st {
	case ShellTypeZsh:
		return []string{"zsh"}
	case ShellTypeBash:
		return []string{"bash"}
	case ShellTypeSh:
		return []string{"sh"}
	case ShellTypePowerShell:
		return []string{"pwsh", "powershell"}
	case ShellTypeCmd:
		return []string{"cmd"}
	default:
		return nil
	}
}

// fallbackPaths returns the well-known absolute paths for a shell type, matching
// the *_FALLBACK_PATHS constants in shell.rs.
func fallbackPaths(st ShellType) []string {
	switch st {
	case ShellTypeZsh:
		return []string{"/bin/zsh"}
	case ShellTypeBash:
		return []string{"/bin/bash"}
	case ShellTypeSh:
		return []string{"/bin/sh"}
	default:
		return nil
	}
}

// fileExists reports whether path names an existing regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// accountShellPath returns the current user's account login shell (the passwd
// pw_shell field), or "" when it cannot be determined. On macOS it queries the
// directory service via dscl; on other unix systems it parses /etc/passwd for
// the current uid. The SHELL environment variable is intentionally not consulted
// so the result matches codex's getpwuid_r-based resolution.
func accountShellPath() string {
	switch runtime.GOOS {
	case "darwin":
		return darwinAccountShell()
	case "windows":
		return ""
	default:
		return passwdAccountShell()
	}
}

// darwinAccountShell reads the UserShell attribute from the macOS directory
// service for the current user, matching the pw_shell value codex reads.
func darwinAccountShell() string {
	u, err := user.Current()
	if err != nil || u.Username == "" {
		return ""
	}
	out, err := exec.Command("dscl", ".", "-read", "/Users/"+u.Username, "UserShell").Output()
	if err != nil {
		return ""
	}
	// Output is "UserShell: /bin/zsh\n".
	line := strings.TrimSpace(string(out))
	const prefix = "UserShell:"
	if !strings.HasPrefix(line, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(line, prefix))
}

// passwdAccountShell parses /etc/passwd for the current uid's login shell. The
// passwd line format is name:passwd:uid:gid:gecos:home:shell.
func passwdAccountShell() string {
	u, err := user.Current()
	if err != nil || u.Uid == "" {
		return ""
	}
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	uid, convErr := strconv.Atoi(u.Uid)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) < 7 {
			continue
		}
		// Match by uid when it parses, otherwise by username.
		if convErr == nil {
			if fields[2] == strconv.Itoa(uid) {
				return strings.TrimSpace(fields[6])
			}
			continue
		}
		if fields[0] == u.Username {
			return strings.TrimSpace(fields[6])
		}
	}
	return ""
}
