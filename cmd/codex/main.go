// Command codex is the codexgo entry point: a Go reimplementation of the OpenAI
// Codex coding agent. The full CLI surface is built out across docs/ROADMAP.md
// Phase 10 (spec 41); this is the scaffolding stub from Phase 0 (spec 00).
package main

import "fmt"

// version is the codexgo build version. Parity target: Codex 0.136.0.
const version = "0.0.0-dev"

func main() {
	fmt.Printf("codexgo %s — parity target: codex 0.136.0\n", version)
	fmt.Println("not yet implemented; see docs/ROADMAP.md")
}
