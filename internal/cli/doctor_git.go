package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/internal/gitutils"
)

// gitCheck reports the git environment: the selected git executable, its version,
// and whether the working directory is inside a repository. It mirrors
// git.environment in doctor.rs. A repo detected without a usable git executable
// degrades to a warning.
func gitCheck(ctx context.Context) doctorCheck {
	b := newCheck("git.environment", "git")
	cwd := resolveCwd()

	gitPath, lookErr := exec.LookPath("git")
	gitFound := lookErr == nil
	if gitFound {
		b.detail(fmt.Sprintf("selected git: %s", gitPath))
	} else {
		b.detail("selected git: not found")
	}

	// PATH git candidates: every distinct git executable resolvable on PATH,
	// mirroring git_candidates() in git.rs.
	candidates := gitCandidates()
	b.detail(fmt.Sprintf("PATH git entries: %d", len(candidates)))
	for i, path := range candidates {
		b.detail(fmt.Sprintf("PATH git #%d: %s", i+1, path))
	}

	var gitVersion string
	if gitFound {
		if out := gitOutput(ctx, gitPath, cwd, "--version"); out != "" {
			gitVersion = out
			b.detail(fmt.Sprintf("git version: %s", gitVersion))
		}
		if out := gitOutput(ctx, gitPath, cwd, "--exec-path"); out != "" {
			b.detail(fmt.Sprintf("git exec path: %s", out))
		}
		if out := gitOutput(ctx, gitPath, cwd, "version", "--build-options"); out != "" {
			b.detail(fmt.Sprintf("git build options: %s", out))
		}
	}

	root, repoDetected := gitutils.GetGitRepoRoot(cwd)
	b.detail(fmt.Sprintf("repo detected: %t", repoDetected))
	if repoDetected {
		b.detail(fmt.Sprintf("repo root: %s", root))
		b.detail(fmt.Sprintf(".git entry: %s", gitEntrySummary(root)))
		if branch, ok := gitutils.CurrentBranchName(ctx, cwd); ok && branch != "" {
			b.detail(fmt.Sprintf("git branch: %s", normalizedBranch(branch)))
		}
	}

	switch {
	case gitFound && gitVersion == "":
		b.warn("Git executable found but could not be run").
			remedy("Fix the selected Git executable or PATH so Codex can inspect Git metadata.")
	case !gitFound && repoDetected:
		b.warn("Git repository detected but git executable was not found").
			remedy("Install Git or fix PATH so Codex can inspect repository metadata.")
	case gitVersion != "":
		b.ok(gitVersion)
	default:
		b.ok("git environment inspected")
	}
	return b.build()
}

// gitCommandTimeout bounds each git probe so the doctor stays fast, mirroring
// GIT_COMMAND_TIMEOUT in git.rs.
const gitCommandTimeout = 2 * time.Second

// gitCandidates returns every distinct git executable resolvable on PATH,
// mirroring git_candidates() in git.rs (which::which_all, deduplicated).
func gitCandidates() []string {
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		return nil
	}
	exeName := "git"
	if runtime.GOOS == "windows" {
		exeName = "git.exe"
	}
	var out []string
	seen := map[string]struct{}{}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, exeName)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if _, dup := seen[candidate]; dup {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

// gitOutput runs git with args in cwd and returns its trimmed stdout with lines
// joined by "; ", mirroring git_output + command_output_text in git.rs. It returns
// "" on failure, empty output, or timeout. GIT_OPTIONAL_LOCKS=0 keeps the probe
// from taking repository locks.
func gitOutput(ctx context.Context, gitPath, cwd string, args ...string) string {
	probeCtx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, gitPath, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return strings.Join(lines, "; ")
}

// gitEntrySummary describes the repository's .git entry (directory, file ->
// gitdir, missing, etc.), mirroring git_entry_summary in git.rs.
func gitEntrySummary(repoRoot string) string {
	entry := filepath.Join(repoRoot, ".git")
	info, err := os.Stat(entry)
	switch {
	case err == nil && info.IsDir():
		return "directory"
	case err == nil && info.Mode().IsRegular():
		contents, readErr := os.ReadFile(entry)
		if readErr == nil {
			if path, ok := strings.CutPrefix(string(contents), "gitdir:"); ok {
				return fmt.Sprintf("file -> %s", strings.TrimSpace(path))
			}
		}
		return "file"
	case err == nil:
		return "other"
	case os.IsNotExist(err):
		return "missing"
	default:
		return fmt.Sprintf("unreadable (%v)", err)
	}
}

// normalizedBranch maps a detached "HEAD" to "detached HEAD", mirroring
// normalized_branch in git.rs.
func normalizedBranch(branch string) string {
	if branch == "HEAD" {
		return "detached HEAD"
	}
	return branch
}
