#!/bin/zsh
# 在容器里编译 codex 0.136.0 的 parity 基线二进制，不在宿主机装任何 Rust 工具链。
#
# 产物是 linux/amd64 可执行文件，因此 parity 差分套件也要在容器里跑（见
# run-parity-in-container.sh）——宿主机 macOS 跑不了 Linux 二进制，这是容器化
# 编译的必然代价，换来的是宿主机零污染 + 结果可复现。
set -eu
export PATH="/usr/local/bin:/opt/homebrew/bin:$PATH"

SRC=/Users/sqlrush/codexgo/reference-codex/codex-rs
OUT=/Users/sqlrush/codexgo/.parity-bin
TOOLCHAIN=$(sed -n 's/^channel = "\(.*\)"/\1/p' "$SRC/rust-toolchain.toml")

[ -d "$SRC" ] || { echo "ERROR: 找不到 0.136 源码 worktree: $SRC" >&2; exit 1; }
mkdir -p "$OUT"

echo "==> building codex with rust $TOOLCHAIN in a container (host stays clean)"
docker run --rm \
  -v "$SRC":/src:ro \
  -v "$OUT":/out \
  -v codexgo-cargo-registry:/usr/local/cargo/registry \
  -v codexgo-cargo-target:/target \
  -w /work \
  -e CARGO_TARGET_DIR=/target \
  "rust:${TOOLCHAIN}-bookworm" \
  bash -c '
    set -e
    apt-get update -qq && apt-get install -y -qq pkg-config libssl-dev >/dev/null
    cp -r /src/. /work/
    cargo build --release --bin codex
    cp /target/release/codex /out/codex-0.136.0-linux-amd64
    /out/codex-0.136.0-linux-amd64 --version
  '

echo "==> binary at $OUT/codex-0.136.0-linux-amd64"
