package modelproviderinfo

import (
	"testing"
)

func TestBuiltInModelProviders(t *testing.T) {
	providers := BuiltInModelProviders(nil)

	for _, id := range []string{OpenAIProviderID, AmazonBedrockProviderID, OllamaOSSProviderID, LMStudioOSSProviderID} {
		if _, ok := providers[id]; !ok {
			t.Fatalf("missing built-in provider %q", id)
		}
	}
	if len(providers) != 4 {
		t.Fatalf("expected 4 built-in providers, got %d", len(providers))
	}

	bedrock, ok := providers[AmazonBedrockProviderID]
	if !ok || !bedrock.IsAmazonBedrock() {
		t.Fatalf("amazon bedrock provider missing or misidentified")
	}
	if openai := providers[OpenAIProviderID]; !openai.IsOpenAI() {
		t.Fatalf("openai provider misidentified")
	}

	ollama := providers[OllamaOSSProviderID]
	if ollama.BaseURL == nil || *ollama.BaseURL != "http://localhost:11434/v1" {
		t.Fatalf("ollama base url: %v", ollama.BaseURL)
	}
	lmstudio := providers[LMStudioOSSProviderID]
	if lmstudio.BaseURL == nil || *lmstudio.BaseURL != "http://localhost:1234/v1" {
		t.Fatalf("lmstudio base url: %v", lmstudio.BaseURL)
	}
}

