# 46 — OSS Providers (Ollama, LM Studio)

| | |
|---|---|
| **Phase** | 10 — CLI & peripheral |
| **Status** | Not started |
| **Depends on** | 07 |
| **Size** | S |
| **Drop-in critical** | partial |

## 目标 / Goal
Port `codex-ollama` and `codex-lmstudio`: the local OSS provider lifecycle (version
checks, model pull) on top of the provider abstraction (spec 07).

## 源参考 / Source reference
- `reference-codex/codex-rs/ollama/src/` (version check ≥0.13.4, model pull,
  Responses-compat layer).
- `reference-codex/codex-rs/lmstudio/src/`.

## 功能需求 / Functional requirements
1. **ollama**: detect local server (`http://localhost:11434`), version check
   (≥0.13.4) before using the Responses-compat path; model pull/list; streaming
   adaptation.
2. **lmstudio**: detect local server, model discovery, Responses-compat path.
3. Surface OSS mode through config/CLI like Codex (`--oss`/provider selection,
   `utils/oss`).

## 验收方案 / Acceptance criteria
- Version check gates usage with the same threshold/message as Codex.
- Model list/pull behaviors match against a mock Ollama/LM Studio server.
- OSS provider selection routes requests correctly through spec 06/07.

## 风险与难点 / Risks
- Ollama/LM Studio APIs evolve; pin to what 0.136.0 expects.

## 非目标 / Non-goals
- The provider/catalog core (07); generic API client (06).
