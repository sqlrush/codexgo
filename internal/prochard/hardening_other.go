//go:build !linux && !android && !darwin && !freebsd && !openbsd

package prochard

// preMainHardening is a graceful no-op on platforms where codex performs no
// (or not-yet-implemented) hardening, such as Windows and NetBSD.
//
// On Windows, codex has a TODO to perform appropriate configuration; until then
// this mirrors the upstream no-op behavior.
func preMainHardening() {}
