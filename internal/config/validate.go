package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/modelproviderinfo"
)

// reservedModelProviderIDs are built-in provider IDs that cannot be overridden
// by user config (amazon-bedrock is special-cased and may be customized).
var reservedModelProviderIDs = []string{
	modelproviderinfo.AmazonBedrockProviderID,
	modelproviderinfo.OpenAIProviderID,
	modelproviderinfo.OllamaOSSProviderID,
	modelproviderinfo.LMStudioOSSProviderID,
}

// ValidateReservedModelProviderIDs returns an error if any user-defined provider
// ID conflicts with a reserved built-in ID (excluding amazon-bedrock).
func ValidateReservedModelProviderIDs(providers map[string]modelproviderinfo.ModelProviderInfo) error {
	var conflicts []string
	for key := range providers {
		if key == modelproviderinfo.AmazonBedrockProviderID {
			continue
		}
		if containsString(reservedModelProviderIDs, key) {
			conflicts = append(conflicts, fmt.Sprintf("`%s`", key))
		}
	}
	if len(conflicts) == 0 {
		return nil
	}
	sort.Strings(conflicts)
	return fmt.Errorf(
		"model_providers contains reserved built-in provider IDs: %s. "+
			"Built-in providers cannot be overridden. Rename your custom provider (for example, `openai-custom`).",
		strings.Join(conflicts, ", "),
	)
}

// ValidateModelProviders validates all user-defined model providers, mirroring
// the Rust validate_model_providers function.
func ValidateModelProviders(providers map[string]modelproviderinfo.ModelProviderInfo) error {
	if err := ValidateReservedModelProviderIDs(providers); err != nil {
		return err
	}
	for _, key := range sortedProviderKeys(providers) {
		provider := providers[key]
		if key == modelproviderinfo.AmazonBedrockProviderID {
			continue
		}
		if provider.Aws != nil {
			return fmt.Errorf(
				"model_providers.%s: provider aws is only supported for `%s`",
				key, modelproviderinfo.AmazonBedrockProviderID,
			)
		}
		if strings.TrimSpace(provider.Name) == "" {
			return fmt.Errorf("model_providers.%s: provider name must not be empty", key)
		}
		if err := provider.Validate(); err != nil {
			return fmt.Errorf("model_providers.%s: %w", key, err)
		}
	}
	return nil
}

// ValidateOssProvider verifies the OSS provider name is one of the supported
// values, surfacing the removed-ollama-chat error where applicable.
func ValidateOssProvider(provider string) error {
	switch provider {
	case modelproviderinfo.LMStudioOSSProviderID, modelproviderinfo.OllamaOSSProviderID:
		return nil
	case modelproviderinfo.LegacyOllamaChatProviderID:
		return fmt.Errorf("%s", modelproviderinfo.OllamaChatProviderRemovedError)
	default:
		return fmt.Errorf(
			"Invalid OSS provider '%s'. Must be one of: %s, %s",
			provider, modelproviderinfo.LMStudioOSSProviderID, modelproviderinfo.OllamaOSSProviderID,
		)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func sortedProviderKeys(m map[string]modelproviderinfo.ModelProviderInfo) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
