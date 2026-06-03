// Package tui is a faithful Go port of the codex 0.136.0 terminal UI
// (codex-rs/tui, originally ratatui-based) built on bubbletea + lipgloss +
// bubbles in the Elm architecture.
//
// The port targets behavioral parity (the same widgets, slash commands,
// keybindings, streaming behavior) rather than pixel-exact layout parity. Where
// a cosmetic deviation is unavoidable it is documented inline.
//
// # Foundation (this set of files)
//
// This file group is the TUI spine that the area-specific widgets plug into:
//
//   - [Model], [Model.Update], [Model.View]: the root bubbletea program, mapping
//     onto the Rust App/ChatWidget split.
//   - [AppEvent] and [AppEventSender]: the internal event bus that decouples
//     widgets from the top-level app loop (port of app_event.rs /
//     app_event_sender.rs). Bubbletea delivers AppEvents as tea.Msg values.
//   - [AppCommand]: typed outbound commands forwarded to the engine
//     (port of app_command.rs).
//   - The engine wiring in engine.go drives an
//     [appserverclient.InProcessAppServerClient]: it submits user turns and
//     streams the engine's `codex/event` notifications back into the model as
//     [CoreEventMsg] values.
//   - Terminal capability detection (capabilities.go), color/palette helpers
//     (color.go, palette.go), ANSI escape handling (ansi_escape.go), and a
//     lipgloss theme layer loadable from config [tui].theme (theme.go).
//   - The view-region layout in layout.go defines the transcript area + bottom
//     pane split and the [TranscriptView]/[BottomPane] seams area agents
//     implement.
//
// Everything lives in a single package so sibling files added by other agents
// can reference these identifiers without qualification.
package tui
