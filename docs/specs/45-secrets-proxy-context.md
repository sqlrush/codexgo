# 45 — Secrets, Responses Proxy & Install/Terminal Context

| | |
|---|---|
| **Phase** | 10 — CLI & peripheral |
| **Status** | Not started |
| **Depends on** | 04 |
| **Size** | M |
| **Drop-in critical** | partial (secret store + proxy) |

## 目标 / Goal
Port `codex-secrets`, `codex-responses-api-proxy`, `codex-install-context`, and
`codex-terminal-detection`: the age-encrypted secret store, the minimal Responses API
forward proxy, install-method detection, and terminal detection.

## 源参考 / Source reference
- `reference-codex/codex-rs/secrets/src/` (age encryption, `SecretName`/`SecretScope`,
  `redact_secrets`, keyring service `codex`).
- `responses-api-proxy/src/` (tiny_http forward proxy), `install-context/src/`,
  `terminal-detection/src/`.

## 功能需求 / Functional requirements
1. **secrets**: age-encrypted key/value store (`filippo.io/age`) with `LocalSecretsBackend`
   (file) + keyring backend; `SecretName` validation (`A-Z0-9_`), `SecretScope`
   (Global/Environment), `redact_secrets()` for logs. Storage location/format
   compatible with Codex.
2. **responses-api-proxy** (`codex responses-api-proxy`): forward proxy for
   `/v1/responses` with auth-header rewriting; flags `--port`, `--server-info FILE`
   (port/pid JSON), `--upstream-url`, `--http-shutdown`, `--dump-dir`. (Distinct from
   the sandbox network proxy, spec 15.)
3. **install-context**: detect install method (Standalone/Npm/Bun/Brew/Other) from
   exe path/env, package layout discovery.
4. **terminal-detection**: identify terminal/program/multiplexer/version
   (`TERM`/`TERM_PROGRAM`/`TMUX`/`ZELLIJ_VERSION`).

## 验收方案 / Acceptance criteria
- A secret written by `codexgo` decrypts in Codex and vice versa (age round-trip,
  same store location); `redact_secrets` masks the same patterns.
- responses-api-proxy forwards a request with rewritten auth and writes the same
  `--server-info` JSON shape.
- Install-method + terminal detection match Codex on a fixture matrix.

## 风险与难点 / Risks
- age key management/identity must match how Codex derives/stores keys to interop.
- Two proxies exist (15 vs 45) — keep them clearly separated.

## 非目标 / Non-goals
- The sandbox network proxy (15).
