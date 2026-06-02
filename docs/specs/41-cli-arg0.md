# 41 — CLI Entry & arg0 Multitool Dispatch

| | |
|---|---|
| **Phase** | 10 — CLI & peripheral |
| **Status** | Not started |
| **Depends on** | 33, 34 |
| **Size** | M |
| **Drop-in critical** | ★★ (CLI surface + exit codes) |

## 目标 / Goal
Port `codex-cli` + `codex-arg0`: the main binary, the complete subcommand surface,
top-level flags, and the busybox-style arg0 multitool dispatch.

## 源参考 / Source reference
- `reference-codex/codex-rs/cli/src/` (`main.rs`, `lib.rs`, `*_cmd.rs`,
  `exit_status.rs`).
- `reference-codex/codex-rs/arg0/src/lib.rs` (argv0/argv1 dispatch, PATH symlink
  guard).
- `reference-codex/docs/getting-started.md`.

## 功能需求 / Functional requirements
1. Default (no subcommand) → TUI; full subcommand set wired (the rest land in spec 42
   and their owning specs): `exec`/`e`, `review`, `resume`, `fork`, `archive`,
   `unarchive`, `login`/`logout`, `mcp`, `mcp-server`, `app`, `app-server`,
   `remote-control`, `exec-server`, `cloud`/`cloud-tasks`, `plugin`, `marketplace`,
   `apply`/`a`, `doctor`, `sandbox`, `completion`, `update`, `features`, `debug …`,
   `responses-api-proxy`, `stdio-to-uds`, `execpolicy check`, `state-db-recovery`.
2. Top-level flags: `-c key=value` (repeatable), `-p/--profile`,
   `--enable/--disable` (repeatable), `--remote [ws://|wss://|unix://]`,
   `--remote-auth-token-env`, `--strict-config`. Propagate to subcommands like Codex
   (clap-flatten semantics; reject `--remote` on non-TUI subcommands).
3. **arg0 dispatch**: when invoked as `codex-linux-sandbox`, `apply_patch`/
   `applypatch`, `codex-execve-wrapper`, route to the right entrypoint; argv[1]
   dispatch (`FS_HELPER`, apply-patch arg1). PATH symlink guard creating
   session-lifetime aliases.
4. Exit codes: `exit_status` conventions (signals 128+n on Unix; direct on Windows).
5. `completion` for bash/zsh/fish/powershell/elvish.

## 验收方案 / Acceptance criteria
- `codexgo --help` and every `codexgo <cmd> --help` present the same subcommands and
  flags as Codex (golden help-text comparison, modulo binary name).
- arg0 dispatch: invoking the binary under each alias name runs the right tool
  (differential — e.g. `apply_patch` reads a patch from stdin and applies it).
- Exit codes match Codex across representative success/failure/signal cases.
- Shell completion scripts generate for all five shells.

## 风险与难点 / Risks
- cobra/pflag vs clap differ in help formatting and flag propagation; match the
  *structure* (names, arity, defaults) and document cosmetic help differences.
- arg0 trick + PATH symlink guard must work across OSes (Windows has no symlinks in
  the same way — match Codex's Windows behavior).

## 非目标 / Non-goals
- Subcommand bodies owned by other specs (auth 08, mcp 21/35, cloud 43, etc.).
