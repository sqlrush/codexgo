# 00 — Project Foundation & Parity Harness

| | |
|---|---|
| **Phase** | 0 — Foundation |
| **Status** | Not started |
| **Depends on** | — |
| **Size** | M |
| **Drop-in critical** | ★ (defines how parity is proven) |

## 目标 / Goal
Establish the Go module, repository layout, build/CI tooling, and — most
importantly — the **differential parity harness** that every later spec uses to
prove byte/protocol/behavioral compatibility with Codex 0.136.0.

## 源参考 / Source reference
- Workspace layout: `reference-codex/codex-rs/Cargo.toml` (113 members), `justfile`.
- Test conventions across crates (`*/tests/`, `insta` snapshots) inform our
  golden-file approach.

## 功能需求 / Functional requirements
1. Go module `github.com/sqlrush/codexgo` (Go 1.26+), `go.work` optional.
2. Package layout mirroring the crate domains under `internal/` (e.g.
   `internal/protocol`, `internal/config`, `internal/core`, `internal/sandbox`,
   `internal/tui`, `cmd/codex`). One domain per package; small files.
3. CI (GitHub Actions): `go build ./...`, `go vet`, `go test ./...` with race
   detector, `gofmt`/`golangci-lint`, cross-compile matrix (darwin/linux/windows ×
   amd64/arm64).
4. **Parity harness** (`internal/paritytest`):
   - A `referencecapture` tool that, given a path to a real `codex` 0.136.0 binary,
     produces golden fixtures: example `config.toml`/`auth.json`, rollout files,
     `apply_patch` cases, tool JSON schemas (`codex debug ...` dumps), and captured
     JSON-RPC / `Op`/`Event` streams.
   - A `golden` helper: load a fixture, run the Go implementation, assert byte
     equality or canonicalized semantic equality; record explicit, reviewed
     deviations in a `DEVIATIONS.md`.
   - A `differential` helper: run a scenario against both `codex` and `codexgo`,
     diff observable outputs (files, stdout JSONL, exit code).
5. Fixtures stored under `testdata/golden/` (committed) with provenance notes.

## 验收方案 / Acceptance criteria
- `go build ./...` and `go test ./...` pass green in CI on all platforms in the
  matrix.
- `referencecapture --codex $(which codex)` regenerates the committed fixture set
  with no diff (deterministic capture).
- The `golden` helper can fail a deliberately-corrupted fixture (negative test).
- `DEVIATIONS.md` exists and is empty (no deviations yet) but wired into review.

## 风险与难点 / Risks
- Some fixtures require ChatGPT auth or network; mark these as opt-in
  (env-gated) so CI stays hermetic; provide offline recorded variants.
- `encoding/json` does not preserve struct/map key order; the canonicalizer must
  normalize both sides for semantic comparisons.

## 非目标 / Non-goals
- Any agent functionality. This spec is scaffolding + test infrastructure only.
