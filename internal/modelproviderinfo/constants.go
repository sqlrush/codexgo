// Package modelproviderinfo is a faithful, drop-in-compatible Go port of the
// Rust crate codex-model-provider-info (codex 0.136.0).
//
// It defines the registry of model providers supported by Codex: the
// ModelProviderInfo type and the built-in defaults (OpenAI, Amazon Bedrock,
// Ollama, LM Studio) that ship compiled into the binary so Codex works
// out-of-the-box. User-defined entries from config.toml can override or extend
// the defaults at runtime via MergeConfiguredModelProviders.
//
// JSON/TOML on-disk formats match the Rust serde output byte-for-byte after
// key-order canonicalization, so configuration files are shared with real codex.
package modelproviderinfo

import "time"

// Default retry/timeout values used when a provider does not override them.
const (
	defaultStreamIdleTimeoutMS = uint64(300_000)
	defaultStreamMaxRetries    = uint64(5)
	defaultRequestMaxRetries   = uint64(4)

	// DefaultWebsocketConnectTimeoutMS is the default time to wait for a
	// websocket connection attempt before treating it as failed.
	DefaultWebsocketConnectTimeoutMS = uint64(15_000)

	// maxStreamMaxRetries is the hard cap for user-configured stream_max_retries.
	maxStreamMaxRetries = uint64(100)
	// maxRequestMaxRetries is the hard cap for user-configured request_max_retries.
	maxRequestMaxRetries = uint64(100)
)

// retryBaseDelay is the fixed base delay applied to request retries, mirroring
// the Rust `Duration::from_millis(200)` used when building the API provider.
const retryBaseDelay = 200 * time.Millisecond

// CodexPkgVersion mirrors the Rust `env!("CARGO_PKG_VERSION")` value that is
// sent as the OpenAI provider's `version` HTTP header. It is pinned to the codex
// release this port targets (0.136.0).
const CodexPkgVersion = "0.136.0"

// Provider display names and identifiers.
const (
	openaiProviderName = "OpenAI"
	// OpenAIProviderID is the registry key for the built-in OpenAI provider.
	OpenAIProviderID = "openai"
	// ChatGPTCodexBaseURL is the base URL used when authenticating via ChatGPT.
	ChatGPTCodexBaseURL = "https://chatgpt.com/backend-api/codex"

	amazonBedrockProviderName = "Amazon Bedrock"
	// AmazonBedrockProviderID is the registry key for the built-in Amazon
	// Bedrock provider.
	AmazonBedrockProviderID = "amazon-bedrock"
	// AmazonBedrockGPT55ModelID is the Bedrock model id for openai.gpt-5.5.
	AmazonBedrockGPT55ModelID = "openai.gpt-5.5"
	// AmazonBedrockGPT54ModelID is the Bedrock model id for openai.gpt-5.4.
	AmazonBedrockGPT54ModelID = "openai.gpt-5.4"
	// AmazonBedrockDefaultBaseURL is the default Bedrock OpenAI-compatible base URL.
	AmazonBedrockDefaultBaseURL = "https://bedrock-mantle.us-east-1.api.aws/openai/v1"

	amazonBedrockMantleClientAgentHeader = "x-amzn-mantle-client-agent"
	amazonBedrockMantleClientAgentValue  = "codex"
)

// openaiDefaultBaseURL is the default base URL for the OpenAI provider when no
// ChatGPT-style auth mode is in use.
const openaiDefaultBaseURL = "https://api.openai.com/v1"

// Error and removal messages surfaced to users.
const (
	// chatWireAPIRemovedError is returned when a config sets wire_api = "chat".
	chatWireAPIRemovedError = "`wire_api = \"chat\"` is no longer supported.\n" +
		"How to fix: set `wire_api = \"responses\"` in your provider config.\n" +
		"More info: https://github.com/openai/codex/discussions/7782"

	// LegacyOllamaChatProviderID is the removed legacy ollama-chat provider id.
	LegacyOllamaChatProviderID = "ollama-chat"
	// OllamaChatProviderRemovedError is shown when ollama-chat is referenced.
	OllamaChatProviderRemovedError = "`ollama-chat` is no longer supported.\n" +
		"How to fix: replace `ollama-chat` with `ollama` in `model_provider`, " +
		"`oss_provider`, or `--local-provider`.\n" +
		"More info: https://github.com/openai/codex/discussions/7782"
)

// Default OSS provider ports and identifiers.
const (
	// DefaultLMStudioPort is the default LM Studio server port.
	DefaultLMStudioPort = uint16(1234)
	// DefaultOllamaPort is the default Ollama server port.
	DefaultOllamaPort = uint16(11434)

	// LMStudioOSSProviderID is the registry key for the built-in LM Studio provider.
	LMStudioOSSProviderID = "lmstudio"
	// OllamaOSSProviderID is the registry key for the built-in Ollama provider.
	OllamaOSSProviderID = "ollama"
)
