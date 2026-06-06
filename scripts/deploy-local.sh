#!/bin/sh
# deploy-local.sh — build codexgo at the version in ./VERSION and install it
# to /opt/homebrew/bin/codexgo for local verification.
#
# Release flow (maintainer-decided 2026-06-06):
#   1. make changes
#   2. bump ./VERSION (semver: feature -> minor, fix -> patch)
#   3. commit locally + run this script
#   4. USER verifies the deployed binary
#   5. only after verification: git push (and tag v<VERSION>)
set -eu

cd "$(dirname "$0")/.."

VERSION=$(tr -d '[:space:]' < VERSION)
if [ -z "$VERSION" ]; then
    echo "error: VERSION file is empty" >&2
    exit 1
fi
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)
TARGET=${CODEXGO_INSTALL_PATH:-/opt/homebrew/bin/codexgo}

echo "building codexgo v${VERSION} (${COMMIT}) -> ${TARGET}"
go build -trimpath \
    -ldflags "-s -w -X github.com/sqlrush/codexgo/internal/cli.Version=${VERSION} -X github.com/sqlrush/codexgo/internal/cli.BuildCommit=${COMMIT}" \
    -o "${TARGET}" ./cmd/codex

"${TARGET}" --version
echo "deployed. verify, then push + tag v${VERSION}."
