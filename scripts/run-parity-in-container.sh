#!/bin/zsh
# 在容器里跑 parity 差分套件。
#
# 为什么必须在容器里：基线二进制是容器编出来的 linux/amd64（宿主机不装 Rust 是
# 前提），macOS 执行不了它，所以比对的两侧都放进 Linux。
#
# Go 的构建缓存与模块缓存走命名卷，绝不落进共享的 /Users/sqlrush 树——那棵树同时
# 被 macOS 使用，跨架构产物互相污染是踩过的坑。
set -eu
export PATH="/usr/local/bin:/opt/homebrew/bin:$PATH"

REPO=/Users/sqlrush/codexgo
BIN=$REPO/.parity-bin/codex-0.136.0-linux-amd64
GOVER=${GOVER:-1.26}

[ -x "$BIN" ] || { echo "ERROR: 基线二进制不存在，先跑 build-parity-binary.sh" >&2; exit 1; }

docker run --rm \
  -v "$REPO":/repo \
  -v codexgo-go-build:/gocache \
  -v codexgo-go-mod:/gomodcache \
  -w /repo \
  -e GOFLAGS=-mod=readonly \
  -e GOCACHE=/gocache \
  -e GOMODCACHE=/gomodcache \
  -e CODEX_PARITY_BIN=/repo/.parity-bin/codex-0.136.0-linux-amd64 \
  "golang:${GOVER}" \
  bash -c '
    /repo/.parity-bin/codex-0.136.0-linux-amd64 --version
    go test ./internal/paritytest/ "$@"
  ' -- "$@"
