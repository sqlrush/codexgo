# Parity Validation Report

Living record of differential validation of `codexgo` against the reference
**codex 0.136.0** binary (`rust-v0.136.0`). This is the evidence behind the
"drop-in compatible" claim — see `docs/ROADMAP.md` §2.1 and `DEVIATIONS.md`.

## Method

- The real `codex` 0.136.0 binary (`codex-aarch64-apple-darwin` from the GitHub
  release) is run locally and its output captured as golden fixtures under
  `testdata/golden/` (gitignored — the outputs embed OpenAI content such as the
  system prompt, so they are **not** redistributed; CI/contributors regenerate
  them from a codex binary).
- For each surface, `codexgo`'s output is compared to codex's. **No-auth** surfaces
  (no model call) are validated now; **turn-level** surfaces need a one-time
  authenticated recording (see "Pending").

## Results (no-auth surfaces)

| Surface | Command | Result |
|---|---|---|
| Model catalog | `codex debug models --bundled` | ✅ **Pass** — identical model-slug set (`gpt-5.5`, `gpt-5.4`, `gpt-5.4-mini`, `gpt-5.3-codex`, `gpt-5.2`, `codex-auto-review`); 197,239 vs 196,999 bytes (minor field-ordering/formatting delta, semantically equal). |
| Version | `codex --version` | ✅ both report `0.136.0` (codex prints `codex-cli 0.136.0`). |
| Top-level subcommand set | `codex --help` | ✅ **Pass** — exact match (24 subcommands + aliases `e`/`a`/`cloud-tasks`). The earlier gap (missing `app`/`cloud`/`exec-server`/`help`/`plugin`/`remote-control`/`review`/`update`) is closed; `review`/`cloud list`/`plugin list`/`exec-server` stdio/`help` are wired, `app`/`update`/`remote-control` are clear notices. |
| Shell completion | `codex completion bash` | ⚠️ **Deviation**: codex emits a 5,924-line clap-generated script; codexgo's hand-rolled CLI emits a functional but not byte-identical script. Enumerates all subcommands/flags but not clap's exact format. |
| `apply_patch` envelope | `apply_patch "<patch>"` (arg0) | ✅ **Pass** — byte-level differential: real codex and codexgo applied the same multi-op patch (update + add) to identical workdirs with **byte-identical resulting files** and the **same success message** (`Success. Updated the following files: / A baz.txt / M foo.txt`). Plus codex's own `#[cfg(test)]` corpus ported in internal/applypatch. |
| `doctor --json` | `codex doctor --json` | ⚠️ **Partial** — top-level schema matches exactly (`schemaVersion`/`generatedAt`/`overallStatus`/`codexVersion`/`checks`), but codexgo emits 8 coarse checks vs codex's 18 granular check IDs (e.g. `network.websocket_reachability`, `state.rollout_db_parity`, `runtime.provenance`). Functional but not check-for-check identical. |
| execpolicy decisions | (internal) | ⏳ codex exposes no `execpolicy` subcommand; validated via ported upstream tests in internal/execpolicy. |

## Results (turn-level surface — no credentials needed)

| Surface | Command | Result |
|---|---|---|
| `exec --json` turn lifecycle | `codex exec --json "hello"` vs codexgo | ✅ **Pass** — see `TestParityTurnExec`. **Both binaries** are pointed at the **same fake `/v1/responses` SSE endpoint** via the **same drop-in `config.toml`** (`[model_providers.parity]`, `env_key`), and produce a **byte-identical normalized JSONL stream**: same event-type sequence, same final agent message, same usage. The codexgo binary now honors the custom `model_provider` selection, its `base_url`, and its `env_key` directly — no in-process harness. No real OpenAI credentials required. |

### `TestParityTurnExec` — the turn-level differential

This is the highest-value parity test: it proves **behavioral** (not just format)
drop-in by driving one real model turn through both binaries against a fake
Responses-API server, with **no OpenAI credentials**.

**How it works**

- A `net/http/httptest` server answers `POST <…>/responses` with a deterministic
  Server-Sent Events stream (`Content-Type: text/event-stream`). The event
  vocabulary and SSE framing mirror codex's own test harness
  (`codex-rs/core/tests/common/responses.rs`): `response.created` →
  `response.output_item.added` → `response.output_text.delta` ×3
  (`"Hello "`, `"from "`, `"parity"`) → `response.output_item.done` →
  `response.completed` (with `usage`).
- A drop-in `config.toml` defines a custom `[model_providers.parity]` provider
  with `base_url = "<server>/v1"`, `wire_api = "responses"`,
  `requires_openai_auth = false`, `env_key = "PARITY_FAKE_KEY"`, plus top-level
  `model_provider = "parity"` and `model = "gpt-5.5"`. The client appends
  `/responses` to `base_url`, yielding `<server>/v1/responses` (matches
  `Provider::url_for_path` in codex-rs). `PARITY_FAKE_KEY=dummy` is exported so
  the client sends `Authorization: Bearer dummy`.

**Normalized comparison (4/4 events identical)**

```
codex[0]   = codexgo[0] = {"type":"thread.started"}
codex[1]   = codexgo[1] = {"type":"turn.started"}
codex[2]   = codexgo[2] = {"item":{"text":"Hello from parity","type":"agent_message"},"type":"item.completed"}
codex[3]   = codexgo[3] = {"type":"turn.completed","usage":{"cached_input_tokens":0,"input_tokens":11,"output_tokens":3,"reasoning_output_tokens":0}}
```

Raw streams before normalization (real codex / codexgo):

