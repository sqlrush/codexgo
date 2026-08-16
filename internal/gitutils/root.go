package gitutils

import (
	"github.com/sqlrush/codexgo/internal/gitutils/gitroot"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// GetGitRepoRoot returns the repository root for baseDir. See
// [gitroot.GetGitRepoRoot].
func GetGitRepoRoot(baseDir string) (string, bool) { return gitroot.GetGitRepoRoot(baseDir) }

// ResolveRootGitProjectForTrust resolves the repository root used for project
// trust decisions. See [gitroot.ResolveRootGitProjectForTrust].
func ResolveRootGitProjectForTrust(cwd abspath.AbsolutePathBuf) (abspath.AbsolutePathBuf, bool) {
	return gitroot.ResolveRootGitProjectForTrust(cwd)
}

// FindGitCheckoutRoot finds the nearest enclosing checkout root. See
// [gitroot.FindGitCheckoutRoot].
func FindGitCheckoutRoot(cwd abspath.AbsolutePathBuf) (abspath.AbsolutePathBuf, bool) {
	return gitroot.FindGitCheckoutRoot(cwd)
}
