# 36 — TUI Foundation (render core, event loop)

| | |
|---|---|
| **Phase** | 9 — TUI |
| **Status** | Not started |
| **Depends on** | 33 |
| **Size** | XL |
| **Drop-in critical** | partial (UX parity) |

## 目标 / Goal
Establish the TUI foundation: terminal abstraction, the event loop, an immediate-mode
rendering layer equivalent to ratatui, ANSI handling, and the app-server-client wiring
— the base on which the chat experience (37) is built.

## 源参考 / Source reference
- `reference-codex/codex-rs/tui/src/tui.rs` (terminal abstraction over crossterm),
  `app.rs` (event loop), `app_event.rs`/`app_event_sender.rs` (internal event bus).
- `reference-codex/codex-rs/ansi-escape/src/` (`ansi_escape_line`),
  `tui/src/terminal_palette.rs`/`terminal_probe.rs` (capability detection).

## 功能需求 / Functional requirements
1. Terminal abstraction: raw mode, alternate screen, frame scheduling, event polling
   (keyboard/resize/paste), enhanced keycode detection, OSC 8 hyperlinks; built on
   bubbletea + a custom immediate-mode render pass (or a minimal ratatui-equivalent
   buffer differ) to preserve Codex's per-frame redraw model.
2. Capability detection: color depth (`NO_COLOR`/`COLORTERM=truecolor`/heuristics),
   link support, Unicode width — matching `terminal_palette`/`terminal_probe`.
3. Internal event bus (`AppEvent`/sender) decoupling widgets from the `App`.
4. App-server-client wiring (spec 33): drive the engine, receive notifications,
   render events.
5. ANSI parsing that preserves color/bold/dim across reflow.

## 验收方案 / Acceptance criteria
- Renders a static frame to an in-memory buffer matching a snapshot for a fixture
  app state (snapshot tests, like Codex's `insta`-based TUI tests).
- Capability detection matches Codex across a matrix of `TERM`/`COLORTERM`/`NO_COLOR`.
- Resize triggers a correct re-render without corruption.
- Connects to a `codexgo` app-server and receives/renders an event.

## 风险与难点 / Risks
- **No drop-in for ratatui.** bubbletea is Elm-style/retained; reproducing ratatui's
  immediate-mode + fine-grained `buffer_mut()` control requires a custom render layer.
  This is the architectural crux of the whole TUI and the biggest single risk in the
  project alongside the Linux sandbox.
- Snapshot parity with Codex's TUI is hard; target behavioral + layout equivalence,
  document cosmetic deviations.

## 非目标 / Non-goals
- Chat widgets/markdown (37), overlays (38), slash commands (39).
