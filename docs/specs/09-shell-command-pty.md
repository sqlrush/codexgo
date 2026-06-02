# 09 — Shell Command Parsing & PTY

| | |
|---|---|
| **Phase** | 3 — Execution & sandboxing |
| **Status** | Not started |
| **Depends on** | 01 |
| **Size** | M |
| **Drop-in critical** | partial |

## 目标 / Goal
Port `codex-shell-command`, `codex-shell-escalation`, and `utils/pty`: parse/tokenize
shell commands (for policy + display), detect privilege escalation, and allocate
PTYs for command execution.

## 源参考 / Source reference
- `reference-codex/codex-rs/shell-command/src/` (tree-sitter-bash tokenization,
  arg0 resolution, URL extraction, login-shell detection).
- `reference-codex/codex-rs/shell-escalation/src/` (sudo/su detection, execve
  wrapper).
- `reference-codex/codex-rs/utils/pty/`.

## 功能需求 / Functional requirements
1. Tokenize bash command lines compatibly enough for execpolicy matching and TUI
   display: resolve `arg0`, split commands, extract URLs, detect login shells.
   (Use a Go bash parser, e.g. `mvdan.cc/sh`, or tree-sitter-bash bindings.)
2. Detect privilege escalation patterns (`sudo`, `su`, `doas`, env-prefixed forms)
   matching `shell-escalation`.
3. PTY allocation/control: spawn under a PTY, set window size, read/write, resize,
   close (`creack/pty` or native ioctls). Support pipe mode (no TTY) as well.
4. Honor `exec_command` params: `tty` flag, `login` shell, `workdir`, `shell`,
   `environment_id`, `yield_time_ms`, `max_output_tokens`.

## 验收方案 / Acceptance criteria
- Tokenization of a command corpus yields the same token splits Codex feeds to
  execpolicy (golden, validated through spec 03 decisions).
- Escalation detection matches Codex on a labeled command set.
- PTY-run commands produce output capture equivalent to Codex (size negotiation,
  EOF handling) on a scripted set.

## 风险与难点 / Risks
- Exact tree-sitter-bash parse trees are hard to reproduce; match only the derived
  facts Codex actually uses (tokens, arg0, URLs), and snapshot those.
- Windows PTY (ConPTY) handling differs; coordinate with spec 14.

## 非目标 / Non-goals
- The sandbox wrapping of the spawned process (specs 12–14).
