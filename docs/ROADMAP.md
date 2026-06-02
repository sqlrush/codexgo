# codexgo Roadmap

A phased plan to reimplement [OpenAI Codex](https://github.com/openai/codex)
**0.136.0** in Go with 100% feature parity and full drop-in compatibility.

- **Reference target:** `openai/codex` @ `rust-v0.136.0` (113 crates, ~935K LOC Rust)
- **Output:** this roadmap + 49 implementation specs (`docs/specs/00..48`)
- **Audience:** contributors picking up self-contained work units in dependency order

---

## 1. Vision & success definition

`codexgo` is "done" when a user can replace the `codex` binary with `codexgo` in an
existing `~/.codex` setup and observe **no behavioral difference**: same CLI surface,
same TUI experience, same config/auth/session files, same wire protocols, same
sandbox guarantees — implemented as a single dependency-free Go binary.

"100% parity" is operationalized as **three concentric layers of conformance**:

1. **Format parity** — every on-disk artifact (`config.toml`, `auth.json`, rollout
   JSONL, `history.jsonl`, SQLite schema, plugin/skill/hook manifests) is read and
   written byte-for-byte compatibly. Verified by golden-file round-trips against
   files produced by real Codex 0.136.0.
2. **Protocol parity** — every wire contract (`Op`/`Event` submission protocol,
   app-server JSON-RPC, MCP client/server, Responses API requests, `apply_patch`
   envelope) is bit-compatible. Verified by differential message capture.
3. **Behavioral parity** — the agent loop, tool execution, sandboxing, approvals,
   compaction, and TUI behave identically for the same inputs. Verified by
   end-to-end scenario tests run against both binaries.

## 2. Core principles (apply to every spec)

### 2.1 Differential testing drives drop-in compatibility
This is the central mechanism that makes "100% parity" *verifiable* rather than
aspirational. Spec `00` builds a **parity harness**:

- A **reference capture** tool drives the real Codex 0.136.0 binary (and reads the
  files/sockets it produces) to record golden fixtures: sample `config.toml`
  loads, `auth.json` shapes, rollout files, protocol message streams, `apply_patch`
  inputs/outputs, JSON-RPC exchanges, and tool JSON schemas.
- Every spec that owns a drop-in artifact ships **golden-file tests** asserting the
  Go implementation produces byte-identical output (or accepts Codex-produced input
  and re-emits it unchanged).
- Where exact byte order is impractical (e.g. Go's `encoding/json` map ordering),
  the spec defines a **canonicalizer** and asserts semantic equality, and documents
  the deviation explicitly. No silent divergence.

### 2.2 Native, dependency-free implementations
Per the project's fidelity decision, hard-to-port subsystems are reimplemented in
Go, not shelled out: landlock+seccomp via `golang.org/x/sys/unix`, macOS seatbelt
via `sandbox-exec` policy generation, Windows restricted-token ACLs via
`golang.org/x/sys/windows`, the network proxy in pure Go, "code mode" via a Go JS
runtime, and WebRTC via `pion`. Each carries elevated risk and is called out in its
spec.

### 2.3 Engineering standards
- **Immutability** — favor returning new values over in-place mutation.
- **Small, focused packages** — one responsibility per package; ~200–400 lines
  typical, 800 max; organize by domain, not by type.
- **Comprehensive error handling & boundary validation** — never swallow errors;
  validate all external input (config, API responses, file content, model output).
- **No hardcoded secrets**; honor the same env vars and credential stores as Codex.

### 2.4 Dependency-ordered execution
Specs are numbered to respect the upstream crate dependency graph (foundations →
contracts → engine → server → UI → CLI). Each spec lists its prerequisite specs.
Same-layer specs without interdependencies may proceed in parallel.

## 3. Phases

| Phase | Theme | Specs | Indicative size |
|------:|-------|-------|-----------------|
| 0 | Project foundation & parity harness | 00–01 | M |
| 1 | Wire protocol & configuration (contracts) | 02–05 | L |
| 2 | Model API client & auth | 06–08 | L |
| 3 | Command execution & sandboxing (native) | 09–15 | XL |
| 4 | Tools | 16 | L |
| 5 | Persistence | 17–20 | L |
| 6 | Extensibility (MCP/plugins/skills/hooks/code-mode/ext) | 21–26 | XL |
| 7 | Core agent engine (`codex-core`) | 27–31 | XL |
| 8 | Headless server & `exec` (first end-to-end usable) | 32–35 | L |
| 9 | TUI | 36–40 | XL |
| 10 | CLI surface & peripheral services | 41–47 | L |
| 11 | Hardening & full parity validation | 48 | M |

**First usable milestone:** end of Phase 8 — a headless `codexgo exec` and
app-server that can run a full agent turn with tools and sandboxing, drop-in
compatible with Codex clients.

## 4. Spec index

Legend — **★** = owns a drop-in-critical artifact (byte/protocol parity required).
Size is a rough T-shirt estimate (S/M/L/XL) of implementation effort.

| # | Spec | Phase | Depends on | Size | ★ |
|--:|------|:----:|------------|:----:|:-:|
| 00 | Project foundation & parity harness | 0 | — | M | |
| 01 | Foundation utilities (`utils/*`) | 0 | 00 | M | |
| 02 | Wire protocol (`codex-protocol`) | 1 | 01 | L | ★ |
| 03 | Exec policy engine (Starlark) | 1 | 01 | M | ★ |
| 04 | Configuration (`config.toml`) | 1 | 02,03 | L | ★ |
| 05 | Feature flags | 1 | 02 | S | |
| 06 | API client (HTTP/SSE/WebSocket) | 2 | 02 | L | ★ |
| 07 | Model providers & catalog | 2 | 04,06 | M | ★ |
| 08 | Login & auth (OAuth/API key/keyring) | 2 | 04,06 | L | ★ |
| 09 | Shell command parsing & PTY | 3 | 01 | M | |
| 10 | Exec server & filesystem ops | 3 | 02,09 | M | |
| 11 | `apply_patch` envelope | 3 | 01 | M | ★ |
| 12 | Sandbox core + macOS seatbelt | 3 | 02,10 | L | ★ |
| 13 | Linux sandbox (landlock+seccomp, native) | 3 | 12 | XL | ★ |
| 14 | Windows sandbox (restricted token) | 3 | 12 | XL | ★ |
| 15 | Network proxy (HTTP/SOCKS5, native) | 3 | 02 | L | ★ |
| 16 | Tools framework & built-in tools | 4 | 06,10,11 | L | ★ |
| 17 | Rollout JSONL persistence | 5 | 02 | M | ★ |
| 18 | State SQLite database | 5 | 02,17 | L | ★ |
| 19 | Thread store, history, agent graph | 5 | 17,18 | M | ★ |
| 20 | Git, file search, file watcher | 5 | 01 | M | |
| 21 | MCP client | 6 | 06,16 | L | ★ |
| 22 | Plugins & marketplace | 6 | 04,23 | L | ★ |
| 23 | Skills | 6 | 04 | M | ★ |
| 24 | Hooks | 6 | 04 | M | ★ |
| 25 | Code mode (JS runtime) | 6 | 16 | L | |
| 26 | Extensions, connectors, memories | 6 | 16,18 | L | ★ |
| 27 | Core: session/thread lifecycle | 7 | 04,06,16,17 | L | |
| 28 | Core: turn execution loop | 7 | 27 | XL | ★ |
| 29 | Core: context mgmt & auto-compaction | 7 | 28 | L | ★ |
| 30 | Core: approvals & guardian | 7 | 28 | M | |
| 31 | Core: API facade & manager assembly | 7 | 21–30 | M | |
| 32 | App-server protocol (JSON-RPC defs) | 8 | 02 | L | ★ |
| 33 | App-server, transport & daemon | 8 | 31,32 | L | ★ |
| 34 | Headless `exec` mode | 8 | 33 | M | ★ |
| 35 | MCP server (`codex mcp-server`) | 8 | 31,32 | M | ★ |
| 36 | TUI foundation (render core, event loop) | 9 | 33 | XL | |
| 37 | TUI chat (composer, history, markdown) | 9 | 36 | XL | |
| 38 | TUI overlays (approval, pickers, popups) | 9 | 37 | L | |
| 39 | TUI slash commands, keymap, vim, themes | 9 | 37 | L | ★ |
| 40 | TUI onboarding, status, resume picker | 9 | 37,08 | M | |
| 41 | CLI entry & arg0 multitool dispatch | 10 | 33,34 | M | ★ |
| 42 | Auxiliary subcommands (doctor, etc.) | 10 | 41 | M | ★ |
| 43 | Cloud features (cloud-tasks, requirements) | 10 | 06,08 | L | |
| 44 | Telemetry & feedback | 10 | 04 | M | |
| 45 | Secrets, responses proxy, install/term ctx | 10 | 04 | M | |
| 46 | OSS providers (Ollama, LM Studio) | 10 | 07 | S | |
| 47 | Realtime WebRTC voice mode (pion) | 10 | 06 | L | |
| 48 | Parity validation & release packaging | 11 | all | M | ★ |

## 5. How to use these specs

Each spec in `docs/specs/NN-<name>.md` follows a fixed template:

- **Header** — phase, status, prerequisite specs, size estimate.
- **目标 / Goal** — what the spec delivers.
- **源参考 / Source reference** — the upstream crate(s) and key files to study.
- **功能需求 / Functional requirements** — the behaviors to replicate.
- **验收方案 / Acceptance criteria** — concrete, testable checks, including the
  drop-in differential assertions (golden files / message capture).
- **风险与难点 / Risks** — Go porting hazards and candidate libraries.
- **非目标 / Non-goals** — explicitly out of scope (YAGNI).

Pick the lowest-numbered spec whose dependencies are all merged. Implement against
its acceptance criteria, wire its golden-file tests into CI, then move on.

## 6. Suggested Go dependency choices (non-binding)

These are starting points; each spec may refine or replace them.

| Concern | Upstream (Rust) | Candidate (Go) |
|---|---|---|
| TOML | `toml`/`toml_edit` | `github.com/pelletier/go-toml/v2` |
| CLI parsing | `clap` | `github.com/spf13/cobra` + `pflag` |
| SQLite | `sqlx` | `modernc.org/sqlite` (pure-Go, cgo-free) |
| Git | `gix` | `github.com/go-git/go-git/v5` |
| Fuzzy match | `nucleo` | custom scorer / `github.com/sahilm/fuzzy` |
| File watch | `notify` | `github.com/fsnotify/fsnotify` |
| Starlark | `starlark-rust` | `go.starlark.net/starlark` |
| TUI | `ratatui` | `github.com/charmbracelet/bubbletea` + `lipgloss` (with a custom immediate-mode render layer) |
| Syntax highlight | `syntect`/`two-face` | `github.com/alecthomas/chroma/v2` |
| Markdown | `pulldown-cmark` | `github.com/yuin/goldmark` (streaming wrapper) |
| JS runtime | `v8` (rusty_v8) | `github.com/dop251/goja` or `rogchap.com/v8go` |
| WebRTC | native AVAudioEngine | `github.com/pion/webrtc/v4` |
| Age encryption | `age` | `filippo.io/age` |
| Keyring | `keyring` | `github.com/zalando/go-keyring` |
| OpenTelemetry | `opentelemetry` | `go.opentelemetry.io/otel` |
| AWS SigV4 | `aws-sigv4` | `github.com/aws/aws-sdk-go-v2` |
| MCP | `rmcp` | `github.com/modelcontextprotocol/go-sdk` (or in-house) |

## 7. Scope & realism note

This is a multi-engineer, multi-quarter effort. The TUI (Phase 9) and the core
engine (Phase 7) are each on the order of several engineer-months. The roadmap is
structured so value is delivered incrementally — a working headless agent at the
end of Phase 8 — well before the full TUI and peripheral surface are complete.

## 8. Collaboration & autonomy model

Agreed working mode: implement autonomously by referencing the Codex 0.136.0 source;
pause only for genuine decision points.

**Escalated to the maintainer (work pauses) — only these five triggers:**
1. A trade-off that conflicts with a stated project goal — e.g. a dependency forcing
   cgo (vs the cgo-free aim) or a choice that breaks drop-in format/protocol parity.
2. A **material** drop-in deviation that cannot be avoided (user-visible behavior or
   ecosystem interop) — accept vs invest more.
3. Scope/priority changes — deferring or dropping a spec/feature (e.g. cross-platform
   voice).
4. Anything requiring the maintainer's accounts/credentials/infra — recording real
   model golden fixtures, enterprise-only features, CI secrets, signing keys.
5. Outward-facing / irreversible / cost-incurring actions — publishing releases,
   npm/brew, large-scale paid API usage, secret rotation, force-push/deletes.

**Decided autonomously (reference source + project principles + the §6 dep table):**
- All deterministic translation; internal structure, naming, and test design.
- Library choices that satisfy the stated goals (the §6 defaults).
- Non-material / unavoidable deviations — logged in `DEVIATIONS.md` and reported in
  the phase-end summary, not blocking.
- Build/test fixes, refactors, and local commits/pushes to this repo.

**Cadence:** fully autonomous within a spec; a short, non-blocking
"done / tested / deviations / next" summary at each phase boundary. The maintainer is
pinged only when one of the five triggers above is hit.
