# Deviations from Codex 0.136.0

The single, reviewed registry of every place codexgo intentionally diverges from the
behavior or output of the reference Codex build. The parity goal is *zero unexplained*
diffs — every entry below has a rationale. **Material** deviations (user-visible
behavior or ecosystem interop) are marked `review` and need maintainer sign-off
(see `docs/ROADMAP.md` §8); non-material ones are `accepted`.

Categories: `format` (on-disk), `protocol` (wire), `behavioral`, `cosmetic`.

| Spec | Category | Deviation | Rationale | Status |
|------|----------|-----------|-----------|--------|
| 01 abspath | behavioral | `dunce` crate → stdlib `filepath` + hand-rolled verbatim/UNC prefix stripping | Go already emits friendly Windows paths; verbatim stripping replicated in `normalizeWindowsDevicePath` | accepted |
| 01 abspath | behavioral | serde thread-local base-path guard → explicit base param (`Unmarshal(data, base)` / `Decoder`) | Go has no thread-locals; explicit base is idiomatic and equivalent | accepted |
| 01 abspath/pathutil | behavioral | `Path::canonicalize` → `filepath.EvalSymlinks`+`Abs` | closest stdlib analogue; macOS `/var`→`/private/var` may differ on some paths | accepted |
| 01 fuzzymatch / 20 filesearch | behavioral | `nucleo` ranking → custom `fuzzymatch` scorer | exact nucleo scores are impractical to reproduce; result **set** preserved, **order** may differ slightly | review |
| 20 filesearch | behavioral | `ignore` crate → hand-rolled `.gitignore` matcher | supports comments/negation/anchoring/`**`/`[...]`; not bit-exact to git for exotic escapes | review |
| 20 filesearch | behavioral | async streaming search session omitted; only synchronous `Run` ported | `Run` is the value-producing entry point; streaming session can be added later | accepted |
| 20 filewatcher | behavioral | `notify` recursive watch → fsnotify (non-recursive OS watch; recursive flag is semantic-only) | fsnotify has no recursive `Add`; synthetic events + direct children match codex; deep recursive OS delivery is platform-dependent | review |
| 20 filewatcher | behavioral | RAII `Drop` → explicit idempotent `Close()`; tokio `Notify` → `sync.Cond` | Go has no destructors/async runtime; semantics preserved | accepted |
| cross-cutting | behavioral | tokio async (channels, `Arc<Mutex>`, `CancellationToken`) → goroutines/channels/`sync`/`context` | language-idiomatic concurrency; observable ordering preserved | accepted |
| cross-cutting | format | JSON object key ordering (`encoding/json` sorts map keys) | parity asserted via canonicalizer (`internal/paritytest`); struct field order is controlled explicitly where it matters | accepted |
| 02 protocol | format | internally/externally-tagged serde enums → single struct + `Kind` discriminator + custom `MarshalJSON`/`UnmarshalJSON` | Go lacks sum types; wire JSON is preserved exactly | accepted |
| 02 protocol | cosmetic | `op.go` is 1193 lines (> 800 guideline) | `Op` marshal/unmarshal form one cohesive 25-variant unit; splitting hurts cohesion | accepted |
| 03 execpolicy | behavioral | `network_rule`, `amend.rs`, and the clap CLI omitted (scoped to the prefix-rule matcher) | belong to network-policy (spec 15) and CLI (spec 41); the matcher itself is faithful | accepted |
| 03 execpolicy | behavioral | Starlark f-strings not enabled (`go.starlark.net` has none) | codex's bundled policies use no f-strings | accepted |
| 12 sandbox | behavioral | macOS seatbelt complete (.sbpl generation + `Backend` + `sandbox-exec` spawn + policy model/matrix/resolution, with golden + darwin behavioral tests). Linux/Windows backends return not-implemented | Linux/Windows are separate XL specs 13/14; macOS is the dev/parity target for the `codex exec` milestone | accepted |
| 25 code-mode | behavioral | Complete: CodeModeService/Session engine + exec/wait on goja + cell isolation + ToolInvoker bridge. goja is ES5.1+/partial-ES2015+ vs V8 | goja is cgo-free (project goal); JS-feature gaps documented; feature-flagged off by default | accepted |
| 41/42 CLI | behavioral | Default (no subcommand) prints "TUI not yet implemented" + exit 1 (until TUI lands); archive/unarchive, login --device-auth, mcp add/remove, debug prompt-input, non-stdio app-server transports give a clear notice + non-zero exit; exec/mcp-server run against the mock model until a real provider client is wired | turn-driving + diagnostic surface is complete; the rest are clear notices (never silent) and land with TUI / real-model wiring | review |
| 19 threadstore | behavioral | `LocalThreadStore` read/list/search/archive/resume/update now fully implemented (state-DB-first with sessions-tree scan fallback). Remaining minor: SearchThreads uses substring match (not ripgrep transcript scan); UpdateThreadMetadata writes the SQLite row but not rollout session-meta lines | the read/list/search/archive surface is complete and tested; the two minor items are dependency-gated | accepted |
| 08 login | behavioral | AWS SigV4 is an interface stub; agent-task X25519 registration network side-effects out of scope | not on the OpenAI/ChatGPT critical path; add with Bedrock support | accepted |
| 04 config | behavioral | layer-state/fingerprint/origins, macOS MDM managed prefs, cloud-requirements, git-trust project loading, keymap parsing carried as opaque TOML trees / omitted | large peripheral surface; load/merge/validate + typed schema are faithful | accepted |
| 32 app-server-protocol | behavioral | ts-rs/schemars schema-export generator deferred; a few cross-area fields carried as `json.RawMessage` | the runtime types/methods are faithful; the codegen tool is non-runtime | accepted |

| 34 exec | behavioral | On an API error, the error *message* text differs: codex surfaces the clean upstream error body; codexgo leaks internal wrapping (`core: model stream failed: …`). The event shape, exit code, and terminal `turn.failed` event all match (verified by `TestParityTurnError`). | error-message cleanup needs threading the upstream API error through core without the `%w` chain prefix; tracked | review |

> Entries are appended as each spec lands and reports its deviations.
> **`review (must finish)`** items are genuine gaps to close before claiming the
> corresponding spec complete — they are not permanent accepted deviations.
