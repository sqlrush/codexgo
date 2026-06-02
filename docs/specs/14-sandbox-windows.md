# 14 — Windows Sandbox (restricted token)

| | |
|---|---|
| **Phase** | 3 — Execution & sandboxing |
| **Status** | Not started |
| **Depends on** | 12 |
| **Size** | XL |
| **Drop-in critical** | ★ (sandbox guarantees) |

## 目标 / Goal
Reimplement the Windows sandbox natively in Go: restricted-token / ACL-based
filesystem confinement plus ConPTY desktop isolation, matching `windows-sandbox-rs`.

## 源参考 / Source reference
- `reference-codex/codex-rs/windows-sandbox-rs/src/` (`acl.rs` deny-read resolver,
  restricted token caps, ConPTY private desktop, `WindowsSandboxLevel`).

## 功能需求 / Functional requirements
1. Build a restricted/low-integrity token: drop privileges, apply capability
   restrictions, set integrity level per `WindowsSandboxLevel`.
2. **Deny-read ACLs**: resolve denied paths and apply ACL entries so the child
   cannot read them (primary mechanism), while keeping writable roots accessible.
3. **ConPTY** allocation with optional private desktop isolation
   (`windows_sandbox_private_desktop`).
4. Spawn the command under the restricted token + ConPTY, stream output, propagate
   exit/cancellation. Use `golang.org/x/sys/windows`.

## 验收方案 / Acceptance criteria
- Behavioral differential (Windows CI): write outside writable root denied; read of
  a denied path denied; reads within allowed roots succeed — same as Codex.
- `WindowsSandboxLevel` strictness tiers map to the same effective restrictions.
- ConPTY output capture equivalent to Codex on scripted commands.

## 风险与难点 / Risks
- Go's `x/sys/windows` is less ergonomic than the Rust `windows` crate; some ACL /
  token APIs may need additional syscalls via `unsafe`/`syscall`.
- Private-desktop isolation and session handling are intricate; validate on real
  Windows, not just Wine.
- ConPTY requires Windows 10+; document minimum version (matches Codex).

## 非目标 / Non-goals
- macOS/Linux backends; the network proxy (15).
