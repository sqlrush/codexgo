#!/bin/zsh
# Run the MCP-touching test packages on the Mac host (shared filesystem; the
# Linux VM must not build Go artifacts here — see airush dev-environment memo).
set -eu
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
export GOFLAGS=-mod=readonly
cd /Users/sqlrush/codexgo
go test -race ./pkg/mcp/... ./internal/cli/... ./pkg/core/... "$@"
