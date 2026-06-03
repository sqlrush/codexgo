package modelproviderinfo

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/protocol"
)

func strptr(s string) *string { return &s }
func u64ptr(v uint64) *uint64 { return &v }

// TestDeserializeProviders covers the TOML-shaped deserialization cases from the
// Rust tests, expressed as JSON (serde field names are identical across formats).
func TestDeserializeProviders(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want ModelProviderInfo
	}{
		{
			name: "ollama",
			in:   `{"name":"Ollama","base_url":"http://localhost:11434/v1"}`,
			want: ModelProviderInfo{
				Name:    "Ollama",
				BaseURL: strptr("http://localhost:11434/v1"),
				WireApi: WireApiResponses,
			},
		},
		{
			name: "azure",
			in: `{"name":"Azure","base_url":"https://xxxxx.openai.azure.com/openai",` +
				`"env_key":"AZURE_OPENAI_API_KEY",` +
				`"query_params":{"api-version":"2025-04-01-preview"}}`,
			want: ModelProviderInfo{
				Name:        "Azure",
				BaseURL:     strptr("https://xxxxx.openai.azure.com/openai"),
				EnvKey:      strptr("AZURE_OPENAI_API_KEY"),
				WireApi:     WireApiResponses,
				QueryParams: map[string]string{"api-version": "2025-04-01-preview"},
			},
		},
		{
			name: "example with headers",
			in: `{"name":"Example","base_url":"https://example.com","env_key":"API_KEY",` +
				`"http_headers":{"X-Example-Header":"example-value"},` +
				`"env_http_headers":{"X-Example-Env-Header":"EXAMPLE_ENV_VAR"}}`,
			want: ModelProviderInfo{
				Name:           "Example",
				BaseURL:        strptr("https://example.com"),
				EnvKey:         strptr("API_KEY"),
				WireApi:        WireApiResponses,
				HTTPHeaders:    map[string]string{"X-Example-Header": "example-value"},
				EnvHTTPHeaders: map[string]string{"X-Example-Env-Header": "EXAMPLE_ENV_VAR"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got ModelProviderInfo
			if err := json.Unmarshal([]byte(tt.in), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("got %+v want %+v", got, tt.want)
			}
		})
	}
}

func TestDeserializeChatWireAPIError(t *testing.T) {
	in := `{"name":"OpenAI using Chat Completions","wire_api":"chat"}`
	err := json.Unmarshal([]byte(in), new(ModelProviderInfo))
	if err == nil {
		t.Fatalf("expected error for wire_api=chat")
	}
	if !strings.Contains(err.Error(), chatWireAPIRemovedError) {
		t.Fatalf("error %q does not contain removal message", err.Error())
	}
}

func TestDeserializeUnknownWireAPI(t *testing.T) {
	in := `{"name":"X","wire_api":"grpc"}`
	err := json.Unmarshal([]byte(in), new(ModelProviderInfo))
	if err == nil || !strings.Contains(err.Error(), "unknown variant `grpc`") {
		t.Fatalf("expected unknown variant error, got %v", err)
	}
}

