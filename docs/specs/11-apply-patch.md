# 11 — `apply_patch` Envelope

| | |
|---|---|
| **Phase** | 3 — Execution & sandboxing |
| **Status** | Not started |
| **Depends on** | 01 |
| **Size** | M |
| **Drop-in critical** | ★★ (byte-compatible patch format) |

## 目标 / Goal
Port `codex-apply-patch`: parse and apply the Codex patch envelope
(`*** Begin Patch` … `*** End Patch`) with add/update/delete/move operations and
context-seeking hunks. The grammar and application semantics must be byte-compatible
since the model emits these directly.

## 源参考 / Source reference
- `reference-codex/codex-rs/apply-patch/src/parser.rs` (the Lark grammar + lenient
  parsing).
- `apply-patch/src/` application logic, delta tracking, Unicode normalization.

## 功能需求 / Functional requirements
1. Parse the grammar exactly:
   - `*** Begin Patch` / `*** End Patch`, optional `*** Environment ID: <file>`.
   - `*** Add File: <path>` + `+`-prefixed lines.
   - `*** Delete File: <path>`.
   - `*** Update File: <path>` + optional `*** Move to: <path>` + change blocks of
     `@@`/`@@ <ctx>` context markers and `+`/`-`/` ` change lines, optional
     `*** End of File` terminator.
2. Lenient mode (strip surrounding whitespace around markers), context-seeking
   chunk application, Unicode fuzzy matching (EN DASH / non-breaking hyphen).
3. EOF handling: final empty line omitted from the line array and re-added on
   serialization.
4. Delta tracking: capture before/after, mark partial/inexact application
   (`AppliedPatchDelta.exact`), perform move = write-dest + delete-src.
5. Available both as a library (for the tool) and as the `apply_patch`/`applypatch`
   arg0 multitool entry (spec 41) and `codex apply` (`git apply` of latest diff).

## 验收方案 / Acceptance criteria
- Golden corpus: every captured `apply_patch` input from Codex applied by `codexgo`
  yields byte-identical file results and the same `exact`/inexact status.
- Marker strings match byte-for-byte (`"*** Begin Patch"`, `"*** Update File: "`,
  etc.).
- Move + delete-source semantics match, including failure tracking.
- Malformed-patch error messages are in the same class as Codex.

## 风险与难点 / Risks
- The lenient parser + context-seeking has many edge cases; build the test corpus
  early from upstream tests.
- Unicode normalization must match the exact code points Codex special-cases.

## 非目标 / Non-goals
- Approval UI for patches (specs 30/38).
