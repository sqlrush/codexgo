# 34 — Headless `exec` Mode

| | |
|---|---|
| **Phase** | 8 — Headless server & exec |
| **Status** | Not started |
| **Depends on** | 33 |
| **Size** | M |
| **Drop-in critical** | ★★ (`codex exec` JSONL output) |

## 目标 / Goal
Port `codex-exec`: the non-interactive `codex exec` mode for scripting/CI. This is
the **first end-to-end usable milestone** — a full agent turn with tools/sandbox,
driven headlessly, with Codex-compatible output.

## 源参考 / Source reference
- `reference-codex/codex-rs/exec/src/` (`cli.rs` flags,
  `event_processor_with_jsonl_output.rs`, in-process client usage, resume/review).
- `reference-codex/docs/exec.md`.

## 功能需求 / Functional requirements
1. Run an agent turn via the in-process app-server client (spec 33) from a prompt
   (arg/stdin) and an optional image input.
2. Flags: `--json` (JSONL, one event per line), `--output-last-message FILE`,
   `--output-schema FILE` (constrain response), default human-readable text (final
   message to stdout). Subcommands `resume`, `review`.
3. JSONL event model: transform `ServerNotification`s into the exec `ThreadEvent`
   types (`ThreadStartedEvent`, `TurnStartedEvent`, `CommandExecutionItem`,
   `PatchApplyStatus`, `TodoListItem`, `ThreadErrorEvent`, …) with the same
   `{type, id, timestamp, …}` shape; final status `success`/`failed`/`interrupted`.
4. Exit codes matching Codex (`exit_status` conventions; signals as 128+n on Unix).

## 验收方案 / Acceptance criteria
- Differential: `codexgo exec --json "<prompt>"` over a mocked model produces the
  same JSONL event stream (line-by-line, after canonicalization) as `codex exec`.
- `--output-last-message` / `--output-schema` behave identically.
- Exit codes match across success/failure/interrupt scenarios.
- Human-readable (non-json) output is equivalent (allowing for cosmetic spacing
  documented as deviations).

## 风险与难点 / Risks
- The exec `ThreadEvent` mapping is a distinct surface from raw `EventMsg`; capture
  both and map precisely.
- Deterministic timestamps in fixtures (inject a clock) for golden comparisons.

## 非目标 / Non-goals
- Interactive TUI (Phase 9); cloud `exec` variants beyond local.