func TestDeserializeWebsocketConnectTimeout(t *testing.T) {
	in := `{"name":"OpenAI","base_url":"https://api.openai.com/v1",` +
		`"websocket_connect_timeout_ms":15000,"supports_websockets":true}`
	var got ModelProviderInfo
	if err := json.Unmarshal([]byte(in), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.WebsocketConnectTimeoutMS == nil || *got.WebsocketConnectTimeoutMS != 15000 {
		t.Fatalf("got %v want 15000", got.WebsocketConnectTimeoutMS)
	}
}

func TestDeserializeProviderAuthDefaults(t *testing.T) {
	in := `{"name":"Corp","auth":{"command":"./scripts/print-token","args":["--format=text"]}}`
	var got ModelProviderInfo
	if err := json.Unmarshal([]byte(in), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Auth == nil {
		t.Fatalf("expected auth")
	}
	want := ModelProviderAuthInfo{
		Command:           "./scripts/print-token",
		Args:              []string{"--format=text"},
		TimeoutMS:         5000,
		RefreshIntervalMS: 300_000,
		Cwd:               protocol.AbsolutePath("."),
	}
	if !got.Auth.Equal(want) {
		t.Fatalf("got %+v want %+v", *got.Auth, want)
	}
	if got.Auth.Timeout() != 5*time.Second {
		t.Fatalf("timeout: got %v", got.Auth.Timeout())
	}
}

func TestDeserializeProviderAuthZeroRefreshInterval(t *testing.T) {
	in := `{"name":"Corp","auth":{"command":"./scripts/print-token","refresh_interval_ms":0}}`
	var got ModelProviderInfo
	if err := json.Unmarshal([]byte(in), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Auth.RefreshIntervalMS != 0 {
		t.Fatalf("refresh_interval_ms: got %d", got.Auth.RefreshIntervalMS)
	}
	if d, ok := got.Auth.RefreshInterval(); ok {
		t.Fatalf("refresh interval should be disabled, got %v", d)
	}
}

func TestDeserializeProviderAuthZeroTimeoutRejected(t *testing.T) {
	in := `{"name":"Corp","auth":{"command":"x","timeout_ms":0}}`
	err := json.Unmarshal([]byte(in), new(ModelProviderInfo))
	if err == nil || !strings.Contains(err.Error(), "non-zero") {
		t.Fatalf("expected non-zero timeout error, got %v", err)
	}
}

func TestDeserializeProviderAWSConfig(t *testing.T) {
	in := `{"name":"Amazon Bedrock","base_url":"https://bedrock.example.com/v1",` +
		`"aws":{"profile":"codex-bedrock","region":"us-west-2"}}`
	var got ModelProviderInfo
	if err := json.Unmarshal([]byte(in), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := ModelProviderAwsAuthInfo{Profile: strptr("codex-bedrock"), Region: strptr("us-west-2")}
	if got.Aws == nil || !got.Aws.Equal(want) {
		t.Fatalf("got %+v want %+v", got.Aws, want)
	}
}

// TestMarshalProviderFieldOrderAndNulls verifies the serialized JSON includes all
// fields in declaration order with null for absent optionals, matching serde.
func TestMarshalProviderFieldOrderAndNulls(t *testing.T) {
	p := ModelProviderInfo{Name: "Ollama", BaseURL: strptr("http://localhost:11434/v1")}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"name":"Ollama","base_url":"http://localhost:11434/v1","env_key":null,` +
		`"env_key_instructions":null,"experimental_bearer_token":null,"auth":null,"aws":null,` +
		`"wire_api":"responses","query_params":null,"http_headers":null,"env_http_headers":null,` +
		`"request_max_retries":null,"stream_max_retries":null,"stream_idle_timeout_ms":null,` +
		`"websocket_connect_timeout_ms":null,"requires_openai_auth":false,"supports_websockets":false}`
	if string(data) != want {
		t.Fatalf("marshal mismatch:\n got %s\nwant %s", data, want)
	}
}

func TestProviderRoundTrip(t *testing.T) {
	cases := []ModelProviderInfo{
		CreateOpenAIProvider(nil),
		CreateAmazonBedrockProvider(nil),
		CreateOSSProviderWithBaseURL("http://localhost:11434/v1", WireApiResponses),
		{
			Name:                      "Custom",
			BaseURL:                   strptr("https://x"),
			EnvKey:                    strptr("K"),
			RequestMaxRetries:         u64ptr(2),
			StreamMaxRetries:          u64ptr(3),
			StreamIdleTimeoutMS:       u64ptr(1000),
			WebsocketConnectTimeoutMS: u64ptr(2000),
			WireApi:                   WireApiResponses,
			RequiresOpenAIAuth:        true,
		},
	}
	for i, c := range cases {
		data, err := json.Marshal(c)
		if err != nil {
			t.Fatalf("case %d marshal: %v", i, err)
		}
		var back ModelProviderInfo
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("case %d unmarshal: %v", i, err)
		}
		if !c.Equal(back) {
			t.Fatalf("case %d round-trip mismatch:\n in  %+v\n out %+v", i, c, back)
		}
	}
}

func TestAuthInfoMarshalOmitsDefaultCwd(t *testing.T) {
	a := DefaultModelProviderAuthInfo()
	a.Command = "x"
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "cwd") {
		t.Fatalf("default cwd should be omitted: %s", data)
	}
	want := `{"command":"x","args":[],"timeout_ms":5000,"refresh_interval_ms":300000}`
	if string(data) != want {
		t.Fatalf("got %s want %s", data, want)
	}

	a.Cwd = protocol.AbsolutePath("/abs/path")
	data, err = json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"cwd":"/abs/path"`) {
		t.Fatalf("non-default cwd should serialize: %s", data)
	}
}

func TestSupportsRemoteCompaction(t *testing.T) {
	tests := []struct {
		name string
		p    ModelProviderInfo
		want bool
	}{
		{"openai", CreateOpenAIProvider(nil), true},
		{"azure name", ModelProviderInfo{Name: "Azure", BaseURL: strptr("https://example.com/openai")}, true},
		{"other", ModelProviderInfo{Name: "Example", BaseURL: strptr("https://example.com/v1")}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.SupportsRemoteCompaction(); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestAPIKey(t *testing.T) {
	const envName = "MODELPROVIDERINFO_TEST_KEY"
	t.Setenv(envName, "secret")

	p := ModelProviderInfo{EnvKey: strptr(envName)}
	got, err := p.APIKey()
	if err != nil {
		t.Fatalf("api key: %v", err)
	}
	if got == nil || *got != "secret" {
		t.Fatalf("got %v want secret", got)
	}

	// nil env_key returns (nil, nil).
	none := ModelProviderInfo{}
	got, err = none.APIKey()
	if err != nil || got != nil {
		t.Fatalf("got %v %v want nil nil", got, err)
	}

	// Missing env var returns EnvVarError.
	os.Unsetenv(envName)
	missing := ModelProviderInfo{EnvKey: strptr(envName), EnvKeyInstructions: strptr("set it")}
	_, err = missing.APIKey()
	var envErr *EnvVarError
	if err == nil {
		t.Fatalf("expected error")
	}
	if e, ok := err.(*EnvVarError); ok {
		envErr = e
	} else {
		t.Fatalf("expected *EnvVarError, got %T", err)
	}
	if envErr.Var != envName {
		t.Fatalf("var: got %s", envErr.Var)
	}
	wantMsg := "Missing environment variable: `" + envName + "`. set it"
	if envErr.Error() != wantMsg {
		t.Fatalf("error msg: got %q want %q", envErr.Error(), wantMsg)
	}
}

func TestEffectiveRetriesAndTimeouts(t *testing.T) {
	p := ModelProviderInfo{}
	if got := p.EffectiveRequestMaxRetries(); got != 4 {
		t.Fatalf("default request retries: got %d", got)
	}
	if got := p.EffectiveStreamMaxRetries(); got != 5 {
		t.Fatalf("default stream retries: got %d", got)
	}
	if got := p.StreamIdleTimeout(); got != 300*time.Second {
		t.Fatalf("default idle timeout: got %v", got)
	}
	if got := p.WebsocketConnectTimeout(); got != 15*time.Second {
		t.Fatalf("default ws timeout: got %v", got)
	}

	capped := ModelProviderInfo{RequestMaxRetries: u64ptr(1000), StreamMaxRetries: u64ptr(1000)}
	if got := capped.EffectiveRequestMaxRetries(); got != 100 {
		t.Fatalf("request cap: got %d", got)
	}
	if got := capped.EffectiveStreamMaxRetries(); got != 100 {
		t.Fatalf("stream cap: got %d", got)
	}
}

func TestToAPIProviderBaseURL(t *testing.T) {
	p := CreateOpenAIProvider(nil)

	// No auth mode -> public OpenAI base URL.
	prov := p.ToAPIProvider(nil)
	if prov.BaseURL != openaiDefaultBaseURL {
		t.Fatalf("default base url: got %s", prov.BaseURL)
	}
	if prov.Retry.MaxAttempts != 4 || prov.Retry.Retry429 || !prov.Retry.Retry5xx || !prov.Retry.RetryTransport {
		t.Fatalf("retry config mismatch: %+v", prov.Retry)
	}
	if prov.StreamIdleTimeout != 300*time.Second {
		t.Fatalf("stream idle timeout: got %v", prov.StreamIdleTimeout)
	}

	// ChatGPT auth mode -> codex backend base URL.
	chatgpt := appserverproto.AuthModeChatgpt
	prov = p.ToAPIProvider(&chatgpt)
	if prov.BaseURL != ChatGPTCodexBaseURL {
		t.Fatalf("chatgpt base url: got %s", prov.BaseURL)
	}

	// API key auth mode -> public OpenAI base URL.
	apikey := appserverproto.AuthModeApiKey
	prov = p.ToAPIProvider(&apikey)
	if prov.BaseURL != openaiDefaultBaseURL {
		t.Fatalf("apikey base url: got %s", prov.BaseURL)
	}
}

func TestAmazonBedrockMantleHeader(t *testing.T) {
	prov := CreateAmazonBedrockProvider(nil).ToAPIProvider(nil)
	if got := prov.Headers.Get(amazonBedrockMantleClientAgentHeader); got != amazonBedrockMantleClientAgentValue {
		t.Fatalf("mantle header: got %q want %q", got, amazonBedrockMantleClientAgentValue)
	}
}

func TestBuildHeaderMapSkipsInvalidAndEmptyEnv(t *testing.T) {
	t.Setenv("MPI_PRESENT", "yes")
	os.Unsetenv("MPI_ABSENT")
	t.Setenv("MPI_BLANK", "   ")

	p := ModelProviderInfo{
		HTTPHeaders: map[string]string{
			"Valid-Header": "ok",
			"Bad Header":   "ignored", // space in name -> invalid
			"X-CRLF":       "bad\r\nvalue",
		},
		EnvHTTPHeaders: map[string]string{
			"X-Present": "MPI_PRESENT",
			"X-Absent":  "MPI_ABSENT",
			"X-Blank":   "MPI_BLANK",
		},
	}
	h := p.buildHeaderMap()
	if h.Get("Valid-Header") != "ok" {
		t.Fatalf("valid header missing")
	}
	if h.Get("Bad Header") != "" {
		t.Fatalf("invalid name should be skipped")
	}
	if h.Get("X-CRLF") != "" {
		t.Fatalf("invalid value should be skipped")
	}
	if h.Get("X-Present") != "yes" {
		t.Fatalf("present env header missing")
	}
	if h.Get("X-Absent") != "" {
		t.Fatalf("absent env header should be skipped")
	}
	if h.Get("X-Blank") != "" {
		t.Fatalf("blank env header should be skipped")
	}
}
