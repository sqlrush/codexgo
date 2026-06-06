package gitutils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ApplyGitRequest holds the parameters for ApplyGitPatch.
//
// Mirrors the Rust `ApplyGitRequest`.
type ApplyGitRequest struct {
	// Cwd is a path inside the target repository.
	Cwd string
	// Diff is the unified diff text to apply.
	Diff string
	// Revert applies the patch in reverse (`git apply -R`).
	Revert bool
	// Preflight performs a dry-run (`git apply --check`) without modifying the
	// working tree.
	Preflight bool
}

// ApplyGitResult holds the outcome of ApplyGitPatch, including paths gleaned
// from the command output.
//
// Mirrors the Rust `ApplyGitResult`.
type ApplyGitResult struct {
	ExitCode        int
	AppliedPaths    []string
	SkippedPaths    []string
	ConflictedPaths []string
	Stdout          string
	Stderr          string
	CmdForLog       string
}

// ApplyGitPatch applies a unified diff to the target repository by shelling out
// to `git apply --3way`. When req.Preflight is true it behaves like
// `git apply --check`, leaving the working tree untouched while still parsing
// the output for diagnostics. When req.Revert is true (and not preflight), the
// affected working-tree paths are staged first to avoid an index mismatch.
//
// Mirrors the Rust `apply_git_patch`. The `git` binary is used directly (rather
// than go-git) because go-git has no faithful equivalent of `git apply --3way`.
func ApplyGitPatch(req ApplyGitRequest) (ApplyGitResult, error) {
	gitRoot, err := resolveGitRoot(req.Cwd)
	if err != nil {
		return ApplyGitResult{}, err
	}

	tmpDir, patchPath, err := writeTempPatch(req.Diff)
	if err != nil {
		return ApplyGitResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	if req.Revert && !req.Preflight {
		// Stage working-tree paths first to avoid index mismatch on revert.
		if err := StagePaths(gitRoot, req.Diff); err != nil {
			return ApplyGitResult{}, err
		}
	}

	// Optional additional git config via env knob (defaults OFF).
	cfgParts := applyGitConfigParts()

	if req.Preflight {
		checkArgs := []string{"apply", "--check"}
		if req.Revert {
			checkArgs = append(checkArgs, "-R")
		}
		checkArgs = append(checkArgs, patchPath)
		rendered := renderCommandForLog(gitRoot, cfgParts, checkArgs)
		code, out, errOut, runErr := runApplyGit(gitRoot, cfgParts, checkArgs)
		if runErr != nil {
			return ApplyGitResult{}, runErr
		}
		applied, skipped, conflicted := ParseGitApplyOutput(out, errOut)
		return ApplyGitResult{
			ExitCode:        code,
			AppliedPaths:    applied,
			SkippedPaths:    skipped,
			ConflictedPaths: conflicted,
			Stdout:          out,
			Stderr:          errOut,
			CmdForLog:       rendered,
		}, nil
	}

	args := []string{"apply", "--3way"}
	if req.Revert {
		args = append(args, "-R")
	}
	args = append(args, patchPath)

	cmdForLog := renderCommandForLog(gitRoot, cfgParts, args)
	code, out, errOut, runErr := runApplyGit(gitRoot, cfgParts, args)
	if runErr != nil {
		return ApplyGitResult{}, runErr
	}

	applied, skipped, conflicted := ParseGitApplyOutput(out, errOut)
	return ApplyGitResult{
		ExitCode:        code,
		AppliedPaths:    applied,
		SkippedPaths:    skipped,
		ConflictedPaths: conflicted,
		Stdout:          out,
		Stderr:          errOut,
		CmdForLog:       cmdForLog,
	}, nil
}

// applyGitConfigParts reads the CODEXGO_APPLY_GIT_CFG env var and converts each
// comma-separated `key=value` pair into `-c key=value` argument fragments.
//
// Mirrors the Rust handling of `CODEXGO_APPLY_GIT_CFG`.
func applyGitConfigParts() []string {
	cfg, ok := os.LookupEnv("CODEXGO_APPLY_GIT_CFG")
	if !ok {
		return nil
	}
	parts := make([]string, 0)
	for _, pair := range strings.Split(cfg, ",") {
		p := strings.TrimSpace(pair)
		if p == "" || !strings.Contains(p, "=") {
			continue
		}
		parts = append(parts, "-c", p)
	}
	return parts
}

// resolveGitRoot returns the repository top-level directory for cwd by running
// `git rev-parse --show-toplevel`.
//
// Mirrors the Rust `resolve_git_root`.
func resolveGitRoot(cwd string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		code := -1
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
			stderr = string(ee.Stderr)
		}
		return "", fmt.Errorf("not a git repository (exit %d): %s", code, stderr)
	}
	return strings.TrimSpace(string(out)), nil
}

