# 25 — Code Mode (JS runtime)

| | |
|---|---|
| **Phase** | 6 — Extensibility |
| **Status** | Not started |
| **Depends on** | 16 |
| **Size** | L |
| **Drop-in critical** | partial (tool behavior) |

## 目标 / Goal
Port `codex-code-mode`: the embedded JavaScript runtime ("code mode") that lets the
model execute JS which can call other Codex tools. Upstream uses V8 (rusty_v8 / deno
core); reimplement with a Go JS engine.

## 源参考 / Source reference
- `reference-codex/codex-rs/code-mode/src/` (v8 runtime, `exec`/`wait` tools,
  cell isolation, `RuntimeResponse`).
- `reference-codex/codex-rs/v8-poc/` (V8 proof-of-concept; reference only).

## 功能需求 / Functional requirements
1. Two tools: `exec(source)` — run JS and yield outputs; `wait(duration_ms)` —
   async delay.
2. **Nested tool calls**: JS code can invoke other Codex tools; bridge the JS host
   functions to the tool router (spec 16).
3. Cell-based session isolation (`CellId`): per-invocation result scoping, persistent
   cell state within a session as Codex does.
4. Output capture: text, structured JSON, and images via `RuntimeResponse`.

## 验收方案 / Acceptance criteria
- `exec` runs representative JS programs producing the same outputs/results as Codex
  for a fixture set (deterministic programs).
- Nested tool calls from JS reach the tool router and return results identically.
- Cell isolation/state persistence behaves like Codex across multiple `exec` calls.

## 风险与难点 / Risks
- Engine choice: `dop251/goja` (pure Go, no cgo, ES5.1+/partial ES6) vs
  `rogchap.com/v8go` (real V8, cgo). Goja keeps the binary cgo-free (preferred) but
  may lack APIs some code-mode programs use — evaluate against upstream test
  programs and document any JS feature gaps as deviations.
- The deno/V8 host API surface exposed to JS must be reproduced for nested calls.

## 非目标 / Non-goals
- General sandboxing of JS beyond what code-mode itself provides.
