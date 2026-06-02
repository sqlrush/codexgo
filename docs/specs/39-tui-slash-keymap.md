# 39 — TUI Slash Commands, Keymap, Vim & Themes

| | |
|---|---|
| **Phase** | 9 — TUI |
| **Status** | Not started |
| **Depends on** | 37 |
| **Size** | L |
| **Drop-in critical** | ★ (slash command set + keymap config) |

## 目标 / Goal
Port the full slash-command set, the layered keymap (incl. vim mode), themes, and the
interaction behaviors (Ctrl+C handling, scrolling, copy, raw mode).

## 源参考 / Source reference
- `reference-codex/codex-rs/tui/src/slash_command.rs` (~60 variants),
  `app.rs::handle_slash_command`, `keymap.rs`, `motion.rs` (vim), `theme_picker.rs`,
  `pager_overlay.rs`.
- `reference-codex/docs/slash_commands.md`.

## 功能需求 / Functional requirements
1. **Slash commands** (full 0.136.0 set), incl.: `/model`, `/permissions`, `/keymap`,
   `/vim`, `/approve`, `/review`, `/new`, `/compact`, `/plan`, `/goal`, `/agent`,
   `/resume`, `/fork`, `/archive`, `/rename`, `/init`, `/clear`, `/theme`,
   `/sandbox-add-read-dir`, `/setup-default-sandbox`, `/experimental`, `/memories`,
   `/skills`, `/hooks`, `/mcp`, `/plugins`, `/settings`, `/personality`, `/copy`,
   `/raw`, `/diff`, `/mention`, `/status`, `/transcript`, `/btw`, `/ps`, `/stop`,
   `/feedback`, `/logout`, `/quit`, `/exit`, `/ide`, `/side`, `/realtime`,
   debug commands (`/debug-config`, `/rollout`, `/statusline`, `/title`, …).
   Each wired to the right picker/RPC/config mutation.
2. **Keymap**: layered contexts (`global`, `app`, `chat`, `composer`, `editor`,
   `vim_normal`, `vim_operator`, `vim_text_object`, `pager`, `list`, `approval`)
   loaded from `[tui].keymap`; canonical key-spec parsing (`ctrl+s`, `alt+shift+n`).
3. **Vim mode**: normal/insert/operator/text-object motions (hjkl, w/b, d/c, text
   objects) emulated client-side; toggle via `/vim` or keybind.
4. **Themes**: `[tui].theme`, `/theme` picker with live preview; `statusline_format`,
   `color_status_line_with_theme`.
5. Interactions: Ctrl+C double-press quit (with footer hint), copy, `/raw`
   scrollback toggle, pager scrolling, mouse disabled (as in Codex).

## 验收方案 / Acceptance criteria
- The slash-command set + ordering matches Codex (golden against `slash_command.rs`
  enum / `slash_commands.md`).
- `[tui].keymap` config parsing matches Codex (golden); a custom binding takes effect
  in the right context.
- Vim motions behave per a scripted motion test matching Codex outcomes.
- `/theme` selection persists to config (spec 04) preserving formatting.

## 风险与难点 / Risks
- The command set is large and evolves; pin to 0.136.0.
- Vim emulation is fiddly; cover with a motion test matrix.

## 非目标 / Non-goals
- The popups themselves (38); the underlying features (各自 spec).
