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
| 03 | execpolicy (Starlark) | 🟡 | matcher complete; network_rule/amend deferred |
| 04 | config (config.toml) | ✅ | load/merge/precedence/profiles; some sub-schemas as opaque trees |
| 05 | features | ✅ | |
| 06 | api client (HTTP/SSE/WS) | ✅ | Responses request + SSE verified via turn parity |
| 07 | model providers & catalog | ✅ | model catalog slug-identical to codex |
| 08 | login & auth | 🟡 | OAuth/API-key/keyring/JWT; AWS SigV4 stub, device-auth pending |
| 09 | shell parsing & PTY | ✅ | |
| 10 | exec-server & filesystem | ✅ | |
| 11 | apply_patch | ✅ | **byte-identical** to codex (parity) |
| 12 | sandbox core + macOS seatbelt | ✅ | .sbpl gen + spawn |
| 13 | Linux sandbox (landlock+seccomp) | ✅ | native, cgo-free; cross-compiles (behavioral test linux-only) |
| 14 | Windows sandbox (restricted token) | ✅ | native; cross-compiles (behavioral test windows-only) |
| 15 | network proxy (SOCKS5/HTTP) | 🟡 | policy+env exact; HTTPS MITM data-path deferred |
| 16 | tools framework + built-ins | ✅ | shell_command/apply_patch verified drop-in |
| 17 | rollout JSONL | ✅ | |
| 18 | state SQLite | ✅ | embedded migrations, pure-Go sqlite |
| 19 | thread store / history / graph | ✅ | read/list/search/archive; search substring vs ripgrep |
| 20 | git / file-search / watch | 🟡 | go-git/fsnotify; fuzzy ranking best-effort |
| 21 | MCP client | ✅ | stdio+http, namespacing |
| 22 | plugins & marketplace | 🟡 | manifest/install/list; remote-sync orchestration deferred |
| 23 | skills | ✅ | |
| 24 | hooks | ✅ | |
| 25 | code-mode (JS) | ✅ | goja engine; JS-feature gaps vs V8 documented |
| 26 | extensions/connectors/memories | 🟡 | guardian/goal/memories/imagegen/websearch; connectors-in-core partial |
| 27–31 | core engine | 🟡 | turn loop / streaming / tools / approvals / compaction / assembly run end-to-end; advanced breadth (multi-agent/unified-exec/realtime/goals-deep) ported as packages, not all wired into the live loop |
| 32 | app-server protocol | ✅ | JSON-RPC registry, v1/v2 |
| 33 | app-server + transport | 🟡 | stdio/uds/ws + turn-driving methods; full method surface partial |
| 34 | `codex exec` (headless) | ✅ | **turn JSONL byte-identical** to codex (text + tool turns) |
| 35 | mcp-server | ✅ | stdio v2 + v1 compat |
| 36–40 | TUI | 🟡 | bubbletea chat/overlays/slash/keymap/onboarding; behavioral port, not pixel-identical |
| 41–42 | CLI + arg0 + aux | 🟡 | full subcommand set matches codex; app/update/remote-control are notices; completion not byte-identical to clap |
| 43 | cloud features | 🟡 | cloud-tasks/requirements/backend; connectors omitted |
| 44 | telemetry & feedback | ✅ | analytics(opt-out)/otel/feedback |
| 45 | secrets / proxy / context | ✅ | age store, responses proxy, install/term ctx |
| 46 | OSS providers (ollama/lmstudio) | ✅ | |
| 47 | realtime webrtc | 🟡 | pion session; audio backend pluggable |
| 48 | parity validation | 🟡 | automated differential suite (6/6 surfaces); broader scenarios ongoing |

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
| `doctor --json` | 🟡 18 check IDs + container/keys match; per-check `details` shape differs |
| `completion` | 🟡 functional; not clap-byte-identical |

## Honest overall

Breadth: all 49 specs build; the headless agent runs and is a verified drop-in for
the core flows (text + tool turns). Remaining toward a literal 100%: broader
behavioral differentials (reasoning/multi-step/error paths), TUI pixel-fidelity,
wiring core's advanced features into the live loop, and the documented long-tail
deviations. Rough faithful-and-verified completeness: **~60%**.
