# 16 — Tools Framework & Built-in Tools

| | |
|---|---|
| **Phase** | 4 — Tools |
| **Status** | Not started |
| **Depends on** | 06, 10, 11 |
| **Size** | L |
| **Drop-in critical** | ★ (tool JSON schemas) |

## 目标 / Goal
Port `codex-tools`: the tool specification/registry model and the built-in tools the
model can call. Tool JSON schemas must match Codex so the model behaves identically.

## 源参考 / Source reference
- `reference-codex/codex-rs/tools/src/` (`ToolSpec`, `ResponsesApiTool`, `ToolName`,
  `ToolCall`, `ToolOutput`, MCP/dynamic adapters).
- `reference-codex/codex-rs/core/src/tools/handlers/` (`shell_spec.rs`,
  `apply_patch_spec.rs`, etc.) for the concrete schemas.

## 功能需求 / Functional requirements
1. `ToolSpec` model (Function / Namespace / ToolSearch / ImageGeneration /
   WebSearch / Freeform) serialized to the OpenAI Responses API tool format
   (`strict:false`, exact property schemas).
2. Built-in tools with exact input schemas:
   - **exec_command** — `{cmd, workdir?, shell?, tty?, yield_time_ms?,
     max_output_tokens?, login?, environment_id?}` (+ approval params when enabled);
     routes to exec-server/sandbox.
   - **apply_patch** — freeform grammar tool (spec 11).
   - **view_image** — `{path}`.
   - **update_plan** — the plan / todo-list management tool.
   - **request_user_input** — `{prompt, mode?}`.
   - **request_permissions** — approval-driven.
   - **web_search** — Responses-native (filters, allowed_domains, user_location,
     search_context_size).
   - **tool_search** — discovery namespace.
3. Tool routing context: `turn_id`, `call_id`, `tool_name` (namespace+name), model
   name, truncation policy; parallel vs sequential execution control per tool.
4. Adapters for MCP tools (spec 21), dynamic tools (persisted per thread), and
   code-mode augmentation (spec 25); deferred-loading (`defer_loading`).
5. `ToolOutput`/`AnyToolResult` success/error shapes streamed back to the model.

## 验收方案 / Acceptance criteria
- Golden: serialized tool specs match a captured Codex tool list (per model/feature
  set) byte-for-byte after canonicalization.
- Each built-in tool's input schema matches the captured schema exactly.
- Tool result serialization (success + error) matches captured `ResponseItem`
  tool outputs.
- Parallel/sequential execution policy per tool matches Codex.

## 风险与难点 / Risks
- Schemas vary by model + feature flags; capture across the relevant combinations.
- Web search is server-hosted; parity is about the request shape, not results.

## 非目标 / Non-goals
- The agent loop that decides when to call tools (spec 28).
