#!/bin/zsh
# Run the parity differential suite against the real codex binary on the Mac.
set -eu
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
export GOFLAGS=-mod=readonly
export CODEX_PARITY_BIN="${CODEX_PARITY_BIN:-/Users/sqlrush/.local/bin/codex}"
cd /Users/sqlrush/codexgo

"$CODEX_PARITY_BIN" --version
go test ./internal/paritytest/ "$@"
