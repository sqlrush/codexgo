# 07 — Model Providers & Catalog

| | |
|---|---|
| **Phase** | 2 — Model API & auth |
| **Status** | Not started |
| **Depends on** | 04, 06 |
| **Size** | M |
| **Drop-in critical** | ★ (provider config + model cache format) |

## 目标 / Goal
Port `codex-model-provider`, `codex-model-provider-info`, and `codex-models-manager`:
the provider registry/abstraction, built-in providers, wire-API routing, and the
model catalog with on-disk cache.

## 源参考 / Source reference
- `reference-codex/codex-rs/model-provider-info/src/` (`ModelProviderInfo`,
  built-ins).
- `reference-codex/codex-rs/model-provider/src/` (auth resolution, request routing).
- `reference-codex/codex-rs/models-manager/src/` (catalog, `RefreshStrategy`,
  `models_cache.json`).

## 功能需求 / Functional requirements
1. Built-in providers with exact IDs/URLs: `openai`
   (`https://api.openai.com/v1`, `requires_openai_auth=true`), `amazon-bedrock`
   (AWS SigV4), `ollama` (`http://localhost:11434`), `lmstudio`. Validation that
   `aws`/`env_key`/bearer/`requires_openai_auth` are mutually exclusive.
2. `ModelProviderInfo` → `ApiProvider` conversion: headers, env headers, query
   params, retry config, stream/ws timeouts, `wire_api` (`responses`; reject
   `chat` with the migration error message).
3. Auth resolution chain selecting Bearer / ChatGPT tokens / AWS SigV4 /
   agent-identity (delegates to spec 08).
4. `ModelsManager`: `Online` / `Offline` / `OnlineIfUncached` strategies; merge
   bundled `models.json` with remote catalog; 300s TTL; ETag conditional refresh;
   filter by auth mode (Codex backend requires ChatGPT login).

## 验收方案 / Acceptance criteria
- Built-in provider table matches a captured `codex debug models --bundled` /
  provider dump exactly (IDs, URLs, defaults).
- `models_cache.json` written by `codexgo` round-trips through Codex and vice versa
  (golden file).
- Provider validation rejects the same invalid combinations Codex rejects.
- `wire_api="chat"` produces the same deserialization error/message.

## 风险与难点 / Risks
- AWS SigV4 signing must match (`aws-sdk-go-v2` signer over the same canonical
  request + body hash).
- Catalog merge ordering and ETag semantics must match for cache hits.

## 非目标 / Non-goals
- OSS provider lifecycle (version checks / model pull) — spec 46.
