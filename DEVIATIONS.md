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

> Entries are appended as each spec lands and reports its deviations.
