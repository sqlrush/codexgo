# 44 — Telemetry & Feedback

| | |
|---|---|
| **Phase** | 10 — CLI & peripheral |
| **Status** | Not started |
| **Depends on** | 04 |
| **Size** | M |
| **Drop-in critical** | partial (opt-out + event shapes) |

## 目标 / Goal
Port `codex-analytics`, `codex-otel`, `codex-feedback`, and `codex-process-hardening`:
telemetry/metrics/tracing (opt-out by default), bug-report submission, and process
hardening.

## 源参考 / Source reference
- `reference-codex/codex-rs/analytics/src/` (event client, `TrackEventsContext`,
  fact types, diff fingerprints).
- `otel/src/` (OTLP/Statsig exporters, `OtelSettings`, W3C traceparent),
  `feedback/src/` (Sentry), `process-hardening/src/`.

## 功能需求 / Functional requirements
1. **analytics**: event client emitting the same event/fact types
   (`SkillInvocation`, `HookRunFact`, `TurnTokenUsageFact`, `GuardianReviewEvent`,
   diff fingerprints); **disabled by default**, opt-in via `[analytics]`.
2. **otel**: metrics + tracing via `go.opentelemetry.io/otel`; OTLP HTTP + Statsig
   exporters; `OtelSettings`/exporter selection; W3C traceparent parse/propagate;
   honor standard `OTEL_*` env vars.
3. **feedback**: collect logs (4 MiB cap) + `doctor --json` + sandbox logs and submit
   to Sentry (same DSN/tags) for `/feedback` and `codex feedback`.
4. **process-hardening**: disable ptrace, prevent core dumps, sanitize dangerous env
   (`LD_*`, `DYLD_*`).

## 验收方案 / Acceptance criteria
- Telemetry is off unless explicitly enabled; when enabled, emitted event shapes
  match captures.
- traceparent parsing/propagation matches Codex; `OTEL_*` env honored.
- Feedback bundle contents match Codex's attachment set.
- Process hardening applies the same protections (verify ptrace/core-dump/env on
  Linux).

## 风险与难点 / Risks
- Respect privacy defaults exactly — never emit telemetry without opt-in.
- Sentry/OTLP wire formats handled by mature Go libs; verify field/tag parity.

## 非目标 / Non-goals
- The events are *produced* by core/tools; this owns transport/export only.
