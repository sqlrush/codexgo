# 13 — Linux Sandbox (landlock + seccomp, native)

| | |
|---|---|
| **Phase** | 3 — Execution & sandboxing |
| **Status** | Not started |
| **Depends on** | 12 |
| **Size** | XL |
| **Drop-in critical** | ★ (sandbox guarantees) |

## 目标 / Goal
Reimplement the Linux sandbox **natively in Go** — no shelling out to `bwrap` or the
Rust `codex-linux-sandbox` binary. Provide the same filesystem/network confinement
Codex achieves via bubblewrap (namespaces) and landlock+seccomp.

## 源参考 / Source reference
- `reference-codex/codex-rs/sandboxing/src/bwrap.rs` (bwrap arg construction,
  path-specificity ordering, glob masking, symlink handling).
- `reference-codex/codex-rs/linux-sandbox/` (landlock ruleset, seccomp network
  filter), `bwrap/` (bundled bwrap — we replace, not bundle).

## 功能需求 / Functional requirements
1. **Namespaces** (native via `clone`/`unshare`, `golang.org/x/sys/unix`):
   user, PID, mount, network (when no proxy). Replicate bubblewrap's mount plan:
   global read-only root (`/`), bind writable roots, re-apply protected subpaths
   (`.git`, `.codex`) as read-only under writable parents, fresh `/proc` (skip in
   containers), path-specificity ordering (narrower wins).
2. **Landlock** filesystem ruleset via `golang.org/x/sys/unix` landlock syscalls
   (read/write/execute access rights per path); legacy-landlock fallback path.
3. **Seccomp** BPF network filter (block new `AF_UNIX`/network syscalls after proxy
   bridge is live); construct the same filter Codex applies.
4. **Network proxy mode**: internal TCP↔UDS↔TCP bridge to the per-turn proxy
   (spec 15); seccomp tightening after routing established.
5. Glob masking for unreadable globs; symlink-in-path and missing-first-component
   blocked (mount over `/dev/null` equivalent). Detect/handle WSL1 (unsupported) vs
   WSL2.

## 验收方案 / Acceptance criteria
- Behavioral differential (Linux CI): for a battery of operations (write outside
  root, read protected path, open network socket with/without proxy, follow
  symlink out of jail), `codexgo` and `codex` produce the same allow/deny outcome.
- Writable-root + protected-subpath layout matches Codex (a file under
  `<root>/.git` is read-only even though `<root>` is writable).
- With proxy active, only the proxy endpoint is reachable; direct egress blocked.
- Container detection skips `/proc` remount exactly as Codex does.

## 风险与难点 / Risks
- This is the single riskiest spec: reimplementing bubblewrap's mount/namespace
  setup in Go without the bwrap binary is intricate (correct ordering, error paths,
  capability handling). Budget generously.
- Landlock ABI versions vary by kernel; replicate Codex's ABI negotiation.
- seccomp BPF construction is low-level; validate against Codex's filter program.

## 非目标 / Non-goals
- macOS/Windows backends (12/14). The proxy implementation (15).
