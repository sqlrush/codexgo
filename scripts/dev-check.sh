#!/bin/zsh
# codexgo：build + vet + test 指定包（默认 ./...）。在 Mac 上执行；airush 侧经 ssh 调用。
# 用法：scripts/dev-check.sh [pkg ...]     例：scripts/dev-check.sh ./internal/threadstore/... ./internal/rollout/...
set -eu
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
export GOFLAGS=-mod=readonly
cd /Users/sqlrush/codexgo
pkgs=("$@"); [ ${#pkgs[@]} -eq 0 ] && pkgs=(./...)
gofmt -l ./internal ./cmd | head -20
go build ./... && go vet "${pkgs[@]}" && go test -count=1 "${pkgs[@]}" 2>&1 | grep -vE "^ok|no test files" | tail -40
echo "DEV-CHECK OK (${pkgs[*]})"
