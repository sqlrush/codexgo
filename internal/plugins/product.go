package plugins

// Product gating used by marketplace plugin policies. Mirrors the subset of
// `codex_protocol::protocol::Product` that core-plugins relies on. The protocol
// package does not (yet) expose this type, so it is defined locally to keep this
// package self-contained without modifying other packages.

import "strings"

// Product mirrors the Rust `Product` enum. JSON is lowercase, with uppercase
// aliases accepted on input (CHATGPT, CODEX, ATLAS).
type Product int

const (
	// ProductChatgpt is the ChatGPT product.
	ProductChatgpt Product = iota
	// ProductCodex is the Codex product.
	ProductCodex
	// ProductAtlas is the Atlas product.
	ProductAtlas
)

// productNames maps each product to its canonical lowercase serde name.
var productNames = map[Product]string{
	ProductChatgpt: "chatgpt",
	ProductCodex:   "codex",
	ProductAtlas:   "atlas",
}

// String returns the canonical lowercase name.
func (p Product) String() string {
	if name, ok := productNames[p]; ok {
		return name
	}
	return "chatgpt"
}

// ToAppPlatform mirrors the Rust `Product::to_app_platform`.
func (p Product) ToAppPlatform() string {
	switch p {
	case ProductChatgpt:
		return "chat"
	case ProductCodex:
		return "codex"
	case ProductAtlas:
		return "atlas"
	default:
		return "chat"
	}
}

// ProductFromSessionSourceName mirrors the Rust
// `Product::from_session_source_name`: case-insensitive parse of a session
// source name.
func ProductFromSessionSourceName(value string) (Product, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "chatgpt":
		return ProductChatgpt, true
	case "codex":
		return ProductCodex, true
	case "atlas":
		return ProductAtlas, true
	default:
		return 0, false
	}
}

// MatchesProductRestriction mirrors the Rust
// `Product::matches_product_restriction`: an empty restriction matches all
// products; otherwise the product must be listed.
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

// parseProduct parses a JSON-serialized product string (canonical lowercase or
// uppercase alias).
func parseProduct(value string) (Product, bool) {
	switch value {
	case "chatgpt", "CHATGPT":
		return ProductChatgpt, true
	case "codex", "CODEX":
		return ProductCodex, true
	case "atlas", "ATLAS":
		return ProductAtlas, true
	default:
		return 0, false
	}
}
