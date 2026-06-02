# 01 — Foundation Utilities

| | |
|---|---|
| **Phase** | 0 — Foundation |
| **Status** | Not started |
| **Depends on** | 00 |
| **Size** | M |
| **Drop-in critical** | partial (path/string/truncation semantics affect parity) |

## 目标 / Goal
Port the leaf utility crates that nearly everything else depends on, with matching
semantics. These have no internal Codex dependencies and unblock the contract layer.

## 源参考 / Source reference
`reference-codex/codex-rs/utils/`:
`absolute-path`, `path-utils`, `string`, `home-dir`, `output-truncation`,
`template`, `stream-parser`, `fuzzy-match`, `cache`, `elapsed`, `image`,
`json-to-toml`, `sandbox-summary`, `approval-presets`, `oss`, `sleep-inhibitor`,
`readiness`, `rustls-provider`, `pty` (PTY lives in spec 09), `plugins`, `cli`.
Also `async-utils`, `terminal-detection`, `install-context`, `process-hardening`
(may move to their consuming specs).

## 功能需求 / Functional requirements
1. `path` — `AbsolutePathBuf` equivalent: absolute-path invariants, `dunce`-style
   Windows UNC normalization, `normalize_for_native_workdir`, workspace-root
   symbolic expansion. **Serialization must match** (used in protocol/rollout).
2. `string` — text helpers used in prompts/rendering (encoding detection via
   `chardetng`/`encoding_rs` analog, line handling).
3. `outputtruncation` — the exact tool-output truncation algorithm (token/byte
   budgets) used by tools and rollout (`aggregated_output` ≤ 10KB rule).
4. `template` — the templating used by skills/login/models prompts.
5. `streamparser` — incremental parser utilities for SSE/markdown streaming.
6. `fuzzymatch` — scoring used by `@mention` and pickers (parity with `nucleo`
   ranking is a stretch goal; document scoring differences).
7. `cache`, `elapsed`, `image` (decode/encode + base64 for `view_image`/paste),
   `jsontotoml`, `homedir` (`CODEX_HOME` resolution), `approvalpresets`,
   `sandboxsummary`, `oss`, `sleepinhibitor`, `readiness`.

## 验收方案 / Acceptance criteria
- Unit tests for each utility; table-driven, ≥80% coverage.
- `homedir` resolves `CODEX_HOME` with the same precedence as Codex (env override →
  default `~/.codex`); golden test against captured paths.
- `outputtruncation` reproduces Codex truncation byte-for-byte on captured
  command outputs (golden fixtures).
- `path` round-trips a corpus of Codex-serialized `AbsolutePathBuf` JSON values.
- `image` base64-PNG encoding matches Codex clipboard-paste payloads on sample
  images.

## 风险与难点 / Risks
- `nucleo` fuzzy ranking is hard to match exactly; treat ranking parity as
  best-effort with documented deviations.
- Encoding detection libraries differ; pick one and snapshot behavior.

## 非目标 / Non-goals
- PTY (spec 09), keyring (spec 08), rustls specifics beyond a thin TLS config
  helper.
