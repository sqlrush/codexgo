// Package applypatch is a faithful, drop-in-compatible Go port of OpenAI
// Codex's `apply_patch` implementation (the Rust crate
// `codex-rs/apply-patch`) as part of a reimplementation of Codex 0.136.0.
//
// The model emits `apply_patch` envelopes directly, so the grammar and the
// application semantics implemented here must match Codex byte-for-byte. The
// patch envelope grammar (mirroring the official Lark grammar from
// `parser.rs`) is:
//
//	start: begin_patch environment_id? hunk+ end_patch
//	begin_patch: "*** Begin Patch" LF
//	environment_id: "*** Environment ID: " filename LF
//	end_patch: "*** End Patch" LF?
//
//	hunk: add_hunk | delete_hunk | update_hunk
//	add_hunk: "*** Add File: " filename LF add_line+
//	delete_hunk: "*** Delete File: " filename LF
//	update_hunk: "*** Update File: " filename LF change_move? change?
//	filename: /(.+)/
//	add_line: "+" /(.+)/ LF -> line
//
//	change_move: "*** Move to: " filename LF
//	change: (change_context | change_line)+ eof_line?
//	change_context: ("@@" | "@@ " /(.+)/) LF
//	change_line: ("+" | "-" | " ") /(.+)/ LF
//	eof_line: "*** End of File" LF
//
// As in Codex, the parser is a little more lenient than the explicit spec and
// allows for leading/trailing whitespace around patch markers, as well as
// stripping a surrounding heredoc wrapper (see [ParsePatch]).
//
// # Port scope
//
// This package ports parser.rs (the envelope grammar/parser), seek_sequence.rs
// (context-line seeking with Unicode fuzzy matching), and the type +
// apply-to-filesystem logic from lib.rs.
//
// The following Codex modules are deliberately NOT ported here and remain
// follow-ons:
//
//   - invocation.rs — uses tree-sitter-bash to detect `apply_patch` invocations
//     inside shell command argv. Detecting the invocation is out of scope; this
//     package operates on patch text that has already been extracted.
//   - streaming_parser.rs — the incremental/streaming patch parser.
//
// # Immutability
//
// Following the codebase conventions, operations never mutate their caller's
// inputs: parsing returns fresh values, and applying replacements copies before
// editing.
package applypatch