// writeTempPatch writes the diff to a fresh temporary directory and returns the
// directory and the patch file path. The caller is responsible for removing the
// directory.
//
// Mirrors the Rust `write_temp_patch`.
func writeTempPatch(diff string) (dir string, patchPath string, err error) {
	dir, err = os.MkdirTemp("", "codexgo-patch-")
	if err != nil {
		return "", "", fmt.Errorf("create temp dir for patch: %w", err)
	}
	patchPath = filepath.Join(dir, "patch.diff")
	if err := os.WriteFile(patchPath, []byte(diff), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", "", fmt.Errorf("write patch file: %w", err)
	}
	return dir, patchPath, nil
}

// runApplyGit runs `git <cfg> <args>` in cwd, returning the exit code and the
// captured stdout/stderr. Output is decoded as UTF-8 (invalid bytes are
// replaced), mirroring the Rust `String::from_utf8_lossy`.
//
// Mirrors the Rust `run_git` (in apply.rs).
func runApplyGit(cwd string, gitCfg, args []string) (code int, stdout, stderr string, err error) {
	full := make([]string, 0, len(gitCfg)+len(args))
	full = append(full, gitCfg...)
	full = append(full, args...)

	cmd := exec.Command("git", full...)
	cmd.Dir = cwd
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	runErr := cmd.Run()
	code = 0
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			return -1, "", "", fmt.Errorf("run git apply: %w", runErr)
		}
	}
	return code, lossyUTF8(outBuf.String()), lossyUTF8(errBuf.String()), nil
}

// lossyUTF8 returns s with any invalid UTF-8 sequences replaced by U+FFFD,
// matching Rust's `String::from_utf8_lossy`.
func lossyUTF8(s string) string {
	return strings.ToValidUTF8(s, "�")
}

// StagePaths stages only the files referenced by the diff that actually exist on
// disk (as a symlink or regular entry) under gitRoot. Staging is best-effort: a
// non-zero `git add` exit is not treated as an error.
//
// Mirrors the Rust `stage_paths`.
func StagePaths(gitRoot, diff string) error {
	paths := ExtractPathsFromPatch(diff)
	existing := make([]string, 0, len(paths))
	for _, p := range paths {
		joined := filepath.Join(gitRoot, p)
		if _, err := os.Lstat(joined); err == nil {
			existing = append(existing, p)
		}
	}
	if len(existing) == 0 {
		return nil
	}

	args := append([]string{"add", "--"}, existing...)
	cmd := exec.Command("git", args...)
	cmd.Dir = gitRoot
	// Best-effort: ignore non-zero exit and capture errors only for process
	// start failures.
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return nil
		}
		return fmt.Errorf("stage paths: %w", err)
	}
	return nil
}

// quoteShell renders a single argument for the human-readable command log,
// quoting when the value contains characters outside the simple set.
//
// Mirrors the Rust `quote_shell`.
func quoteShell(s string) string {
	// Rust's `chars().all(...)` is vacuously true for the empty string, so an
	// empty argument is treated as "simple" and returned as-is.
	simple := true
	for _, c := range s {
		if !isSimpleShellChar(c) {
			simple = false
			break
		}
	}
	if simple {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func isSimpleShellChar(c rune) bool {
	if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
		return true
	}
	return strings.ContainsRune("-_.:/@%+", c)
}

// renderCommandForLog builds a copy-pasteable `(cd <dir> && git ...)` string for
// logging.
//
// Mirrors the Rust `render_command_for_log`.
func renderCommandForLog(cwd string, gitCfg, args []string) string {
	parts := make([]string, 0, len(gitCfg)+len(args)+1)
	parts = append(parts, "git")
	for _, a := range gitCfg {
		parts = append(parts, quoteShell(a))
	}
	for _, a := range args {
		parts = append(parts, quoteShell(a))
	}
	return fmt.Sprintf("(cd %s && %s)", quoteShell(cwd), strings.Join(parts, " "))
}
