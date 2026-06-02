# 38 — TUI Overlays (approval, pickers, popups)

| | |
|---|---|
| **Phase** | 9 — TUI |
| **Status** | Not started |
| **Depends on** | 37 |
| **Size** | L |
| **Drop-in critical** | partial (approval UX) |

## 目标 / Goal
Port the transient overlay/popup system: the approval/permission modal, list pickers
(model/theme/permissions/skills/plugins), the `@file` search popup, the slash-command
popup, request-user-input, and MCP elicitation popups.

## 源参考 / Source reference
- `reference-codex/codex-rs/tui/src/bottom_pane/`: `mod.rs` (`BottomPaneView`
  stack), `approval_overlay.rs`, `list_selection_view.rs`, `file_search_popup.rs`,
  `command_popup.rs`, `request_user_input/`, `mcp_server_elicitation.rs`,
  `app_link_view.rs`, `footer.rs`, `key_hint.rs`.
- `utils/approval-presets/`.

## 功能需求 / Functional requirements
1. Dismissible overlay stack (`BottomPaneView`) layered over the chat.
2. **Approval overlay**: render pending exec/patch/permission requests (spec 30);
   approve/deny/approve-for-session/revise; approval presets.
3. **List pickers**: generic selection for models, themes, permission profiles,
   skills, plugins (filter-as-you-type, arrow/enter selection).
4. **`@file` popup**: fuzzy file search (spec 20) with highlight indices.
5. **Slash-command popup**: triggers on `/`, filters commands, completes (spec 39).
6. **request_user_input**: render prompt + options (`isOther`), capture answer →
   `Op::UserInputAnswer`.
7. **MCP elicitation / app-link**: credential/setup popups.
8. Footer hints + key-hint line reflecting available actions.

## 验收方案 / Acceptance criteria
- Approval overlay round-trips a request→decision producing the correct `Op`
  (differential against engine events from spec 30).
- Each picker selects and applies the right config/op (e.g. `/model` updates thread
  settings).
- `@file` popup result matches the search set (spec 20).
- request_user_input emits the same answer payload Codex does.

## 风险与难点 / Risks
- The approval overlay is large/stateful in Codex; reproduce its option set and
  keybindings faithfully.
- Overlay focus/stacking interactions with the composer must match.

## 非目标 / Non-goals
- The slash-command *handlers* (39); onboarding/login (40).
