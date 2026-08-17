package config

import (
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/modelproviderinfo"
)

func TestValidateModelProvidersReservedIDs(t *testing.T) {
	providers := map[string]modelproviderinfo.ModelProviderInfo{
		"openai": {Name: "Custom"},
		"ollama": {Name: "Custom"},
	}
	err := ValidateModelProviders(providers)
	if err == nil || !strings.Contains(err.Error(), "reserved built-in provider IDs") {
		t.Fatalf("want reserved error, got %v", err)
	}
	if !strings.Contains(err.Error(), "`ollama`") || !strings.Contains(err.Error(), "`openai`") {
		t.Fatalf("conflicts should be sorted and listed: %v", err)
	}
}

func TestValidateModelProvidersEmptyName(t *testing.T) {
	providers := map[string]modelproviderinfo.ModelProviderInfo{
		"custom": {Name: "   "},
	}
	err := ValidateModelProviders(providers)
	if err == nil || !strings.Contains(err.Error(), "provider name must not be empty") {
		t.Fatalf("want empty-name error, got %v", err)
	}
}

func TestValidateModelProvidersAwsOnlyBedrock(t *testing.T) {
	providers := map[string]modelproviderinfo.ModelProviderInfo{
		"custom": {Name: "Custom", Aws: &modelproviderinfo.ModelProviderAwsAuthInfo{}},
	}
	err := ValidateModelProviders(providers)
	if err == nil || !strings.Contains(err.Error(), "provider aws is only supported for `amazon-bedrock`") {
		t.Fatalf("want aws error, got %v", err)
	}
}

func TestValidateModelProvidersValid(t *testing.T) {
	providers := map[string]modelproviderinfo.ModelProviderInfo{
		"custom":         {Name: "Custom"},
		"amazon-bedrock": {Name: "Bedrock"},
	}
	if err := ValidateModelProviders(providers); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateOssProvider(t *testing.T) {
	tests := []struct {
		provider string
		wantErr  string
	}{
		{provider: "lmstudio"},
		{provider: "ollama"},
		{provider: "ollama-chat", wantErr: "no longer supported"},
		{provider: "bogus", wantErr: "Invalid OSS provider"},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			err := ValidateOssProvider(tt.provider)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want %q, got %v", tt.wantErr, err)
			}
		})
	}
}
