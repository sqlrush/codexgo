# 06 — API Client (HTTP / SSE / WebSocket)

| | |
|---|---|
| **Phase** | 2 — Model API & auth |
| **Status** | Not started |
| **Depends on** | 02 |
| **Size** | L |
| **Drop-in critical** | ★ (Responses API request/event shapes) |

## 目标 / Goal
Port `codex-client` + `codex-api`: the low-level HTTP transport (retries, custom
CA, compression) and the high-level Responses API request builder and streaming
event parser (SSE + WebSocket).

## 源参考 / Source reference
- `reference-codex/codex-rs/client/src/` (`request.rs`, retry, TLS, reqwest wrapper).
- `reference-codex/codex-rs/api/src/` (`common.rs` `ResponsesApiRequest`,
  `sse/responses.rs`, `provider.rs` `RetryPolicy`, `auth.rs` `AuthProvider`).

## 功能需求 / Functional requirements
1. HTTP client: configurable base URL/headers/query params, `RequestCompression`
   (none/Zstd), custom CA (`CODEX_CUSTOM_CA`), Cloudflare cookie handling for the
   ChatGPT backend, request timeout.
2. `RetryPolicy`: defaults `request_max_retries=4`, `stream_max_retries=5`, caps at
   100; exponential backoff; retry on 429 / 5xx / transport; honor `Retry-After`.
3. **Responses API request** (`POST /v1/responses`): build the exact JSON shape —
   `model`, `instructions`, `input[]` (ResponseItem), `tools`, `tool_choice`,
   `parallel_tool_calls`, `reasoning{effort,summary}`, `store`, `stream`,
   `include`, `service_tier`, `prompt_cache_key`, `text{verbosity,format}`,
   `client_metadata`.
4. **SSE streaming** parse (`text/event-stream`): `response.created`,
   `response.output_item.added/done`, `response.output_text.delta`,
   `response.output_tool_call_input.delta`, `response.reasoning_summary.delta`,
   `response.completed` (with `usage`), `response.metadata`; idle timeout
   (`stream_idle_timeout_ms`, default 300s); parse `X-RateLimit-*`, `X-Request-ID`,
   `OpenAI-Model`, `X-Models-Etag` headers into a `RateLimitSnapshot`/metadata.
5. **WebSocket** streaming where `supports_websockets`: `http→ws`/`https→wss`
   upgrade, connect timeout (`websocket_connect_timeout_ms`, default 15s).
6. `AuthProvider` interface: Bearer, AWS SigV4, no-op (implementations wired in
   specs 07/08).

## 验收方案 / Acceptance criteria
- Golden: serialized `ResponsesApiRequest` for captured inputs is byte-identical to
  Codex's outgoing body (against a recording proxy capture).
- Replay a captured SSE stream → emitted internal events match a reference event
  log (order + payloads).
- Retry/backoff schedule matches Codex for simulated 429/5xx (timing within
  tolerance; attempt count exact).
- Rate-limit header parsing matches captured response headers.

## 风险与难点 / Risks
- Go has no built-in SSE; implement a robust line parser with idle-timeout via
  `context`.
- Zstd request compression must be byte-compatible (`klauspost/compress/zstd`).
- WebSocket Responses transport may need server cooperation; gate behind capability.

## 非目标 / Non-goals
- Provider selection / model catalog (spec 07); credential acquisition (spec 08).
