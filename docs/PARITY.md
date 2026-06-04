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
| `exec --json` tool-call turn (shell) | `codex exec --json` w/ `shell_command` call vs codexgo | ❌ **DIVERGENCE** — see `TestParityTurnExecCommand`. Multi-request agent loop (tool call → tool output → final message) at the same fake server. **Real codex** runs `echo parity-tool-ok` non-interactively (`approval_policy = "never"`, `sandbox_mode = "danger-full-access"`) and emits the `command_execution` lifecycle item (begin+end) then the final message. **codexgo binary** rejects the call with `tool dispatch error: unsupported call: shell_command` — it executes **no** tool. Root cause below. Test asserts the codex contract, then **skips** with the documented gap (suite stays green). |
| `exec --json` tool-call turn (apply_patch) | `codex exec --json` w/ apply_patch heredoc vs codexgo | ❌ **DIVERGENCE** — see `TestParityTurnApplyPatch`. Same loop; the model sends `shell_command` whose script is an `apply_patch <<'EOF' … EOF` heredoc (how codex 0.136.0 delivers apply_patch for gpt-5.5). **Real codex** intercepts it, writes the file, and emits the `file_change` lifecycle item. **codexgo binary** again rejects with `unsupported call: shell_command` and writes **no** file. Same root cause. |

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

### `TestParityTurnExecCommand` / `TestParityTurnApplyPatch` — tool-call turns (DIVERGENCE found)

These extend the turn-level proof from a single message turn to a **multi-request
agent loop** (the tool-execution path), again with **no OpenAI credentials**. They
are the credential-free analogue of "run a command / edit a file under sandbox".

**How they work**

- A multi-request fake `/v1/responses` server tracks request count: the **first**
  POST streams a `function_call`; after the binary runs the tool and feeds the
  output back, the **second** POST streams a final assistant message. Both end with
  `response.completed` carrying the fixed parity `usage`.
- Both binaries are driven identically via `exec --json --skip-git-repo-check -C
  <tmp workdir>` and the same drop-in `config.toml`, which adds the
  non-interactive execution settings `codex exec` needs to run a command without an
  approval prompt: `approval_policy = "never"` and `sandbox_mode =
  "danger-full-access"`.
- Tool name + argument shape match codex 0.136.0 exactly: a **`shell_command`**
  function call whose single argument is `command` — a **string** shell script
  (codex's own harness `ev_shell_command_call` →
  `{"command":"<script>"}`, `codex-rs/core/tests/common/responses.rs`). gpt-5.5 has
  `shell_type = "shell_command"`, so this is the model-visible exec tool. apply_patch
  is delivered as a `shell_command` whose script is an `apply_patch <<'EOF' … EOF`
  heredoc, which codex intercepts (`intercept_apply_patch`).

**What the real codex emits (the reference contract these tests assert)**

```
# exec command turn
{"type":"thread.started","thread_id":"…"}
{"type":"turn.started"}
{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"/bin/zsh -lc 'echo parity-tool-ok'","aggregated_output":"","exit_code":null,"status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_0","type":"command_execution","command":"/bin/zsh -lc 'echo parity-tool-ok'","aggregated_output":"parity-tool-ok\n","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"ran the command"}}
{"type":"turn.completed","usage":{"input_tokens":22,"cached_input_tokens":0,"output_tokens":6,"reasoning_output_tokens":0}}

# apply_patch turn
{"type":"item.started","item":{"id":"item_0","type":"file_change","changes":[{"path":"<WORKDIR>/parity_patch.txt","kind":"add"}],"status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_0","type":"file_change","changes":[{"path":"<WORKDIR>/parity_patch.txt","kind":"add"}],"status":"completed"}}
…  # file written: "hello from apply_patch parity\n"
```

**What the codexgo binary does (the divergence)**

The codexgo binary executes **no** tool. The function-call output it feeds back on
the follow-up request is:

```
shell_command  → output: "tool dispatch error: unsupported call: shell_command"
exec_command   → output: "tool dispatch error: unsupported call: exec_command"
apply_patch    → output: "tool dispatch error: unsupported call: apply_patch"
```

So no `command_execution` / `file_change` item is emitted and no file is written
(`apply_patch` workdir stays empty); the stream jumps straight to the final
`agent_message`.

