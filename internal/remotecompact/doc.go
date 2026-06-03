// Package remotecompact is a faithful, drop-in-compatible Go port of the remote
// conversation-compaction transport used by codex 0.136.0.
//
// It bundles two server-driven compaction paths plus the shared, pure history
// transformations both rely on:
//
//   - v1 (the "/responses/compact" endpoint): a unary HTTP call that posts the
//     current transcript to a dedicated compaction endpoint and parses a fresh
//     list of [protocol.ResponseItem] back. This mirrors codex-api's
//     CompactClient / CompactionInput plus the inline helpers from
//     core/compact_remote.rs.
//   - v2 (remote compaction over the normal Responses stream): collects a single
//     compaction output item from a streamed response and rebuilds the retained
//     history locally, with token-budget truncation. This mirrors
//     core/compact_remote_v2.rs.
//
// The package deliberately depends only on the public APIs of internal/api,
// internal/client, internal/protocol, and internal/utils/truncation. The
// session/turn/hook/analytics orchestration that wraps these primitives in the
// reference lives in the core package and is intentionally out of scope here:
// this package exposes the transport, the request/response types, and the pure
// transcript reducers so callers can wire them into a session.
//
// All exported functions follow the project's immutability rules: inputs are
// never mutated in place; new slices and items are returned instead.
package remotecompact
