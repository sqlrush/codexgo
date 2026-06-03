package codemode

// This file documents the JavaScript-feature and runtime-architecture
// differences between this Go port (which embeds the pure-Go goja engine) and
// codex's reference implementation (which embeds a V8 isolate via the `v8`
// crate). These are behavioral deviations a faithful drop-in port must surface;
// nothing here changes the wire format, CLI flags, or exit codes.
//
// Engine architecture
//
//   - V8 isolates run on a dedicated OS thread and expose `IsolateHandle::
//     terminate_execution()` for forcibly unwinding CPU-bound scripts, plus an
//     explicit `perform_microtask_checkpoint()` the host calls after settling a
//     promise. goja runtimes are not thread-safe and own their own microtask
//     (job) queue, which they drain automatically inside `leave()` at the end of
//     every top-level call (RunString, a Go-invoked callback, or a promise
//     resolve/reject). The port therefore (a) confines each cell's runtime to a
//     dedicated goroutine, (b) terminates CPU-bound scripts with rt.Interrupt
//     instead of terminate_execution, and (c) relies on goja's implicit drain
//     rather than an explicit microtask checkpoint. Observable promise-settling
//     semantics are equivalent.
//
//   - ES modules: codex compiles exec source as an async ES module, which gives
//     top-level `await` and `import`. goja has no ES-module loader. The port
//     wraps the source in an async IIFE (`(async () => { ...source... })()`) so
//     top-level await behaves identically. Static/dynamic `import` is therefore
//     unsupported (codex also rejects imports at runtime via its module
//     resolver, so a failing import surfaces as an error in both ports, just via
//     different mechanisms: a SyntaxError at compile time under goja vs. a
//     rejected resolver promise under V8).
//
// JavaScript language and standard library
//
//   - Intl / ICU: V8 ships full ICU data, so `Intl.DateTimeFormat`,
//     `Date.prototype.toLocaleString`, locale-aware collation, number/currency
//     formatting, etc. produce CLDR-accurate output (the reference tests assert
//     French month names with ICU spacing). goja's Intl support is minimal and
//     locale-insensitive, so locale-formatted strings differ. Scripts that only
//     use ISO/UTC formatting (toISOString, getTime, etc.) match.
//
//   - WebAssembly, SharedArrayBuffer, Atomics: removed from globalThis in both
//     ports. V8 implements them (then deletes the globals); goja does not
//     implement them at all, so the deletion is a no-op-equivalent but the end
//     state (absent globals) matches.
//
//   - BigInt, TypedArrays, Proxy, Reflect, generators, async/await, Promise,
//     WeakMap/WeakSet, Map/Set, Symbol, optional chaining, nullish coalescing,
//     and spread are all supported by goja and behave as expected.
//
//   - Regular expressions: goja uses Go's regexp engine with a JS-compatibility
//     shim, falling back to a JS regexp implementation for features Go's RE2
//     lacks (backreferences, lookbehind). Extremely obscure V8 regexp edge cases
//     may differ; common patterns match.
//
//   - Stack traces: error `.stack` formatting differs textually between goja and
//     V8 (frame layout, eval source naming). The port surfaces goja's `.stack`
//     verbatim as the cell error_text, mirroring codex surfacing V8's `.stack`;
//     the value is informational, not a stable contract.
//
//   - Numeric formatting: both engines follow ECMA-262 Number-to-String, so
//     JSON.stringify and String(number) match. The port routes all stored-value
//     and tool-argument marshaling through JSON.stringify / JSON.parse exactly
//     as codex does, so the data contract is identical.
//
// Timers
//
//   - setTimeout/clearTimeout are implemented by spawning a Go timer goroutine
//     per timeout that posts a TimeoutFired command back into the runtime loop,
//     mirroring codex spawning an OS thread per timeout. Callback invocation
//     happens on the runtime goroutine in both ports, preserving single-threaded
//     JS semantics. Pending timers never keep a cell alive on their own.
//
// None of the above affects the RuntimeResponse wire shape, the exec/wait tool
// contract, stored-value isolation, or the nested-tool bridge.

// Deviations returns a short human-readable summary of the goja-vs-V8 gaps. It
// exists so callers (and tests) can surface the deviation note programmatically
// without parsing this file.
func Deviations() string {
	return "code-mode runs on goja (pure Go), not V8. Gaps vs codex: no ES-module " +
		"import (top-level await via async IIFE); minimal Intl/ICU (locale-formatted " +
		"strings differ); no WebAssembly/SharedArrayBuffer/Atomics; goja-style error " +
		".stack text; CPU-bound termination via interrupt; per-timeout goroutines. " +
		"RuntimeResponse wire format and the exec/wait/stored-value/nested-tool " +
		"contracts are identical."
}
