# Implementation Status

Accurate per-spec state of the codexgo port (the `Status:` lines inside individual
`docs/specs/NN-*.md` files predate implementation and are not maintained — this is
the live record). Pairs with [`DEVIATIONS.md`](../DEVIATIONS.md) (intentional
divergences) and [`docs/PARITY.md`](PARITY.md) (differential evidence vs real
codex 0.136.0).

Legend: ✅ implemented + tested · 🟡 implemented with documented deviation/partial ·
⏳ not started.

## Spec status

| # | Spec | Status | Note |
|--:|------|:--:|------|
| 00 | foundation & parity harness | ✅ | module/CI + `internal/paritytest` golden + differential helpers |
| 01 | foundation utils | ✅ | 19 packages |
| 02 | protocol (Op/EventMsg/…) | ✅ | drop-in JSON |
| 03 | execpolicy (Starlark) | 🟡 | matcher complete; **network_rule landed** (builtin + rule type + protocol/decision aliasing + host normalization + compiled allow/deny domains + merge_overlay); **compiled domains now wired into the network proxy** (ApplyExecPolicyNetworkRules port of apply_exec_policy_network_rules: upsert after config entries, exec-policy wins per host); amend deferred |
| 04 | config (config.toml) | ✅ | load/merge/precedence/profiles; some sub-schemas as opaque trees |
| 05 | features | ✅ | |
| 06 | api client (HTTP/SSE/WS) | ✅ | Responses request + SSE verified via turn parity |
| 07 | model providers & catalog | ✅ | model catalog slug-identical to codex |
| 08 | login & auth | 🟡 | OAuth/API-key/keyring/JWT; device-auth wired (device_code_auth.rs port: usercode/token endpoints, polling state machine, byte-identical prompt + messages, auth.json persist); **`--experimental_issuer`/`--experimental_client-id` now parsed + threaded into the device-code ServerOptions** (browser flow keeps defaults, per Rust parity); AWS SigV4 stub |
| 09 | shell parsing & PTY | ✅ | |
| 10 | exec-server & filesystem | ✅ | |
| 11 | apply_patch | ✅ | **byte-identical** to codex (parity) |
| 12 | sandbox core + macOS seatbelt | ✅ | .sbpl gen + spawn; LIVE per-turn policy resolution for exec (read-only/workspace-write -> seatbelt, danger -> none) |
| 13 | Linux sandbox (landlock+seccomp) | ✅ | native, cgo-free; cross-compiles (behavioral test linux-only) |
| 14 | Windows sandbox (restricted token) | ✅ | native; cross-compiles (behavioral test windows-only) |
| 15 | network proxy (SOCKS5/HTTP) | 🟡 | policy+env exact; HTTPS MITM data-path wired (certs.rs+mitm.rs port: per-install CA + per-host leaf minting, CONNECT TLS termination, inner-request policy/hooks, streaming both ways); body-inspection logging is a no-op upstream (MITM_INSPECT_BODIES=false) |
| 16 | tools framework + built-ins | ✅ | shell_command/apply_patch verified drop-in; UnifiedExec PTY pair (exec_command/write_stdin) wired with per-model selection |
| 17 | rollout JSONL | ✅ | |
| 18 | state SQLite | ✅ | embedded migrations, pure-Go sqlite; thread-goal store (goals.rs port: budget-limit promotion + accounting modes) |
| 19 | thread store / history / graph | ✅ | read/list/search/archive; search now scans rollout transcripts (search_threads.rs port: snippet gate, excerpts, cursor paging) |
| 20 | git / file-search / watch | 🟡 | go-git/fsnotify; fuzzy ranking best-effort |
| 21 | MCP client | ✅ | stdio+http, namespacing; defer_loading tools register as deferred runtimes ranked by tool_search BM25 |
| 22 | plugins & marketplace | 🟡 | manifest/install/list; **remote-sync orchestration landed** — curated-plugins startup sync (git→GitHub-HTTP→export-archive fallback chain, sha-based update decision, atomic swap) + configured-git-marketplace upgrade (per-marketplace revision-compare update decision, failure isolation, atomic activation+metadata) wired into `codex plugin marketplace upgrade` |
| 23 | skills | ✅ | loader/render/system-install + LIVE `<skills_instructions>` injection (default roots; admin `/etc/codex/skills` always emitted; **project `.codex/skills` now gated on the ported git-trust decision**) |
| 24 | hooks | ✅ | |
| 25 | code-mode (JS) | ✅ | goja engine; JS-feature gaps vs V8 documented |
| 26 | extensions/connectors/memories | 🟡 | guardian/goal/memories/imagegen/websearch; goal tools now LIVE in the headless loop (SQLite-backed via state bridge; events/metrics no-op until registry wiring); connectors-in-core partial |
| 27–31 | core engine | 🟡 | turn loop / streaming / tools / approvals / compaction / assembly run end-to-end; unified-exec + goals + tool_search + LIVE collab execution (spawn/wait/send_input/resume/close) wired into the loop; realtime remains package-only |
| 32 | app-server protocol | ✅ | JSON-RPC registry, v1/v2 |
| 33 | app-server + transport | 🟡 | stdio/uds/ws + turn-driving methods; full method surface partial |
| 34 | `codex exec` (headless) | ✅ | **turn JSONL byte-identical** to codex (text + tool turns) |
| 35 | mcp-server | ✅ | stdio v2 + v1 compat |
| 36–40 | TUI | 🟡 | bubbletea chat/overlays/slash/keymap/onboarding; behavioral port. **Pixel-fidelity wave 3 landed** (idle first frame cell-identical incl. SGR). codexgo runs INLINE (no alt-screen): finalized history cells are printed into native scrollback via `tea.Println` (`ScrollbackDrainer`/`Model.withScrollback`) while the live viewport renders only the top inset + composer block. The composer matches codex's bordered-less `Min(3)` block (blank pad / `› <text-or-placeholder>` / blank pad) + default status-line footer `model-with-reasoning · current-dir`, with the random 8-entry placeholder pool mirrored. **Wave 3:** (A) the VT emulator (`vtgrid.go`) now RECORDS per-cell SGR attributes (fg/bg incl. 256/24-bit, bold/dim/italic/underline/reverse) and a second assertion layer compares them under `CODEX_TUI_FRAME_STRICT=2` — 0 cell-attribute mismatches after aligning codexgo's styling to codex's captured SGR (header `/model` = named cyan SGR 36; prompt `›` = bold-only; placeholder = bare DIM). (B) the real impersonated version `tui.CodexVersion=0.136.0` is threaded so the `(v0.136.0)` title row matches and is unmasked — **masks dropped to 3** (header dir, random placeholder, footer cwd; all per-run). (C) a DYNAMIC frame scenario (`tui_dynamic_test.go`) drives a scripted assistant turn through a fake `/v1/responses` server and diffs the post-reply frame, verifying the inline history-insertion path end-to-end. Tooltip disabled in the harness for both (network/time-volatile). **Wave 4 (dynamic frame now STRICT):** the three history-rendering gaps the wave-3 dynamic diff revealed are closed and the dynamic test now rides the SAME `CODEX_TUI_FRAME_STRICT=1/2` ladder as the idle frames (its separate `CODEX_TUI_DYNAMIC_STRICT` opt-in is removed) — 0 row + 0 cell-attribute mismatches at both 80×24 and 120×40, verified stable across 3+ consecutive strict-2 runs. (1) the submitted user message is echoed into scrollback as a `› <text>` user history cell, inserted TUI-side at submit time (`Model` → `TranscriptView.AppendUserMessage`, codex's `on_user_message_display`), with `display_lines` = leading blank / `› ` (bold+dim) marker + unstyled body / trailing blank; (2) the assistant message renders with a dim `• ` bullet prefix and two-space continuation indent (`AgentMarkdownCell` analogue, applied to streaming-tail lines too); (3) the user marker glyph is `› ` (was `> `) and plain markdown paragraph text now carries the terminal default foreground (codex's `Style::default()`, was theme fg `idx189`) so the agent body reads `·`. **Wave 5 (final TUI wave — /clear + footer theme colors):** (A) `/clear` is fully wired — `ClearUIEvent` → `Model.handleClearUI` emits the EXACT codex terminal-clear sequence `\x1b[r\x1b[0m\x1b[H\x1b[2J\x1b[3J\x1b[H` (DECSTBM reset + SGR reset + cursor-home + ED2 + ED3 scrollback-purge + cursor-home, captured byte-for-byte; matches `clear_scrollback_and_visible_screen_ansi`), resets the transcript (`ClearResettable`/`ChatTranscript.ResetForClear`) and re-seeds + re-drains the fresh session-header card (port of `reset_transcript_state_after_clear` + fresh-session header insertion). Enter on the slash popup now DISPATCHES the highlighted command (`composer.go dispatchSelectedSlash`, was text-completion only) so `/clear` fires. New `tui_clear_test.go` frame scenario: 0 row + 0 attribute mismatches at both sizes, strict 2, stable across 3 runs. (B) Footer theme colors — the default status line renders softened per-segment theme colors (`footer_color.go` ports `soften_status_line_color`, verified `soften(255,0,0)==(228,11,11)`): model `#f6e2b7`, path `#abdfa7`, separator dim, captured byte-for-byte at TrueColor. `ComposerFooter.styledLine` builds styled spans rendered via a TrueColor-pinned lipgloss renderer (ratatui serializes `Color::Rgb` as 24-bit unconditionally — no downsampling). `tui_footer_test.go` tightens the mask to the path span only and compares the footer PREFIX (model span + separator) glyph- and attribute-identically: 0 mismatches, both sizes, stable across 3 runs. Remaining: syntect TextMate theme resolution for non-default themes (default-theme accents are hardcoded from captured values) + resize-reflow frame tests |
| 41–42 | CLI + arg0 + aux | 🟡 | full subcommand set matches codex; app/update/remote-control are notices; `completion` byte-identical to clap for all 5 shells |
| 43 | cloud features | 🟡 | cloud-tasks/requirements/backend; connectors omitted |
| 44 | telemetry & feedback | ✅ | analytics(opt-out)/otel/feedback |
| 45 | secrets / proxy / context | ✅ | age store, responses proxy, install/term ctx |
| 46 | OSS providers (ollama/lmstudio) | ✅ | |
| 47 | realtime webrtc | 🟡 | pion session; audio backend pluggable |
| 48 | parity validation | 🟡 | automated differential suite (6/6 surfaces); broader scenarios ongoing |
| 49 | MCP forward-sync 0.147 (cluster A) | ✅ | needs 1-5: protocol negotiation, tool-catalog cache + non-blocking startup, schema shape preservation, startup retry + selective reconnect, OAuth refresh. Need 6 (tool-search exposure) split out |

## Parity scorecard (vs real codex 0.136.0 — `docs/PARITY.md`)

Automated, credential-free, binary-vs-binary (env-gated on `CODEX_PARITY_BIN`):

| Surface | Result |
|---|---|
| `debug models` catalog | ✅ slug-identical |
| subcommand set | ✅ exact |
| `apply_patch` envelope | ✅ byte-identical files + message |
| `exec --json` text turn | ✅ byte-identical normalized JSONL |
| `exec --json` shell-command tool turn | ✅ byte-identical (runs the command) |
| `exec --json` apply_patch tool turn | ✅ byte-identical + identical patched file |
| `exec --json` error turn | ✅ same terminal `turn.failed` + exit code + **byte-identical error message** (upstream body surfaced verbatim) |
| `exec -o/--output-last-message` | ✅ byte-identical file |
| `exec --output-schema` request `text` | ✅ byte-identical json_schema block |
| full `/responses` request body | ✅ **EVERY top-level field byte-identical** (`documentedGapFields` empty): scalars, `instructions`, full 11-tool `tools` registry, and `input` (per-run CODEX_HOME normalized) |
| `/responses` `input` context | ✅ permissions + **skills_instructions** (system skills materialized under skills/.system) + environment_context all byte-identical (`TestParityInputContext`) |
| built-in tool specs | ✅ **11/11 byte-identical, full-array order equality** (`TestParityToolSpecs` + `TestParityToolOrder`): UnifiedExec PTY pair, update_plan, goals trio (live SQLite store), request_user_input, apply_patch (custom grammar), view_image, tool_search (empty-entries dispatch until BM25/deferred registry), hosted web_search |
| `doctor --json` | ✅ 18 check IDs + **structured details object, 17/18 exact key sets** (1 probe-outcome-conditional row; best-effort value sources in DEVIATIONS "44 doctor") |
| `completion` | ✅ **byte-identical for all 5 shells** (bash/elvish/fish/powershell/zsh + default-is-bash) via `TestParityCompletion`; bash is a ported clap_complete v4.5.65 generator, the rest are vendored deterministic output |

## Honest overall

Breadth: all 49 specs build; the headless agent runs and is a verified drop-in for
the core flows (text + tool turns). The `/responses` REQUEST is now byte-identical
to codex for **every top-level field except `input`**: all scalars,
`instructions` (personality rendering), and the **complete 11-tool registry in
codex's exact spec_plan order** (full-array equality, every spec byte-identical).

**Tools registry is closed**: the UnifiedExec PTY pair rides the ported
per-model shell selection (`shell_type_for_model_and_features`); the goals trio
is gated like codex's `goal_tools_enabled` and backed by a real SQLite goal
store (`internal/state/goals.go`); `tool_search` is gated like
`append_tool_search_executor` with the ported spec renderer. Behavioral tails
are tracked in `DEVIATIONS.md` (tool_search returns the empty-entries result
until the deferred registry + BM25 land; goal events no-op in the headless
bridge).

**The plain-turn `/responses` request is now a FULL byte-level drop-in** —
`documentedGapFields` is empty; skills_instructions renders from the same
embedded system skills codex materializes under `CODEX_HOME/skills/.system`.

**tool_search dispatch is now byte-verified too** (`TestParityToolSearchTurn`):
the five multi_agent_v1 deferred specs, the BM25 engine (bm25 crate 2.3.2
semantics incl. the rust-stemmers Porter2 variant, validated against its full
29,417-word vocabulary), and the coalesced namespace output all match the real
binary — and the differential exposed a real codex nondeterminism (HashSet tie
order) documented in DEVIATIONS.

Long-tail wins this wave: the turn-error message is now byte-identical
(upstream HTTP body surfaced verbatim, enforced by `TestParityTurnError`), and
`terminal_interaction` echoes the originating exec_command call id.

Remaining toward a literal 100%:
- **git-trust gate landed** — the Rust loader's ProjectTrustContext decision is
  ported (`internal/config/trust.go`: `[projects."<key>"] trust_level` lookup
  with canonical+logical keys, dir→project-root→repo-root precedence,
  worktree-aware repo root). The headless host resolves it per-cwd and enables
  project `.codex/skills` (WithProjectLayer) only for trusted projects;
  untrusted/no-entry projects keep project layers off (parity-neutral: harness
  cwds carry no `.codex/skills`). Collab is fully live incl. full-history
  fork_context.
- Legacy workspace-write knobs + per-call sandbox_permissions (denial
  escalation is LIVE: prompt -> approved unsandboxed retry).
- TUI pixel-fidelity and the documented long-tail deviations (completion
  clap-bytes landed: all 5 shells byte-identical; doctor details: 17/18 exact).
Rough faithful-and-verified completeness: **~99%**.
