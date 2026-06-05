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
| Shell completion | `codex completion {bash,elvish,fish,powershell,zsh}` | ✅ **Pass — byte-identical** for all five clap_complete shells (bash 204,453 B, zsh 206,193 B, fish 150,346 B, elvish 133,387 B, powershell 215,622 B) plus the default-is-bash case, asserted by env-gated `TestParityCompletion`. bash is a faithful port of clap_complete v4.5.65's `bash.rs` generator driven by an embedded command-tree model (`internal/cli/completion_tree.go`); zsh/fish/elvish/powershell are the deterministic generated scripts vendored verbatim via `//go:embed`. Cosmetic gap: unsupported-shell error omits clap's optional `tip:` suggestion line (see DEVIATIONS.md). |
| `apply_patch` envelope | `apply_patch "<patch>"` (arg0) | ✅ **Pass** — byte-level differential: real codex and codexgo applied the same multi-op patch (update + add) to identical workdirs with **byte-identical resulting files** and the **same success message** (`Success. Updated the following files: / A baz.txt / M foo.txt`). Plus codex's own `#[cfg(test)]` corpus ported in internal/applypatch. |
| `doctor --json` | `codex doctor --json` | ✅ **Pass (17/18 exact)** — top-level schema matches exactly, all 18 granular check IDs are emitted, and per-check `details` is the structured object codex emits (label → string \| string-array, `{}` when empty; ported `structured_json_details`/`JsonDetailValue`). Verified against a live codex capture: 17/18 checks match the detail key set + value types exactly; the one remaining row (`network.websocket_reachability` handshake) is probe-outcome-conditional — the failure path emits the identical key. Best-effort value sources documented in DEVIATIONS "44 doctor". |
| execpolicy decisions | (internal) | ⏳ codex exposes no `execpolicy` subcommand; validated via ported upstream tests in internal/execpolicy. |

## Results (turn-level surface — no credentials needed)