```
codex:   {"type":"thread.started","thread_id":"019e905f-…"}
codexgo: {"thread_id":"thread-00000000000000000001","type":"thread.started"}
codex:   {"type":"turn.started"}
codexgo: {"type":"turn.started"}
codex:   {"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"Hello from parity"}}
codexgo: {"item":{"id":"item_0","text":"Hello from parity","type":"agent_message"},"type":"item.completed"}
codex:   {"type":"turn.completed","usage":{"input_tokens":11,"cached_input_tokens":0,"output_tokens":3,"reasoning_output_tokens":0}}
codexgo: {"type":"turn.completed","usage":{"input_tokens":11,"cached_input_tokens":0,"output_tokens":3,"reasoning_output_tokens":0}}
```

**Documented divergences (noise, not behavioral gaps)**

1. **JSON object key order.** codex (serde) preserves declaration order; codexgo
   marshals Go maps in sorted-key order. Both are valid JSON with identical
   semantics. The test normalizes by re-parsing each line and re-marshalling, so
   key order does not affect the comparison.
2. **`thread_id` value.** codex mints a UUIDv7
   (`019e905f-…`); codexgo's in-memory thread store uses a monotonic id
   (`thread-00000000000000000001`). Both are opaque per-run identifiers. The test
   strips `thread_id` (and any item `id`) before comparing.

**Drop-in gap CLOSED: the codexgo binary now honors a custom provider's
`base_url`/`env_key`.**

Both binaries are now driven *exactly* as a user would: the actual binary,
`exec --json`, configured purely through `config.toml`. Each picks up
`[model_providers.parity]`, sends `Authorization: Bearer dummy`, POSTs to the
fake `/v1/responses`, and emits a normal turn.

codexgo's `cmd/codex exec` assembly (`internal/cli/assembly.go` →
`buildAssemblyWithDefaults`, with provider selection in
`internal/cli/provider_select.go`) now:

- reads the resolved `model_provider` selection and the `[model_providers]` map
  from the loaded config (projected through `internal/cli/config_load.go`), merges
  the configured providers onto the built-in catalog
  (`modelproviderinfo.MergeConfiguredModelProviders` over
  `BuiltInModelProviders`, honoring `openai_base_url`), and builds the
  `api.Provider` for the **selected** provider — so a custom
  `[model_providers.<id>]` `base_url` (and `wire_api`, `http_headers`, retry, …)
  is honored;
- resolves credentials honoring the provider's `env_key` first (a static
  `Authorization: Bearer <env_key value>`), and only falls back to the
  `OPENAI_API_KEY` / `CODEX_API_KEY` / `auth.json` login path for
  `requires_openai_auth` providers; and
- honors the configured `model` (over `CODEX_MODEL`, over the mock slug) and
  threads the resolved provider id + model into the exec/review/TUI session
  defaults.

The scripted mock remains the fallback **only** when no usable credential /
provider resolves (preserving the offline/dev behavior and
`CODEX_EXEC_MOCK_REPLY`). As a result, the **codexgo binary** run against this
config.toml now contacts the server and produces a real turn whose normalized
JSONL stream is byte-identical to the real codex binary's — proving the binary
itself is a behavioral drop-in for a custom provider. The OpenAI-provider path
(`OPENAI_API_KEY` + default base_url) is wired through the same code.

## Pending (need a one-time authenticated recording — maintainer)

The turn-level `exec --json` differential above no longer needs credentials.
These remaining surfaces still warrant a one-time authenticated capture or
further offline differentials:

- `Op`/`EventMsg` wire stream over the app-server.
- Auto-compaction trigger points and `ContextCompacted` payloads.
- Tool-call (exec_command / apply_patch) end-to-end under sandbox.

## Harness

- `internal/paritytest` provides golden helpers (`AssertBytes`, `AssertJSONEqual`,
  `CanonicalizeJSON`).
- **Automated differential tests** live in
  `internal/paritytest/differential_test.go` (no-auth surfaces) and
  `internal/paritytest/turn_test.go` (turn-level), env-gated on `CODEX_PARITY_BIN`
  (path to a real codex binary). They build the codexgo `codex` binary and compare
  it to codex for: the subcommand set, the bundled model-slug set, an
  `apply_patch` byte-identity round-trip, and a full `exec --json` model turn
  against a fake `/v1/responses` SSE endpoint (`TestParityTurnExec`). They
  **skip** when `CODEX_PARITY_BIN` is unset, so the default `go test ./...` / CI
  stays hermetic. Run locally:
  ```
  CODEX_PARITY_BIN=/path/to/codex go test ./internal/paritytest/ -run Parity -v
  CODEX_PARITY_BIN=/path/to/codex go test ./internal/paritytest/ -run TestParityTurnExec -v
  ```
  Current status with codex 0.136.0: **3/3 no-auth + 1/1 turn-level pass**.
- Per-spec golden tests run in CI against committed fixtures where the fixture
  contains no OpenAI content; codex-output fixtures are env-gated and regenerated
  locally.

## Honest status

Format/CLI-surface parity is validated and faithful (model catalog identical;
subcommand set complete; `apply_patch` byte-identical). The **turn-level
behavioral drop-in proof is now done, binary-vs-binary**: `TestParityTurnExec`
runs one real `exec --json` model turn through **both built binaries** against a
fake `/v1/responses` endpoint, configured purely through the same drop-in
`config.toml` + `PARITY_FAKE_KEY`, with **no OpenAI credentials**, and the
normalized JSONL streams are byte-identical (same event sequence, message text,
and usage). The codexgo binary now honors a custom provider's `model_provider`
selection, `base_url`, and `env_key` (previous in-process workaround removed). The
remaining honest caveat is that tool-call / compaction / app-server wire-stream
differentials are still pending.
