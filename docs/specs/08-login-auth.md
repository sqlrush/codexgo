# 08 — Login & Authentication

| | |
|---|---|
| **Phase** | 2 — Model API & auth |
| **Status** | Not started |
| **Depends on** | 04, 06 |
| **Size** | L |
| **Drop-in critical** | ★★ (`auth.json` + OAuth flow) |

## 目标 / Goal
Port `codex-login`, `codex-keyring-store`, `codex-agent-identity`, and
`codex-aws-auth`: ChatGPT OAuth (PKCE) login, API-key auth, the `auth.json`
credential file, system keyring storage, token refresh, and delegated agent
identity. Must interoperate with credentials written by Codex.

## 源参考 / Source reference
- `reference-codex/codex-rs/login/src/` (OAuth server, token exchange/refresh,
  `AuthDotJson`, `CodexAuth`).
- `reference-codex/codex-rs/keyring-store/`, `agent-identity/`, `aws-auth/`.
- `reference-codex/docs/authentication.md`.

## 功能需求 / Functional requirements
1. **ChatGPT OAuth (PKCE)**: local callback server on port **1455** (fallback
   1457), issuer `https://auth.openai.com`, client_id
   `app_EMoamEEZ73f0CkXaXp7hrann`, S256 challenge + state, browser open, code→token
   exchange at `/oauth/token`. Honor `CODEX_REFRESH_TOKEN_URL_OVERRIDE` /
   `CODEX_REVOKE_TOKEN_URL_OVERRIDE`.
2. **`auth.json`** (exact JSON): `auth_mode`, `OPENAI_API_KEY`,
   `tokens{ id_token{email,chatgpt_plan_type,chatgpt_account_id,raw_jwt},
   access_token, refresh_token, account_id }`, `last_refresh`, `agent_identity`.
   Unix file mode `0600`.
3. **Token storage modes**: `File` (`~/.codex/auth.json`), `Keyring`
   (service `Codex Auth`, key = `SHA256(codex_home)[:16]`), `Auto` (keyring then
   file), `Ephemeral`. Same selection logic as `auth_credentials_store`.
4. **Refresh lifecycle**: refresh at 5-minute pre-expiry window; parse JWT exp;
   distinguish expired/reused/revoked/account-mismatch with matching messages;
   revoke old tokens unless opted out.
5. **API key auth**: `OPENAI_API_KEY`/`CODEX_API_KEY`/`CODEX_ACCESS_TOKEN` and
   provider `env_key`.
6. **agent-identity**: decode signed JWT (runtime id, private key, account, plan,
   FedRAMP) for managed/delegated agent mode.
7. **aws-auth**: SigV4 credential resolution for Bedrock.

## 验收方案 / Acceptance criteria
- `auth.json` written by `codexgo` is accepted by Codex and vice versa (golden
  round-trip incl. field order via canonicalizer).
- OAuth flow completes end-to-end against a mock issuer; PKCE challenge/state match
  RFC 7636 and Codex's parameters.
- Keyring entry written by Codex is readable by `codexgo` (where keyring backend
  available); `Auto` fallback to file matches Codex.
- Refresh triggers at the same threshold; error classification matches captured
  cases.

## 风险与难点 / Risks
- Keyring backends differ per OS (`go-keyring`); the service/key derivation must
  match exactly to share entries.
- JWT claims are parsed *without* verification (JWKS may be offline); use an
  unverified decode path.

## 非目标 / Non-goals
- MCP server OAuth (spec 21) — separate credential store (`oauth_credentials_store`).
