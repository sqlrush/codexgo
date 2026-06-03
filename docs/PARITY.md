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
| `apply_patch` envelope | upstream test corpus | ✅ ported from codex's own `#[cfg(test)]` cases (internal/applypatch). Standalone arg0 differential pending. |
| execpolicy decisions | `codex execpolicy check` | ⏳ pending (wire the differential). |

## Pending (need a one-time authenticated recording — maintainer)

These exercise a real model turn, so they require a single authenticated capture
against the real codex (ChatGPT login or API key) to record golden traffic; after
that, differentials run deterministically offline:

- `codex exec --json "<prompt>"` JSONL event stream (turn lifecycle, items, usage).
- `Op`/`EventMsg` wire stream over the app-server.
- Auto-compaction trigger points and `ContextCompacted` payloads.
- Tool-call (exec_command / apply_patch) end-to-end under sandbox.

## Harness

- `internal/paritytest` provides golden helpers (`AssertBytes`, `AssertJSONEqual`,
  `CanonicalizeJSON`).
- Per-spec golden tests run in CI against committed fixtures where the fixture
  contains no OpenAI content; codex-output fixtures are env-gated
  (`CODEX_PARITY_BIN=/path/to/codex`) and regenerated locally.

## Honest status

Format/CLI-surface parity is being validated and is largely faithful (model
catalog identical; subcommand set being completed). The **turn-level behavioral
drop-in proof is not yet done** because it needs an authenticated model recording —
this is the single largest remaining gap to a fully-evidenced "100% drop-in".
