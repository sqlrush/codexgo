package core

// Provider capability surface for the turn path, mirroring the Rust
// model-provider/src/provider.rs `ProviderCapabilities` upper bounds that
// spec_plan reads through `turn_context.provider.capabilities()`.
//
// In Rust the capabilities live on the `ModelProvider` trait object carried by
// the TurnContext. codexgo keeps `core` decoupled from the model-provider crate
// (mirroring how WebSearchMode is a resolved value, not a provider object): the
// resolved [ProviderCapabilities] flow from the assembly layer (which calls
// modelproviderinfo.CapabilitiesForProvider) through SessionConfiguration into
// the TurnContext. The zero value (all false) is treated as "not configured"
// and resolves to codex's all-true default so unit tests and clients that build
// a config directly keep the OpenAI-style upper bound.

// ProviderCapabilities are the active provider's feature upper bounds. Mirrors
// the Rust `ProviderCapabilities` struct field-for-field (namespace_tools,
// image_generation, web_search).
type ProviderCapabilities struct {
	// NamespaceTools gates the tool_search deferred-tool surface
	// (provider.capabilities().namespace_tools).
	NamespaceTools bool
	// ImageGeneration gates the hosted image_generation tool
	// (provider.capabilities().image_generation).
	ImageGeneration bool
	// WebSearch gates the hosted web_search tool
	// (provider.capabilities().web_search).
	WebSearch bool
}

// defaultProviderCapabilities returns the all-true upper bound, mirroring the
// Rust `ProviderCapabilities::default()` that backs the OpenAI/configured
// providers' `capabilities()`.
func defaultProviderCapabilities() ProviderCapabilities {
	return ProviderCapabilities{
		NamespaceTools:  true,
		ImageGeneration: true,
		WebSearch:       true,
	}
}

// providerCapabilitiesResolver maps a provider id to its capability upper
// bounds. It is the injection seam the assembly layer wires to
// modelproviderinfo.CapabilitiesForProvider, keeping `core` decoupled from the
// model-provider package (mirroring how the Rust TurnContext carries a
// ModelProvider trait object whose `capabilities()` core reads). The default
// returns the all-true upper bound so a bare run / directly built config keeps
// the OpenAI-style capabilities.
var providerCapabilitiesResolver = func(_ string) ProviderCapabilities {
	return defaultProviderCapabilities()
}

// SetProviderCapabilitiesResolver installs the resolver used to derive a turn's
// provider capabilities from its provider id. The assembly layer calls this once
// at startup with modelproviderinfo.CapabilitiesForProvider; tests may override
// it to simulate a capabilities-off provider. Passing nil restores the all-true
// default.
func SetProviderCapabilitiesResolver(resolve func(providerID string) ProviderCapabilities) {
	if resolve == nil {
		providerCapabilitiesResolver = func(string) ProviderCapabilities {
			return defaultProviderCapabilities()
		}
		return
	}
	providerCapabilitiesResolver = resolve
}

// resolveProviderCapabilities returns the capability upper bounds for a provider
// id via the installed resolver.
func resolveProviderCapabilities(providerID string) ProviderCapabilities {
	return providerCapabilitiesResolver(providerID)
}

// turnProviderCapabilities resolves the effective provider capabilities for a
// turn. A zero-value capabilities struct (the unconfigured default a directly
// built TurnContext/SessionConfiguration carries) resolves to the codex all-true
// default; otherwise the configured upper bounds are returned verbatim. Mirrors
// reading `turn_context.provider.capabilities()` with the OpenAI default.
func turnProviderCapabilities(tc *TurnContext) ProviderCapabilities {
	if tc == nil {
		return defaultProviderCapabilities()
	}
	if tc.ProviderCapabilities == (ProviderCapabilities{}) {
		return defaultProviderCapabilities()
	}
	return tc.ProviderCapabilities
}