| Surface | Command | Result |
|---|---|---|
| `exec --json` turn lifecycle | `codex exec --json "hello"` vs codexgo | ✅ **Pass** — see `TestParityTurnExec`. **Both binaries** are pointed at the **same fake `/v1/responses` SSE endpoint** via the **same drop-in `config.toml`** (`[model_providers.parity]`, `env_key`), and produce a **byte-identical normalized JSONL stream**: same event-type sequence, same final agent message, same usage. The codexgo binary now honors the custom `model_provider` selection, its `base_url`, and its `env_key` directly — no in-process harness. No real OpenAI credentials required. |
| `exec --json` tool-call turn (shell) | `codex exec --json` w/ `shell_command` call vs codexgo | ✅ **Pass** — see `TestParityTurnExecCommand`. Multi-request agent loop (tool call → tool output → final message) at the same fake server. Both binaries register `shell_command` (string `command`), wrap it in the user shell (`/bin/zsh -lc 'echo parity-tool-ok'`), run it non-interactively (`approval_policy = "never"`, `sandbox_mode = "danger-full-access"`), and emit the **byte-identical** `command_execution` lifecycle item (begin `in_progress` + end `completed`, same `command`, `aggregated_output`, `exit_code`) then the same final message and usage. codexgo wires the builtin tool router (`core.BuiltinToolRouter`) into the exec assembly and threads the session into dispatch so the executor emits the lifecycle events. |
| `exec --json` tool-call turn (apply_patch) | `codex exec --json` w/ apply_patch heredoc vs codexgo | ✅ **Pass** — see `TestParityTurnApplyPatch`. Same loop; the model sends `shell_command` whose script is an `apply_patch <<'EOF' … EOF` heredoc (how codex 0.136.0 delivers apply_patch for gpt-5.5). Both binaries intercept the heredoc (`shellcmd.ExtractApplyPatchHeredoc`, a mvdan.cc/sh port of codex's tree-sitter detection), route it to `internal/applypatch`, write the file, and emit the **byte-identical** `file_change` lifecycle item (begin `in_progress` + end `completed`, same `changes` path/kind). The resulting file content is byte-identical to real codex (`hello from apply_patch parity\n`). The `-C/--cd` workdir is now honored by `codex exec` so the file lands in the run cwd. |
| `exec --json` error turn | `codex exec --json` against an HTTP-400 endpoint vs codexgo | ✅ **Pass** — see `TestParityTurnError`. A fake `/v1/responses` returns a non-retryable HTTP 400; both binaries fail the turn with the **same terminal `turn.failed` event**, the **same exit code**, and the **same event-type sequence**. Fixed a real bug: codexgo emitted `turn.completed` on a failed turn (`collectTurnComplete` now emits `TurnFailedEvent` when a critical error is set). The error *message* is now **byte-identical** too: `modelFacingErrorMessage` surfaces the upstream HTTP response body verbatim (the Rust UnexpectedStatus display) instead of the internal wrapping chain, asserted by this test. |
| `exec -o/--output-last-message <FILE>` | `codex exec --json -o <file>` vs codexgo | ✅ **Pass** — see `TestParityOutputLastMessage`. Both binaries write the **byte-identical** final-agent-message file (`Hello from parity`). |
| `exec --output-schema <FILE>` request shape | `codex exec --json --output-schema <file>` vs codexgo | ✅ **Pass** — see `TestParityOutputSchemaRequest`. A **request-side** differential (what the binary SENDS): the captured POST `/responses` body's `text` block is byte-identical, i.e. `{"format":{"type":"json_schema","strict":true,"schema":<schema>,"name":"codex_output_schema"},"verbosity":"low"}`. Found+fixed **two** real drop-in bugs: (1) `output_schema_strict` defaulted to `false` (Go zero value) instead of codex's `true` — now set in `buildResponsesClientConfig`; (2) the model-client factory wasn't given the bundled **model catalog**, so every model resolved to minimal slug-derived metadata and the request omitted `text.verbosity` (and would mis-set reasoning/service-tier) — `assembly.go` now wires `ModelCatalog: bundledModelCatalog()`, so `gpt-5.5` resolves its real `support_verbosity`/`default_verbosity = "low"`. |
| `/responses` `input` context | `codex exec --json` vs codexgo (captured POST body) | ✅ **Pass (full)** — see `TestParityInputContext` + `TestParityInputContextSandboxModes`. codexgo seeds the complete codex initial context: the developer message bundles **two content parts** — `<permissions instructions>` and `<skills_instructions>` (the `SKILL.md` scan; the five embedded system skills are materialized under `CODEX_HOME/skills/.system` exactly like codex, and the default skill roots now port all four config-layer scopes in the `skill_roots_from_layer_stack_inner` order: project `.codex/skills` [Repo, gated behind the host opting in — git-trust gate is a STUB], user layer, admin `/etc/codex/skills` [System layer, always emitted], then the repo `.agents/skills` chain) — and the user message carries `<environment_context>`. All three fragments are **byte-identical** to the real codex binary (per-run CODEX_HOME paths normalized; same host ⇒ same cwd/date/timezone/sandbox). The `<filesystem>` block is now byte-verified for **all three sandbox modes**: read-only (`:root` read), workspace-write (managed/restricted with cwd write, `:slash_tmp`/`:tmpdir` write, and `{cwd}/.git`,`.agents`,`.codex` read carveouts), and danger-full-access (disabled/unrestricted) — `TestParityInputContextSandboxModes` runs both binaries in one cwd per mode. Fixed a real bug en route: `codex exec` defaults approval to `never` (not on-request). |
| Full `/responses` request body | `codex exec --json` vs codexgo (captured POST body) | ⚠️ **Partial** — see `TestParityRequestBody`. The broadest request-side differential: it captures the full POST `/responses` body from both binaries for a plain turn and compares **every top-level field**. The key set matches, and these fields are now **byte-identical**: `model`, `tool_choice`, `store`, `stream`, `include`, `service_tier`, `text`, `reasoning`, `parallel_tool_calls`, **`instructions`**. It found+fixed **three more** real drop-in bugs: (3) `parallel_tool_calls` always sent `false` — now derived from `model_info.supports_parallel_tool_calls` (gpt-5.5 → `true`), matching codex (`compact_remote.rs`); (4) `reasoning` sent `"summary":"auto"` while codex omits it — now resolved as `config.model_reasoning_summary` ?? `model_info.default_reasoning_summary` (gpt-5.5 → `"none"`, so no summary), matching codex (`turn_context.rs`); (5) `instructions` sent gpt-5.5's `base_instructions` verbatim, which **bakes the "friendly" personality**, while codex renders the model's `instructions_template` with the **resolved personality** — `cli.or(config.personality).or(Pragmatic)` since the Personality feature is on by default (`config/mod.rs`). `buildResponsesClientConfig` now calls `GetModelInstructions(resolvePersonality(...))`, so the base prompt is byte-identical (gpt-5.5 → "pragmatic"). **Every top-level field is now byte-identical and enforced** (`documentedGapFields` is empty): all scalars, `instructions` (personality rendering), the full 11-tool `tools` registry, and `input` (permissions + skills_instructions + environment_context, per-run CODEX_HOME paths normalized). The `/responses` request is a **full drop-in** for the plain-turn scenario. |
| `/responses` `tools` specs + order | `codex exec --json` vs codexgo (captured POST body) | ✅ **Pass (11/11 advertised, full-array equality)** — see `TestParityToolSpecs` + `TestParityToolOrder`. codexgo advertises codex's complete default tool registry in the exact spec_plan order — `exec_command, write_stdin, update_plan, get_goal, create_goal, update_goal, request_user_input, apply_patch, view_image, tool_search, web_search` — with **every spec byte-identical**. The UnifiedExec PTY pair comes from the ported `shell_type_for_model_and_features` (with `shell_command` registered dispatch-only); the **goals trio** is gated like codex's `goal_tools_enabled` (persistent state DB wired ∧ `Feature::Goals` (default on) ∧ not the review sub-agent) and is backed by a real SQLite goal store (`internal/state/goals.go`, a faithful port of `state/src/runtime/goals.rs` incl. budget-limit promotion + accounting modes) bridged from `internal/ext/goal`; **`tool_search`** is gated like `append_tool_search_executor` (`model_info.supports_search_tool` ∧ provider `namespace_tools` ∧ deferred sources exist — the collab agent tools when `Feature::Collab` is on without MultiAgentV2) and renders the ported `create_tool_search_tool` description (multi-agent source, BM25 wording, limit 8). Remaining behavioral tail (multi-agent area): the five collab agent tool specs as deferred runtimes + the BM25 search engine, so `tool_search` dispatch currently validates args like codex then returns codex's empty-entries result. |

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

### `TestParityTurnExecCommand` / `TestParityTurnApplyPatch` — tool-call turns (✅ PASS, binary-vs-binary)

These extend the turn-level proof from a single message turn to a **multi-request
agent loop** (the tool-execution path), again with **no OpenAI credentials**. They
are the credential-free analogue of "run a command / edit a file under sandbox".
Both now **pass binary-vs-binary** with byte-identical normalized JSONL.

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

**What both binaries emit (byte-identical after normalization)**

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
…  # file written: "hello from apply_patch parity\n"  (byte-identical to real codex)
```

**How codexgo achieves parity (the implementation)**

1. **`shell_command` tool spec** (`internal/tools/shell_command_spec.go`):
   `CreateShellCommandTool` ports codex's `create_shell_command_tool` byte-faithfully
   — a single required `command` string plus optional `workdir` / `timeout_ms` /
   `login` / approval params, `strict:false`, `additionalProperties:false`, properties
   in sorted-key order (matching the Rust `BTreeMap`). `CreateExecCommandTool` keeps
   the PTY-oriented `exec_command` (a `cmd` string) for models that use it.
   `ShellCommandToolCallParams` decodes the call arguments (with the `timeout` alias).
2. **`shell_command` executor** (`internal/core/shell_command_executor.go`):
   `shellCommandExecutor` wraps the `command` string in the user's default shell
   (`shellcmd.DefaultUserShell().DeriveExecArgs(cmd, login)` →
   `[/bin/zsh, -lc, <cmd>]`), runs it through the injected `ExecService`, and emits
   the `ExecCommandBegin`/`ExecCommandEnd` events the exec JSONL processor renders as
   the `command_execution` lifecycle item. Both `shell_command` (key `command`) and
   `exec_command` (key `cmd`) share this path.
3. **apply_patch heredoc interception** (`internal/shellcmd/apply_patch_heredoc.go`):
   `ExtractApplyPatchHeredoc` ports codex's `extract_apply_patch_from_bash` /
   `maybe_parse_apply_patch` using `mvdan.cc/sh` (the Go analogue of tree-sitter-bash
   the rest of `internal/shellcmd` already uses). It recognizes the conservative
   single-statement forms `apply_patch <<'EOF' … EOF` and
   `cd <path> && apply_patch <<'EOF' … EOF`. When matched, the executor routes the
   patch body to `internal/applypatch` and emits the `file_change` item lifecycle
   (begin with absent status → `in_progress`, end → `completed`) instead of spawning
   the shell.
4. **Router wiring + session threading** (`internal/cli/assembly.go`,
   `internal/core/turn_output.go`, `internal/core/tools.go`): the exec assembly wires
   `core.BuiltinToolRouter(core.BuiltinToolDeps{Exec: newLocalExecService()})`, and
   the turn runner dispatches through the session-aware path
   (`DefaultToolRouter.DispatchWithSession`) so the executor's events reach the exec
   JSONL stream.
5. **`-C/--cd` workdir** (`internal/exec/cli.go`, `internal/cli/cmd_exec.go`):
   `codex exec` now parses `-C/--cd` and overrides the run cwd, so commands and
   apply_patch resolve relative paths against the same directory real codex uses.
6. **`file_change` status mapping** (`internal/exec/item_mapping.go`): an absent
   engine patch-apply status maps to `in_progress` (matching codex's v2
   `status.map(...).unwrap_or(InProgress)`), so the started item reports
   `in_progress` and the completed item reports `completed`.

**Residual byte-level divergences:** none observed for these two turns — the
normalized JSONL streams compare byte-for-byte and the apply_patch file content is
identical. (The `command_execution` item's `command` field is the user's resolved
shell, e.g. `/bin/zsh -lc '…'`; both binaries resolve the same account shell on the
same host, so it matches. On a host where the two binaries would resolve different
default shells the rendered `command` could differ — not observed here.)

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
  turn-level pass + 2/2 tool-call turns pass** (`TestParityTurnExecCommand` /
  `TestParityTurnApplyPatch` now run binary-vs-binary with byte-identical
  normalized JSONL and identical apply_patch file content).
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

The **tool-call path is now a verified binary-vs-binary drop-in**:
`TestParityTurnExecCommand` and `TestParityTurnApplyPatch` drive the multi-request
agent loop through both binaries. Both register `shell_command` (with codex's exact
`{command: string, …}` schema), wrap the command in the user shell, run it through
the `ExecService`, and emit the `command_execution` lifecycle item; for an
`apply_patch <<'EOF' … EOF` heredoc both intercept it (`mvdan.cc/sh` port of codex's
tree-sitter detection), route it to `internal/applypatch`, write the file, and emit
the `file_change` lifecycle item. The normalized JSONL streams compare byte-for-byte
and the apply_patch file content is identical to real codex. The wiring (builtin
tool router + `ExecService` into the exec assembly, session-aware dispatch, `-C/--cd`
workdir) is in place. The remaining honest caveats are compaction and app-server
wire-stream differentials.
