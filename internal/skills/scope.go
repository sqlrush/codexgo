// Package skills is a faithful, drop-in-compatible Go port of the OpenAI Codex
// Rust crates codex-skills and codex-core-skills (codex 0.136.0).
//
// It models on-disk skills (a SKILL.md file with YAML front-matter plus an
// optional agents/openai.yaml metadata sidecar), discovers them from a set of
// roots with scope-based precedence, applies enable/disable rules from config,
// detects implicit skill invocations from shell commands, and renders the
// model-visible "Available skills" instruction block with token-budget aware
// truncation.
//
// Built-in (system) skills are embedded via go:embed and installed into
// CODEX_HOME/skills/.system at startup, mirroring the Rust include_dir approach.
//
// All exported values follow Codex's immutability conventions: operations return
// new values rather than mutating their receivers or arguments.
package skills

// SkillScope identifies where a skill came from. It mirrors the Rust
// `codex_protocol::protocol::SkillScope` enum, including its
// `rename_all = "snake_case"` serde representation.
type SkillScope string

const (
	// SkillScopeUser is a user-installed skill (CODEX_HOME or $HOME/.agents).
	SkillScopeUser SkillScope = "user"
	// SkillScopeRepo is a skill discovered in the project/repository tree.
	SkillScopeRepo SkillScope = "repo"
	// SkillScopeSystem is an embedded built-in skill installed by Codex.
	SkillScopeSystem SkillScope = "system"
	// SkillScopeAdmin is a skill installed via the system config layer
	// (for example /etc/codex/skills).
	SkillScopeAdmin SkillScope = "admin"
)

// Product mirrors the Rust `codex_protocol::protocol::Product` enum used to gate
// skills by surface. It serializes lowercase to match Codex.
type Product string

const (
	// ProductChatgpt is the ChatGPT surface.
	ProductChatgpt Product = "chatgpt"
	// ProductCodex is the Codex surface.
	ProductCodex Product = "codex"
	// ProductAtlas is the Atlas surface.
	ProductAtlas Product = "atlas"
)

// MatchesProductRestriction reports whether p satisfies a product restriction
// list. An empty list matches every product, mirroring
// `Product::matches_product_restriction`.
func (p Product) MatchesProductRestriction(products []Product) bool {
	if len(products) == 0 {
		return true
	}
	for _, candidate := range products {
		if candidate == p {
			return true
		}
	}
	return false
}

// scopeRank orders scopes for dedupe/root scan order. Higher-priority scopes
// come first (lower rank). Mirrors `scope_rank` in loader.rs.
func scopeRank(scope SkillScope) int {
	switch scope {
	case SkillScopeRepo:
		return 0
	case SkillScopeUser:
		return 1
	case SkillScopeSystem:
		return 2
	case SkillScopeAdmin:
		return 3
	default:
		return 4
	}
}

// promptScopeRank orders scopes for prompt rendering. Mirrors
// `prompt_scope_rank` in render.rs.
func promptScopeRank(scope SkillScope) int {
	switch scope {
	case SkillScopeSystem:
		return 0
	case SkillScopeAdmin:
		return 1
	case SkillScopeRepo:
		return 2
	case SkillScopeUser:
		return 3
	default:
		return 4
	}
}