func TestCreateAmazonBedrockProvider(t *testing.T) {
	got := CreateAmazonBedrockProvider(nil)
	want := ModelProviderInfo{
		Name:    "Amazon Bedrock",
		BaseURL: strptr("https://bedrock-mantle.us-east-1.api.aws/openai/v1"),
		Aws:     &ModelProviderAwsAuthInfo{},
		WireApi: WireApiResponses,
		HTTPHeaders: map[string]string{
			amazonBedrockMantleClientAgentHeader: amazonBedrockMantleClientAgentValue,
		},
		RequiresOpenAIAuth: false,
		SupportsWebsockets: false,
	}
	if !got.Equal(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestCreateOpenAIProviderHeaders(t *testing.T) {
	got := CreateOpenAIProvider(nil)
	if got.HTTPHeaders["version"] != CodexPkgVersion {
		t.Fatalf("version header: got %q", got.HTTPHeaders["version"])
	}
	if got.EnvHTTPHeaders["OpenAI-Organization"] != "OPENAI_ORGANIZATION" {
		t.Fatalf("org env header missing")
	}
	if got.EnvHTTPHeaders["OpenAI-Project"] != "OPENAI_PROJECT" {
		t.Fatalf("project env header missing")
	}
	if !got.RequiresOpenAIAuth || !got.SupportsWebsockets {
		t.Fatalf("openai provider should require auth and support websockets")
	}
}

func TestMergeAddsCustomProvider(t *testing.T) {
	custom := ModelProviderInfo{Name: "Custom", BaseURL: strptr("https://example.com/v1"), WireApi: WireApiResponses}
	configured := map[string]ModelProviderInfo{"custom": custom}

	merged, err := MergeConfiguredModelProviders(BuiltInModelProviders(nil), configured)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	got, ok := merged["custom"]
	if !ok || !got.Equal(custom) {
		t.Fatalf("custom provider not merged: %+v", got)
	}
	if len(merged) != 5 {
		t.Fatalf("expected 5 providers, got %d", len(merged))
	}
}

func TestMergeDoesNotMutateInput(t *testing.T) {
	builtIn := BuiltInModelProviders(nil)
	configured := map[string]ModelProviderInfo{
		"custom": {Name: "Custom", WireApi: WireApiResponses},
	}
	_, err := MergeConfiguredModelProviders(builtIn, configured)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, ok := builtIn["custom"]; ok {
		t.Fatalf("input map was mutated")
	}
}

func TestMergeAppliesBedrockProfileOverride(t *testing.T) {
	configured := map[string]ModelProviderInfo{
		AmazonBedrockProviderID: {
			Aws:     &ModelProviderAwsAuthInfo{Profile: strptr("codex-bedrock"), Region: strptr("us-west-2")},
			WireApi: WireApiResponses,
		},
	}
	merged, err := MergeConfiguredModelProviders(BuiltInModelProviders(nil), configured)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	got := merged[AmazonBedrockProviderID]
	want := ModelProviderAwsAuthInfo{Profile: strptr("codex-bedrock"), Region: strptr("us-west-2")}
	if got.Aws == nil || !got.Aws.Equal(want) {
		t.Fatalf("aws override not applied: %+v", got.Aws)
	}
}

func TestMergeBedrockPartialProfileOverride(t *testing.T) {
	// Only profile override; region should remain from built-in (nil).
	configured := map[string]ModelProviderInfo{
		AmazonBedrockProviderID: {
			Aws:     &ModelProviderAwsAuthInfo{Profile: strptr("only-profile")},
			WireApi: WireApiResponses,
		},
	}
	merged, err := MergeConfiguredModelProviders(BuiltInModelProviders(nil), configured)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	got := merged[AmazonBedrockProviderID].Aws
	if got == nil || got.Profile == nil || *got.Profile != "only-profile" {
		t.Fatalf("profile not applied: %+v", got)
	}
	if got.Region != nil {
		t.Fatalf("region should remain nil, got %v", *got.Region)
	}
}

func TestMergeRejectsBedrockNonDefaultFields(t *testing.T) {
	configured := map[string]ModelProviderInfo{
		AmazonBedrockProviderID: {
			Name:    "Custom Bedrock",
			Aws:     &ModelProviderAwsAuthInfo{Profile: strptr("codex-bedrock")},
			WireApi: WireApiResponses,
		},
	}
	_, err := MergeConfiguredModelProviders(BuiltInModelProviders(nil), configured)
	if err == nil {
		t.Fatalf("expected error for non-default bedrock fields")
	}
	want := "model_providers.amazon-bedrock only supports changing `aws.profile` and `aws.region`; " +
		"other non-default provider fields are not supported"
	if err.Error() != want {
		t.Fatalf("error: got %q want %q", err.Error(), want)
	}
}

func TestMergeAllowsBedrockDefaultFields(t *testing.T) {
	configured := map[string]ModelProviderInfo{
		AmazonBedrockProviderID: {
			Aws:     &ModelProviderAwsAuthInfo{},
			WireApi: WireApiResponses,
		},
	}
	merged, err := MergeConfiguredModelProviders(BuiltInModelProviders(nil), configured)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	want := BuiltInModelProviders(nil)
	if len(merged) != len(want) {
		t.Fatalf("size mismatch: got %d want %d", len(merged), len(want))
	}
	for id, w := range want {
		if got := merged[id]; !got.Equal(w) {
			t.Fatalf("provider %q changed:\n got %+v\nwant %+v", id, got, w)
		}
	}
}

func TestCreateOSSProviderEnvOverride(t *testing.T) {
	t.Setenv("CODEXGO_OSS_PORT", "")
	t.Setenv("CODEXGO_OSS_BASE_URL", "")
	p := CreateOSSProvider(DefaultOllamaPort, WireApiResponses)
	if p.BaseURL == nil || *p.BaseURL != "http://localhost:11434/v1" {
		t.Fatalf("default base url: %v", p.BaseURL)
	}
	if p.Name != "gpt-oss" {
		t.Fatalf("oss name: %q", p.Name)
	}

	t.Setenv("CODEXGO_OSS_PORT", "9999")
	p = CreateOSSProvider(DefaultOllamaPort, WireApiResponses)
	if p.BaseURL == nil || *p.BaseURL != "http://localhost:9999/v1" {
		t.Fatalf("port override base url: %v", p.BaseURL)
	}

	t.Setenv("CODEXGO_OSS_BASE_URL", "http://example.invalid/v1")
	p = CreateOSSProvider(DefaultOllamaPort, WireApiResponses)
	if p.BaseURL == nil || *p.BaseURL != "http://example.invalid/v1" {
		t.Fatalf("base url override: %v", p.BaseURL)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		p       ModelProviderInfo
		wantErr string
	}{
		{
			name: "aws conflicts with env_key and requires_openai_auth",
			p: func() ModelProviderInfo {
				p := CreateOpenAIProvider(nil)
				p.Aws = &ModelProviderAwsAuthInfo{}
				p.EnvKey = strptr("AWS_BEARER_TOKEN_BEDROCK")
				p.SupportsWebsockets = false
				return p
			}(),
			wantErr: "provider aws cannot be combined with env_key, requires_openai_auth",
		},
		{
			name: "aws conflicts with websockets",
			p: func() ModelProviderInfo {
				p := CreateOpenAIProvider(nil)
				p.Aws = &ModelProviderAwsAuthInfo{}
				p.RequiresOpenAIAuth = false
				p.SupportsWebsockets = true
				return p
			}(),
			wantErr: "provider aws cannot be combined with supports_websockets",
		},
		{
			name:    "aws with auth and bearer",
			p:       ModelProviderInfo{Aws: &ModelProviderAwsAuthInfo{}, Auth: &ModelProviderAuthInfo{Command: "x"}, ExperimentalBearerToken: strptr("t")},
			wantErr: "provider aws cannot be combined with experimental_bearer_token, auth",
		},
		{
			name:    "empty auth command",
			p:       ModelProviderInfo{Auth: &ModelProviderAuthInfo{Command: "  "}},
			wantErr: "provider auth.command must not be empty",
		},
		{
			name:    "auth conflicts",
			p:       ModelProviderInfo{Auth: &ModelProviderAuthInfo{Command: "x"}, EnvKey: strptr("K"), RequiresOpenAIAuth: true},
			wantErr: "provider auth cannot be combined with env_key, requires_openai_auth",
		},
		{
			name:    "valid auth only",
			p:       ModelProviderInfo{Auth: &ModelProviderAuthInfo{Command: "x"}},
			wantErr: "",
		},
		{
			name:    "valid empty",
			p:       ModelProviderInfo{},
			wantErr: "",
		},
		{
			name:    "valid aws alone",
			p:       ModelProviderInfo{Aws: &ModelProviderAwsAuthInfo{}},
			wantErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.p.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("got %v want %q", err, tt.wantErr)
			}
		})
	}
}

func TestWireApiStringAndMarshal(t *testing.T) {
	if WireApiResponses.String() != "responses" {
		t.Fatalf("string: %q", WireApiResponses.String())
	}
	data, err := WireApiResponses.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `"responses"` {
		t.Fatalf("marshal: %s", data)
	}
	var w WireApi
	if err := w.UnmarshalJSON([]byte(`"responses"`)); err != nil || w != WireApiResponses {
		t.Fatalf("unmarshal: %v %q", err, w)
	}
	// codexgo divergence: chat is supported again (DEVIATIONS.md "wire_api chat").
	if err := w.UnmarshalJSON([]byte(`"chat"`)); err != nil || w != WireApiChat {
		t.Fatalf("chat unmarshal: %v %q, want WireApiChat", err, w)
	}
	if got := WireApiChat.String(); got != "chat" {
		t.Fatalf("chat string: %q", got)
	}
}

func TestHasCommandAuth(t *testing.T) {
	if (ModelProviderInfo{}).HasCommandAuth() {
		t.Fatalf("empty provider should not have command auth")
	}
	if !(ModelProviderInfo{Auth: &ModelProviderAuthInfo{Command: "x"}}).HasCommandAuth() {
		t.Fatalf("provider with auth should have command auth")
	}
}
