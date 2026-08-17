package modelproviderinfo

import "testing"

// TestDefaultCapabilities asserts the all-true default, mirroring the Rust
// `ProviderCapabilities::default()`.
func TestDefaultCapabilities(t *testing.T) {
	got := DefaultCapabilities()
	want := Capabilities{NamespaceTools: true, ImageGeneration: true, WebSearch: true}
	if got != want {
		t.Fatalf("DefaultCapabilities() = %+v, want %+v", got, want)
	}
}

// TestCapabilitiesForProvider asserts the per-provider selection: OpenAI/OSS and
// any unknown/configured provider use the all-true default, while Amazon Bedrock
// disables the hosted image_generation and web_search tools (namespace_tools
// stays on). Mirrors the Rust per-impl `capabilities()`.
func TestCapabilitiesForProvider(t *testing.T) {
	tests := []struct {
		name       string
		providerID string
		want       Capabilities
	}{
		{
			name:       "openai uses default capabilities",
			providerID: OpenAIProviderID,
			want:       Capabilities{NamespaceTools: true, ImageGeneration: true, WebSearch: true},
		},
		{
			name:       "ollama uses default capabilities",
			providerID: OllamaOSSProviderID,
			want:       Capabilities{NamespaceTools: true, ImageGeneration: true, WebSearch: true},
		},
		{
			name:       "configured/unknown provider uses default capabilities",
			providerID: "my-custom-provider",
			want:       Capabilities{NamespaceTools: true, ImageGeneration: true, WebSearch: true},
		},
		{
			name:       "amazon bedrock disables hosted tools",
			providerID: AmazonBedrockProviderID,
			want:       Capabilities{NamespaceTools: true, ImageGeneration: false, WebSearch: false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CapabilitiesForProvider(tt.providerID); got != tt.want {
				t.Errorf("CapabilitiesForProvider(%q) = %+v, want %+v", tt.providerID, got, tt.want)
			}
		})
	}
}
