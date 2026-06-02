# 37 — TUI Chat (composer, history, markdown)

| | |
|---|---|
| **Phase** | 9 — TUI |
| **Status** | Not started |
| **Depends on** | 36 |
| **Size** | XL |
| **Drop-in critical** | partial (UX parity) |

## 目标 / Goal
Port the core chat surface: the input composer, the transcript/history cells, the
streaming markdown renderer, syntax highlighting, and the diff viewer.

## 源参考 / Source reference
- `reference-codex/codex-rs/tui/src/chatwidget/` (`mod.rs`, `streaming.rs`),
  `history_cell/`, `exec_cell/`.
- `tui/src/bottom_pane/chat_composer.rs`, `markdown_render.rs`,
  `markdown_stream.rs`, `render/highlight.rs`, `diff_render.rs`,
  `transcript_reflow.rs`, `terminal_hyperlinks.rs`.

## 功能需求 / Functional requirements
1. **Composer**: multiline input with history (up/down + Ctrl+R search),
   `@mention`/`@file` expansion (spec 20), image paste (clipboard → base64 PNG),
   submit/queue-while-running, bracketed paste.
2. **History cells**: render user/agent/exec/tool-call/reasoning cells; live
   in-flight cell with streaming; exec cells stream output with progress.
3. **Streaming markdown**: incremental render via a goldmark-based streaming wrapper
   (commit on newline boundaries), reflow on width change without losing state.
4. **Syntax highlighting**: chroma-based bridge (syntect/two-face analog) with
   theme + palette quantization (truecolor/256/16/mono); load custom themes from
   `$CODEX_HOME/themes/`.
5. **Diff viewer**: unified diff with line numbers, theme-aware add/delete
   backgrounds (`diff_render`).
6. **Transcript overlay** (Ctrl+T): committed cells + cached live tail.

## 验收方案 / Acceptance criteria
- Snapshot tests: rendering representative conversations (user msg, agent markdown,
  code blocks, exec output, diff) matches layout snapshots (parity with Codex's
  rendered output for the same input, modulo documented cosmetic deviations).
- Streaming render commits at newline boundaries and reflows correctly on resize.
- `@file` search popup expands to full paths in the submission.
- Image paste produces the same base64 PNG payload as Codex (spec 01).

## 风险与难点 / Risks
- syntect→chroma scope/theme differences will cause minor color deltas; document
  deviations; consider porting Codex's bundled themes.
- Newline-gated streaming markdown has no Go library; build the wrapper carefully.
- This + spec 36 are the bulk of the ~201K-line TUI; realistically multi-month.

## 非目标 / Non-goals
- Overlays/pickers (38), slash commands/keymap/vim (39).
