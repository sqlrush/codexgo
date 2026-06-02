# 15 — Network Proxy (HTTP / SOCKS5, native)

| | |
|---|---|
| **Phase** | 3 — Execution & sandboxing |
| **Status** | Not started |
| **Depends on** | 02 |
| **Size** | L |
| **Drop-in critical** | ★ (proxy policy + env contract) |

## 目标 / Goal
Reimplement `codex-network-proxy` natively in Go: the per-turn HTTP + SOCKS5 proxy
that gives sandboxed commands policy-controlled network access, replacing the
Rust `rama`-based stack.

## 源参考 / Source reference
- `reference-codex/codex-rs/network-proxy/src/` (`NetworkProxyConfig`,
  `NetworkMode`, `NetworkPolicyDecider`, MITM hooks, audit metadata, blocked-request
  observer).

## 功能需求 / Functional requirements
1. HTTP(S) forward proxy + SOCKS5 proxy listeners on loopback; emit the proxy URL
   into the standard env vars consumed by the sandbox/command: `HTTP_PROXY`,
   `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`, `PROXY_ACTIVE`, and the macOS git-ssh
   marker `CODEX_PROXY_GIT_SSH_COMMAND_MARKER`.
2. **Policy enforcement** (`NetworkPolicyDecider`): allow/deny by domain/host and
   Unix-socket path per `NetworkSandboxPolicy`; default-deny posture matching Codex.
3. **MITM hooks** (`MitmHookConfig`): header injection / response modification points
   with the same configuration surface.
4. Audit metadata + blocked-request observer for logging policy violations.
5. Integrate with the sandbox bridge (spec 13's TCP↔UDS↔TCP path).

## 验收方案 / Acceptance criteria
- Policy decisions (allow/deny per domain & socket) match Codex on a fixture policy
  set (differential).
- Env vars exported to the child process match Codex exactly (names + URL format).
- A blocked request is observed/logged with equivalent audit metadata.
- HTTP and SOCKS5 paths both proxy successfully to an allowed upstream.

## 风险与难点 / Risks
- `rama` provides a lot for free; in Go this is `net/http` + a SOCKS5 implementation
  + a TLS MITM layer — more assembly required.
- TLS interception (if used) needs a generated CA matching how Codex injects trust.

## 非目标 / Non-goals
- The `responses-api-proxy` (spec 45) is a separate, simpler forward proxy.
