# codexgo

A ground-up reimplementation of the [OpenAI Codex](https://github.com/openai/codex)
coding agent in **Go**, targeting **100% feature parity** with — and **drop-in
compatibility** for — the latest Codex release.

> **Status:** 📐 Planning / roadmap stage. No implementation code yet.
> This repository currently contains the **roadmap and implementation specs**
> that decompose the full port into executable, independently verifiable units.

---

## Why

Codex is a powerful terminal coding agent, but it ships as a ~935K-line Rust
workspace (113 crates) with deep platform dependencies. `codexgo` aims to deliver
the same behavior as a single statically-linked Go binary with:

- **No external runtime dependencies** — sandboxing, JS execution, WebRTC, and the
  network proxy are all reimplemented natively in Go (no shelling out to `bwrap`,
  the Rust `codex-linux-sandbox` binary, V8, etc.).
- **Drop-in compatibility** — `codexgo` reads and writes the same on-disk formats
  and speaks the same wire protocols as Codex, so it can share a `~/.codex`
  directory and plug into the existing Codex ecosystem.

## Reference target

| | |
|---|---|
| Upstream | [`openai/codex`](https://github.com/openai/codex) |
| Frozen reference version | **0.136.0** (`rust-v0.136.0`, published 2026-06-01) |
| Upstream language | Rust (`codex-rs/`, 113 crates, ~935K LOC) |
| Target language | Go 1.26+ |

Codex iterates very quickly, so the roadmap pins **0.136.0** as the frozen
parity target. Later versions are tracked as roadmap deltas once the baseline lands.

## Design decisions

These four decisions, made up front, shape the entire roadmap:

1. **Phasing — core engine first.** Headless agent engine (agent loop → model
   client → tools → exec → sandbox → config → protocol → MCP) → headless server &
   `exec` → TUI → CLI/cloud/peripheral.
2. **Fidelity — full native Go rewrite.** Hard-to-port, platform-specific pieces
   (seatbelt, landlock+seccomp, Windows restricted-token sandbox, WebRTC, the V8
   "code mode", the network proxy) are reimplemented natively rather than wrapped.
3. **Compatibility — full drop-in.** Byte/behavior-level parity for `config.toml`,
   `auth.json`, rollout JSONL, `history.jsonl`, the SQLite state schema, the
   app-server JSON-RPC protocol, MCP, the `Op`/`Event` wire protocol, the
   `apply_patch` envelope, and the CLI surface.
4. **Output — master roadmap + per-spec files.** [`docs/ROADMAP.md`](docs/ROADMAP.md)
   plus one file per work unit under [`docs/specs/`](docs/specs/).

## Repository layout

```
codexgo/
├── README.md            ← you are here
├── docs/
│   ├── ROADMAP.md       ← master roadmap: phases, principles, spec index
│   └── specs/           ← 49 implementation specs (00–48)
└── reference-codex/     ← gitignored shallow clone of openai/codex (analysis only)
```

## Where to start

Read [`docs/ROADMAP.md`](docs/ROADMAP.md) for the full plan, then browse the
[`docs/specs/`](docs/specs/) directory. Specs are dependency-ordered: a fresh
contributor can pick the lowest-numbered spec with all dependencies satisfied.

## Relationship to OpenAI Codex

`codexgo` is an **independent reimplementation** that studies the public
`openai/codex` source (Apache-2.0) to achieve behavioral and format compatibility.
It is not affiliated with or endorsed by OpenAI. The upstream source is **not**
redistributed in this repository.
