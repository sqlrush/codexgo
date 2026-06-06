#!/bin/sh
# rollback-local.sh — build a TAGGED version and install it locally, WITHOUT
# touching the current branch or working tree (uses a throwaway git worktree).
#
# Usage:
#   ./scripts/rollback-local.sh v0.2.2     # roll the deployed binary back
#   ./scripts/rollback-local.sh            # list available version tags
#
# This only swaps the deployed binary. The repo stays on your branch, so you
# can keep developing while running a known-good version. To also roll the
# SOURCE back, use normal git (e.g. `git revert` or a new branch from the tag).
set -eu

cd "$(dirname "$0")/.."

if [ $# -lt 1 ]; then
    echo "available versions:"
    git tag -l 'v*' | sort -V
    echo "usage: $0 <tag>" >&2
    exit 1
fi

TAG=$1
if ! git rev-parse -q --verify "refs/tags/${TAG}" >/dev/null; then
    echo "error: tag ${TAG} not found. available:" >&2
    git tag -l 'v*' | sort -V >&2
    exit 1
fi

VERSION=${TAG#v}
COMMIT=$(git rev-parse --short "${TAG}^{commit}")
TARGET=${CODEXGO_INSTALL_PATH:-/opt/homebrew/bin/codexgo}
WORKTREE=$(mktemp -d /tmp/codexgo-rollback.XXXXXX)

cleanup() {
    git worktree remove --force "${WORKTREE}" >/dev/null 2>&1 || true
    rm -rf "${WORKTREE}" 2>/dev/null || true
}
trap cleanup EXIT

echo "building ${TAG} (${COMMIT}) in a throwaway worktree..."
git worktree add --detach "${WORKTREE}" "${TAG}" >/dev/null

(
    cd "${WORKTREE}"
    go build -trimpath \
        -ldflags "-s -w -X github.com/sqlrush/codexgo/internal/cli.Version=${VERSION} -X github.com/sqlrush/codexgo/internal/cli.BuildCommit=${COMMIT}" \
        -o "${TARGET}" ./cmd/codex
)

"${TARGET}" --version
echo "rolled deployed binary back to ${TAG}. repo branch/tree untouched."
