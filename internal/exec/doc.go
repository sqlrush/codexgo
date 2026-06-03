// Package exec ports `codex exec`, the non-interactive Codex agent runner
// (codex-rs/exec). It runs a single agent turn from a prompt (argument or stdin)
// plus optional images, driving the engine through the in-process app-server
// client (internal/appserverclient) and transforming the engine's event stream
// into output.
//
// Output modes:
//   - --json: a JSONL stream, one [ThreadEvent] per line (thread.started,
//     turn.started, item.started/updated/completed, turn.completed/turn.failed,
//     error), byte-for-byte compatible with codex exec's --json output.
//   - default: a human-readable summary with the final agent message on stdout.
//
// Both modes can write the final message to a file (--output-last-message) and
// constrain the model's response with a JSON Schema (--output-schema). The
// subcommands `resume` and `review` are supported alongside the default session.
//
// Architecture: [Run] parses-validated CLI + an [Environment] into a [Session]
// over an [EventSink]. The [Session] performs the initialize/thread/turn JSON-RPC
// handshake and runs the event loop; the [JSONLProcessor] is the pure
// transformation from engine [protocol.Event]s to [ThreadEvent]s, mirroring the
// Rust EventProcessorWithJsonOutput. Because the Go app-server forwards engine
// events in the v1 EventMsg wire format (as `codex/event` notifications) rather
// than the v2 typed ServerNotification union, the processor maps directly from
// EventMsg; the emitted JSONL is identical to the reference.
package exec
