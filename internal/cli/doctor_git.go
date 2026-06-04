package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

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

	var gitVersion string
	if gitFound {
		if out, err := exec.CommandContext(ctx, gitPath, "--version").Output(); err == nil {
			gitVersion = strings.TrimSpace(string(out))
			b.detail(fmt.Sprintf("git version: %s", gitVersion))
		}
	}

	root, repoDetected := gitutils.GetGitRepoRoot(cwd)
	b.detail(fmt.Sprintf("repo detected: %t", repoDetected))
	if repoDetected {
		b.detail(fmt.Sprintf("repo root: %s", root))
		if branch, ok := gitutils.CurrentBranchName(ctx, cwd); ok {
			b.detail(fmt.Sprintf("git branch: %s", branch))
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
