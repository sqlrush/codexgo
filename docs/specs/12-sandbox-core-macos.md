# 12 — Sandbox Core + macOS Seatbelt

| | |
|---|---|
| **Phase** | 3 — Execution & sandboxing |
| **Status** | Not started |
| **Depends on** | 02, 10 |
| **Size** | L |
| **Drop-in critical** | ★ (sandbox policy semantics) |

## 目标 / Goal
Port `codex-sandboxing` (the platform-agnostic coordinator) plus the **macOS
seatbelt** backend: translate `SandboxPolicy` + `AskForApproval` into a concrete
confinement and run a command under `sandbox-exec` with a generated `.sbpl` policy.

## 源参考 / Source reference
- `reference-codex/codex-rs/sandboxing/src/` (`manager.rs` `SandboxType` /
  transform request, `seatbelt.rs`, embedded `.sbpl` policies, network policy).
- `reference-codex/docs/sandbox.md`.

## 功能需求 / Functional requirements
1. `SandboxPolicy` model: `FileSystemSandboxPolicy` (per-path read/write/denied),
   `NetworkSandboxPolicy`, writable roots, protected subpaths (`.git`, `.codex`).
2. The **AskForApproval × SandboxPolicy matrix**: `SandboxablePreference`
   Auto/Require/Forbid, `effective_permission_profile()`,
   `should_require_platform_sandbox()` — choose backend or fail.
3. macOS seatbelt: generate `.sbpl` from policy (deny-by-default + per-path
   allow/deny rules), embed the base/network/read-only-defaults policies identical
   to Codex, extract loopback proxy ports from `*_PROXY_URL` env, restrict UDS to
   proxy paths, and exec via `/usr/bin/sandbox-exec -f <policy> <cmd>`.
4. Common spawn path used by all backends (env, cwd, PTY, cancellation).

## 验收方案 / Acceptance criteria
- Generated `.sbpl` for a set of policies matches Codex's generated policy
  text byte-for-byte (golden).
- Matrix decisions (which backend / approval requirement) match Codex across the
  full Auto/Require/Forbid × policy grid.
- On macOS: a command that writes outside writable roots is denied; reads of
  protected paths denied; loopback proxy reachable — same as Codex (behavioral
  differential).

## 风险与难点 / Risks
- The embedded `.sbpl` policies are large string assets; copy verbatim and snapshot.
- Behavioral tests are macOS-only; gate in CI accordingly.

## 非目标 / Non-goals
- Linux (13) / Windows (14) backends; the network proxy itself (15).
