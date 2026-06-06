// Package cliutil provides small command-line helper utilities shared across
// the Codex CLI entry points.
//
// It is a faithful Go port of the codex-utils-cli Rust crate. The crate
// collects a handful of CLI building blocks:
//
//   - [ApprovalModeCliArg] and [SandboxModeCliArg]: the kebab-case CLI flag
//     values for --approval-mode and --sandbox, plus their conversion into the
//     corresponding protocol enums ([AskForApproval], [SandboxMode]).
//   - [ProfileV2Name]: a validated --profile name used to select a
//     $CODEXGO_HOME/<name>.config.toml layer.
//   - [CliConfigOverrides]: capture and parsing of repeated `-c key=value`
//     overrides whose right-hand sides are interpreted as TOML, plus the logic
//     to apply them onto a TOML value tree.
//   - [FormatEnvDisplay]: rendering of an environment map/list as a redacted,
//     sorted display string.
//   - [ResumeCommand] / [ResumeHint]: user-facing `codex resume` hint strings.
//   - [SharedCliOptions]: flags shared by the interactive and non-interactive
//     entry points, with the precedence-merging helpers used to combine root,
//     subcommand, and inherited options.
//
// The upstream crate depends on several types from sibling crates (codex_protocol
// and codex_shell_command). To keep this package self-contained and dependent
// only on the Go standard library, the minimal subset of those types and
// behaviors required here is reimplemented locally. The externally observable
// behavior — flag value spellings, TOML override parsing semantics, the redacted
// environment display format, and the exact `codex resume` command strings
// (including shell quoting) — is preserved so callers behave identically across
// the two implementations.
//
// All exported helpers follow an immutable style: they return new values and
// never mutate slices, maps, or structs owned by the caller.
package cliutil
