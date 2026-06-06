package modelproviderinfo

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// CreateOpenAIProvider builds the built-in OpenAI provider. The optional
// baseURL overrides the default OpenAI base URL. It mirrors
// create_openai_provider: requires_openai_auth and supports_websockets are true,
// the CARGO_PKG_VERSION "version" header is attached, and the
// OpenAI-Organization / OpenAI-Project env headers are wired up.
func CreateOpenAIProvider(baseURL *string) ModelProviderInfo {
	return ModelProviderInfo{
		Name:    openaiProviderName,
		BaseURL: cloneStringPtr(baseURL),
		WireApi: WireApiResponses,
		HTTPHeaders: map[string]string{
			"version": CodexPkgVersion,
		},
		EnvHTTPHeaders: map[string]string{
			"OpenAI-Organization": "OPENAI_ORGANIZATION",
			"OpenAI-Project":      "OPENAI_PROJECT",
		},
		RequiresOpenAIAuth: true,
		SupportsWebsockets: true,
	}
}

// CreateAmazonBedrockProvider builds the built-in Amazon Bedrock provider. The
// optional aws overrides the default (empty) AWS auth info. It mirrors
// create_amazon_bedrock_provider: the Bedrock base URL and the mantle
// client-agent header are set, and supports_websockets is false.
func CreateAmazonBedrockProvider(aws *ModelProviderAwsAuthInfo) ModelProviderInfo {
	awsInfo := ModelProviderAwsAuthInfo{}
	if aws != nil {
		awsInfo = ModelProviderAwsAuthInfo{
			Profile: cloneStringPtr(aws.Profile),
			Region:  cloneStringPtr(aws.Region),
		}
	}
	baseURL := AmazonBedrockDefaultBaseURL
	return ModelProviderInfo{
		Name:    amazonBedrockProviderName,
		BaseURL: &baseURL,
		Aws:     &awsInfo,
		WireApi: WireApiResponses,
		HTTPHeaders: map[string]string{
			amazonBedrockMantleClientAgentHeader: amazonBedrockMantleClientAgentValue,
		},
		RequiresOpenAIAuth: false,
		SupportsWebsockets: false,
	}
}

// BuiltInModelProviders returns the built-in default provider catalog keyed by
// provider id: openai, amazon-bedrock, ollama, and lmstudio. The optional
// openaiBaseURL overrides the OpenAI provider's base URL. It mirrors
// built_in_model_providers.
func BuiltInModelProviders(openaiBaseURL *string) map[string]ModelProviderInfo {
	return map[string]ModelProviderInfo{
		OpenAIProviderID:        CreateOpenAIProvider(openaiBaseURL),
		AmazonBedrockProviderID: CreateAmazonBedrockProvider(nil),
		OllamaOSSProviderID:     CreateOSSProvider(DefaultOllamaPort, WireApiResponses),
		LMStudioOSSProviderID:   CreateOSSProvider(DefaultLMStudioPort, WireApiResponses),
	}
}

// MergeConfiguredModelProviders merges configured providers into the built-in
// catalog. Configured providers extend the built-in set, but built-in providers
// are not overridable except that the built-in Amazon Bedrock provider accepts
// aws.profile and aws.region overrides. It mirrors
// merge_configured_model_providers and returns a new map (the input maps are not
// mutated).
//
// An error (matching the Rust String error) is returned when the configured
// amazon-bedrock entry sets any non-default field other than aws.
func MergeConfiguredModelProviders(
	modelProviders map[string]ModelProviderInfo,
	configuredModelProviders map[string]ModelProviderInfo,
) (map[string]ModelProviderInfo, error) {
	out := make(map[string]ModelProviderInfo, len(modelProviders))
	for k, v := range modelProviders {
		out[k] = v
	}

	for key, provider := range configuredModelProviders {
		if key == AmazonBedrockProviderID {
			awsOverride := provider.Aws
			provider.Aws = nil
			if !provider.Equal(DefaultModelProviderInfo()) {
				return nil, fmt.Errorf(
					"model_providers.%s only supports changing `aws.profile` and `aws.region`; "+
						"other non-default provider fields are not supported",
					AmazonBedrockProviderID,
				)
			}

			if awsOverride != nil {
				if builtIn, ok := out[AmazonBedrockProviderID]; ok && builtIn.Aws != nil {
					updated := mergeAWSOverride(*builtIn.Aws, *awsOverride)
					builtIn.Aws = &updated
					out[AmazonBedrockProviderID] = builtIn
				}
			}
			continue
		}

		if _, exists := out[key]; !exists {
			out[key] = provider
		}
	}

	return out, nil
}

// mergeAWSOverride returns a copy of base with any non-nil fields from override
// applied, mirroring the Rust per-field overwrite for aws.profile / aws.region.
func mergeAWSOverride(base, override ModelProviderAwsAuthInfo) ModelProviderAwsAuthInfo {
	out := ModelProviderAwsAuthInfo{
		Profile: cloneStringPtr(base.Profile),
		Region:  cloneStringPtr(base.Region),
	}
	if override.Profile != nil {
		out.Profile = cloneStringPtr(override.Profile)
	}
	if override.Region != nil {
		out.Region = cloneStringPtr(override.Region)
	}
	return out
}

// CreateOSSProvider builds an open-source ("gpt-oss") provider for the given
// default port and wire API. The base URL honors the experimental CODEXGO_OSS_PORT
// and CODEXGO_OSS_BASE_URL environment variables. It mirrors create_oss_provider.
func CreateOSSProvider(defaultProviderPort uint16, wireApi WireApi) ModelProviderInfo {
	port := defaultProviderPort
	if raw, ok := os.LookupEnv("CODEXGO_OSS_PORT"); ok && strings.TrimSpace(raw) != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 16); err == nil {
			port = uint16(parsed)
		}
	}
	defaultBaseURL := fmt.Sprintf("http://localhost:%d/v1", port)

	baseURL := defaultBaseURL
	if raw, ok := os.LookupEnv("CODEXGO_OSS_BASE_URL"); ok && strings.TrimSpace(raw) != "" {
		baseURL = raw
	}
	return CreateOSSProviderWithBaseURL(baseURL, wireApi)
}

// CreateOSSProviderWithBaseURL builds an open-source ("gpt-oss") provider with an
// explicit base URL. It mirrors create_oss_provider_with_base_url.
func CreateOSSProviderWithBaseURL(baseURL string, wireApi WireApi) ModelProviderInfo {
	url := baseURL
	return ModelProviderInfo{
		Name:               "gpt-oss",
		BaseURL:            &url,
		WireApi:            wireApi,
		RequiresOpenAIAuth: false,
		SupportsWebsockets: false,
	}
}