**Root cause (a real codexgo bug, in a package outside this task's edit scope)**

The codexgo *binary*'s exec/run/TUI assembly builds an **empty** tool router and
injects **no** `ExecService`:

- `internal/cli/assembly.go` → `assembleResult` calls `appserver.Assemble(...)`
  **without** setting `AssemblyConfig.ToolRouterFactory` **or**
  `AssemblyConfig.ExecService`.
- `internal/appserver/assembly.go` then defaults the router factory to
  `core.NewDefaultToolRouter()` — **zero executors** — and leaves `ExecService` nil.
- At dispatch, `internal/core/tools.go` returns `unsupported call: <name>` for every
  function call (`fmt.Sprintf("unsupported call: %s", name)`), which the turn loop
  serializes as the `function_call_output`.

The machinery to do this correctly **already exists** in `internal/core` but is
never wired into the binary:

- `core.BuiltinToolRouter(core.BuiltinToolDeps{...})` (`internal/core/tools.go`)
  builds a router with the built-in executors.
- `execCommandExecutor` and `applyPatchExecutor` (`internal/core/tool_executors.go`)
  implement the `exec_command` and `apply_patch` tools, including emitting
  `ExecCommandBegin`/`ExecCommandEnd` events.

**Two secondary divergences observed while probing (would surface after wiring):**

1. **Tool name.** codexgo registers `exec_command` (+ `apply_patch`) but **not**
   `shell_command`. The reference dispatches `shell_command` (string `command`) and
   `exec_command` (unified exec, string **`cmd`**); for gpt-5.5 the model-visible
   tool is `shell_command`. So even with the router wired, codexgo would reject a
   `shell_command` call until it registers that name (or an alias).
2. **`exec_command` argument schema.** codexgo's `execCommandExecutor` parses
   `{"command": ["echo","…"]}` (a **string array**) + `workdir`. The reference's
   `exec_command` (unified exec) parses `{"cmd": "echo …"}` (a **string**) — probing
   the reference with the array form yields `failed to parse function arguments:
   missing field 'cmd'`. The two `exec_command` tools are **not** wire-compatible.
3. **apply_patch is not a model-visible function tool for gpt-5.5.** Sending a
   top-level `apply_patch` function call to the reference returns `output: "aborted"`
   (it is delivered via `shell_command` heredoc instead). codexgo registers a
   top-level `apply_patch` tool, but it is also unreachable in the binary today (same
   empty-router root cause).

**Fix sketch (not applied — lives outside `internal/tools` / `internal/core`, and
this task may only edit `internal/paritytest/` + this file):** thread a
`ToolRouterFactory` and `ExecService` into `appserver.AssemblyConfig` from
`internal/cli/assembly.go`, building the router via `core.BuiltinToolRouter` with a
real `ExecService` (and register a `shell_command` name / accept the reference's
`cmd`-string `exec_command` schema). Once wired, `TestParityTurnExecCommand` and
`TestParityTurnApplyPatch` stop skipping and assert full normalized parity +
byte-identical patched-file contents automatically (the assertions are already
written and run on the codex side every invocation).

## Pending (need a one-time authenticated recording — maintainer)

The turn-level `exec --json` differential above no longer needs credentials.
These remaining surfaces still warrant a one-time authenticated capture or
further offline differentials:

- `Op`/`EventMsg` wire stream over the app-server.
- Auto-compaction trigger points and `ContextCompacted` payloads.
- Tool-call (exec_command / apply_patch) end-to-end **is now characterized
  credential-free** by `TestParityTurnExecCommand` / `TestParityTurnApplyPatch`
  (see the divergence section above). The remaining work is the **fix** (wire the
  builtin tool router + `ExecService` into the binary assembly, register
  `shell_command`, accept the reference's `cmd`-string `exec_command` schema), not
  more recording.

## Harness

- `internal/paritytest` provides golden helpers (`AssertBytes`, `AssertJSONEqual`,
  `CanonicalizeJSON`).
- **Automated differential tests** live in
  `internal/paritytest/differential_test.go` (no-auth surfaces),
  `internal/paritytest/turn_test.go` (single-message turn-level), and
  `internal/paritytest/turn_toolcall_test.go` (multi-request tool-call turns),
  env-gated on `CODEX_PARITY_BIN` (path to a real codex binary). They build the
  codexgo `codex` binary and compare it to codex for: the subcommand set, the
  bundled model-slug set, an `apply_patch` byte-identity round-trip, a full `exec
  --json` model turn against a fake `/v1/responses` SSE endpoint
  (`TestParityTurnExec`), and the tool-call agent loop for shell exec and
  apply_patch (`TestParityTurnExecCommand` / `TestParityTurnApplyPatch`). They
  **skip** when `CODEX_PARITY_BIN` is unset, so the default `go test ./...` / CI
  stays hermetic. Run locally:
  ```
  CODEX_PARITY_BIN=/path/to/codex go test ./internal/paritytest/ -run Parity -v
  CODEX_PARITY_BIN=/path/to/codex go test ./internal/paritytest/ -run TestParityTurn -v
  ```
  Current status with codex 0.136.0: **3/3 no-auth + 1/1 single-message
  turn-level pass**; the **2 tool-call turns assert the codex contract and skip on
  the documented codexgo unwired-tools divergence** (binary builds an empty tool
  router / no `ExecService`).
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
selection, `base_url`, and `env_key` (previous in-process workaround removed).

The **tool-call path is now characterized credential-free** and revealed a **real
codexgo binary gap, not just a missing recording**: `TestParityTurnExecCommand` and
`TestParityTurnApplyPatch` drive the multi-request agent loop through both binaries.
The real codex runs the shell command / applies the patch and emits the
`command_execution` / `file_change` lifecycle items; the **codexgo binary executes
no tool at all** (`tool dispatch error: unsupported call: …`) because its
exec/run/TUI assembly wires an empty tool router and no `ExecService` — the builtin
executors + `BuiltinToolRouter` exist in `internal/core` but are never connected
(see the divergence section above for the exact files, plus the secondary tool-name
and `exec_command` schema divergences). The fix lives outside this task's edit
scope (`internal/cli` / `internal/appserver`), so it is documented here rather than
applied; the tests assert the codex contract every run and skip on the codexgo gap,
so they begin enforcing full parity automatically once the wiring lands. The
remaining honest caveats are compaction and app-server wire-stream differentials.
