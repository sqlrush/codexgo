package ossutil

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestGetDefaultModelForOSSProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		providerID string
		wantModel  string
		wantOK     bool
	}{
		{
			name:       "lmstudio",
			providerID: LMStudioOSSProviderID,
			wantModel:  LMStudioDefaultOSSModel,
			wantOK:     true,
		},
		{
			name:       "ollama",
			providerID: OllamaOSSProviderID,
			wantModel:  OllamaDefaultOSSModel,
			wantOK:     true,
		},
		{
			name:       "unknown provider",
			providerID: "unknown-provider",
			wantModel:  "",
			wantOK:     false,
		},
		{
			name:       "empty provider",
			providerID: "",
			wantModel:  "",
			wantOK:     false,
		},
		{
			name:       "case sensitive mismatch",
			providerID: "Ollama",
			wantModel:  "",
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotModel, gotOK := GetDefaultModelForOSSProvider(tt.providerID)
			if gotModel != tt.wantModel || gotOK != tt.wantOK {
				t.Fatalf("GetDefaultModelForOSSProvider(%q) = (%q, %v), want (%q, %v)",
					tt.providerID, gotModel, gotOK, tt.wantModel, tt.wantOK)
			}
		})
	}
}

func TestDefaultModelConstants(t *testing.T) {
	t.Parallel()

	// These values are externally observable and must match the Rust crate.
	if LMStudioDefaultOSSModel != "openai/gpt-oss-20b" {
		t.Errorf("LMStudioDefaultOSSModel = %q", LMStudioDefaultOSSModel)
	}
	if OllamaDefaultOSSModel != "gpt-oss:20b" {
		t.Errorf("OllamaDefaultOSSModel = %q", OllamaDefaultOSSModel)
	}
	if LMStudioOSSProviderID != "lmstudio" {
		t.Errorf("LMStudioOSSProviderID = %q", LMStudioOSSProviderID)
	}
	if OllamaOSSProviderID != "ollama" {
		t.Errorf("OllamaOSSProviderID = %q", OllamaOSSProviderID)
	}
}

// fakeProvisioner records calls and returns configured errors. It lets tests
// assert dispatch order and error wrapping without network access.
type fakeProvisioner struct {
	calls []string

	lmStudioErr  error
	responsesErr error
	ollamaErr    error
}

func (f *fakeProvisioner) EnsureLMStudioReady(context.Context) error {
	f.calls = append(f.calls, "lmstudio")
	return f.lmStudioErr
}

func (f *fakeProvisioner) EnsureOllamaResponsesSupported(context.Context) error {
	f.calls = append(f.calls, "responses")
	return f.responsesErr
}

func (f *fakeProvisioner) EnsureOllamaReady(context.Context) error {
	f.calls = append(f.calls, "ollama")
	return f.ollamaErr
}

func TestEnsureOSSProviderReady(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	tests := []struct {
		name       string
		providerID string
		prov       *fakeProvisioner
		wantCalls  []string
		wantErr    string // empty means no error
	}{
		{
			name:       "lmstudio success",
			providerID: LMStudioOSSProviderID,
			prov:       &fakeProvisioner{},
			wantCalls:  []string{"lmstudio"},
		},
		{
			name:       "lmstudio failure is wrapped",
			providerID: LMStudioOSSProviderID,
			prov:       &fakeProvisioner{lmStudioErr: boom},
			wantCalls:  []string{"lmstudio"},
			wantErr:    "OSS setup failed: boom",
		},
		{
			name:       "ollama success runs responses then ready",
			providerID: OllamaOSSProviderID,
			prov:       &fakeProvisioner{},
			wantCalls:  []string{"responses", "ollama"},
		},
		{
			name:       "ollama responses failure propagates unwrapped and short-circuits",
			providerID: OllamaOSSProviderID,
			prov:       &fakeProvisioner{responsesErr: boom},
			wantCalls:  []string{"responses"},
			wantErr:    "boom",
		},
		{
			name:       "ollama ready failure is wrapped",
			providerID: OllamaOSSProviderID,
			prov:       &fakeProvisioner{ollamaErr: boom},
			wantCalls:  []string{"responses", "ollama"},
			wantErr:    "OSS setup failed: boom",
		},
		{
			name:       "unknown provider skips setup",
			providerID: "unknown-provider",
			prov:       &fakeProvisioner{},
			wantCalls:  nil,
		},
		{
			name:       "empty provider skips setup",
			providerID: "",
			prov:       &fakeProvisioner{},
			wantCalls:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := EnsureOSSProviderReady(context.Background(), tt.providerID, tt.prov)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("EnsureOSSProviderReady returned error %v, want nil", err)
				}
			} else {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("EnsureOSSProviderReady error = %v, want %q", err, tt.wantErr)
				}
			}

			if !equalStrings(tt.prov.calls, tt.wantCalls) {
				t.Fatalf("calls = %v, want %v", tt.prov.calls, tt.wantCalls)
			}
		})
	}
}

