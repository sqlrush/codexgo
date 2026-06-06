package modelproviderinfo

// Provider capability modeling, mirroring the Rust
// model-provider/src/provider.rs `ProviderCapabilities`. These are the
// provider-owned upper bounds on optional, provider-backed features (namespace
// tools, hosted image generation, hosted web search). Callers may disable more
// through config, but should not expose a feature the active provider marks
// unsupported.
//
// In Rust each `ModelProvider` impl reports its own `capabilities()`: the
// default OpenAI-compatible provider (and any configured provider) uses the
// trait default (all true), while the Amazon Bedrock provider overrides
// image_generation and web_search to false. codexgo selects the provider by id
// rather than a trait object, so the override table here is keyed by provider id
// and falls back to the all-true default for every other provider.

// Capabilities are the provider-owned feature upper bounds. Mirrors Rust
// `ProviderCapabilities` (Debug/Clone/Copy/PartialEq/Eq).
type Capabilities struct {
	// NamespaceTools reports whether the provider supports namespaced tools
	// (the tool_search deferred-tool surface).
	NamespaceTools bool
	// ImageGeneration reports whether the provider supports the hosted
	// image_generation tool.
	ImageGeneration bool
	// WebSearch reports whether the provider supports the hosted web_search
	// tool.
	WebSearch bool
}

// DefaultCapabilities returns the all-true capability set, mirroring the Rust
// `impl Default for ProviderCapabilities` (namespace_tools, image_generation,
// web_search all true) which backs the `ModelProvider::capabilities` trait
// default used by the OpenAI and configured providers.
func DefaultCapabilities() Capabilities {
	return Capabilities{
		NamespaceTools:  true,
		ImageGeneration: true,
		WebSearch:       true,
	}
}

// amazonBedrockCapabilities returns the Amazon Bedrock override, mirroring the
// Rust `impl ModelProvider for AmazonBedrockModelProvider::capabilities`:
// namespace_tools stays on, but hosted image_generation and web_search are
// disabled.
func amazonBedrockCapabilities() Capabilities {
	return Capabilities{
		NamespaceTools:  true,
		ImageGeneration: false,
		WebSearch:       false,
	}
}

// CapabilitiesForProvider returns the capability upper bounds for the provider
// identified by providerID. It mirrors the per-impl `capabilities()` selection
// in Rust: the built-in Amazon Bedrock provider overrides the hosted-tool
// capabilities, while every other provider (OpenAI, OSS, configured) uses the
// all-true default.
func CapabilitiesForProvider(providerID string) Capabilities {
	switch providerID {
	case AmazonBedrockProviderID:
		return amazonBedrockCapabilities()
	default:
		return DefaultCapabilities()
	}
}