func TestEnsureOSSProviderReadyNilProvisioner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		providerID string
		wantErr    bool
	}{
		{name: "lmstudio rejects nil", providerID: LMStudioOSSProviderID, wantErr: true},
		{name: "ollama rejects nil", providerID: OllamaOSSProviderID, wantErr: true},
		{name: "unknown ignores nil", providerID: "nope", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := EnsureOSSProviderReady(context.Background(), tt.providerID, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("EnsureOSSProviderReady(nil) error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSupportsResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version SemVer
		want    bool
	}{
		{name: "dev build 0.0.0 supported", version: SemVer{0, 0, 0}, want: true},
		{name: "below cutoff 0.13.3 unsupported", version: SemVer{0, 13, 3}, want: false},
		{name: "exact cutoff 0.13.4 supported", version: SemVer{0, 13, 4}, want: true},
		{name: "above cutoff 0.14.0 supported", version: SemVer{0, 14, 0}, want: true},
		{name: "much newer 1.0.0 supported", version: SemVer{1, 0, 0}, want: true},
		{name: "below cutoff 0.1.0 unsupported", version: SemVer{0, 1, 0}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := SupportsResponses(tt.version); got != tt.want {
				t.Fatalf("SupportsResponses(%s) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestMinResponsesVersion(t *testing.T) {
	t.Parallel()
	got := MinResponsesVersion()
	want := SemVer{Major: 0, Minor: 13, Patch: 4}
	if got != want {
		t.Fatalf("MinResponsesVersion() = %v, want %v", got, want)
	}
	if got.String() != "0.13.4" {
		t.Fatalf("MinResponsesVersion().String() = %q, want %q", got.String(), "0.13.4")
	}
}

func TestParseSemVer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    SemVer
		wantErr bool
	}{
		{name: "plain", input: "0.13.4", want: SemVer{0, 13, 4}},
		{name: "leading v", input: "v1.2.3", want: SemVer{1, 2, 3}},
		{name: "surrounding whitespace", input: "  0.14.0  ", want: SemVer{0, 14, 0}},
		{name: "pre-release dropped", input: "0.14.0-rc1", want: SemVer{0, 14, 0}},
		{name: "build metadata dropped", input: "0.14.0+build5", want: SemVer{0, 14, 0}},
		{name: "v with pre-release", input: "v0.13.4-beta", want: SemVer{0, 13, 4}},
		{name: "missing patch", input: "1.2", wantErr: true},
		{name: "too many parts", input: "1.2.3.4", wantErr: true},
		{name: "non-numeric", input: "1.a.3", wantErr: true},
		{name: "empty", input: "", wantErr: true},
		{name: "empty component", input: "1..3", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseSemVer(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseSemVer(%q) = %v, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSemVer(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("ParseSemVer(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestParseSemVerRoundTripsSupportsResponses verifies the fetch_version-style
// normalization integrates with the support check the way the Rust crate uses
// it end to end.
func TestParseSemVerRoundTripsSupportsResponses(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"v0.13.4", "0.13.4", " 0.14.1 "} {
		v, err := ParseSemVer(in)
		if err != nil {
			t.Fatalf("ParseSemVer(%q): %v", in, err)
		}
		if !SupportsResponses(v) {
			t.Fatalf("SupportsResponses after ParseSemVer(%q) = false, want true", in)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Guard against accidental error-message drift in the wrapping format.
func TestOSSSetupFailedFormat(t *testing.T) {
	t.Parallel()
	if !strings.HasPrefix(ossSetupFailedFormat, "OSS setup failed: ") {
		t.Fatalf("ossSetupFailedFormat = %q", ossSetupFailedFormat)
	}
}
