package core

import (
	"encoding/json"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/modelsmanager"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// buildResponsesRequest assembles a Responses API request body from a [Prompt]
// and the session-scoped config. It is the port of the Rust
// `ModelClient::build_responses_request`.
//
// The Prompt carries the conversation input, the resolved tool set, and the
// base/developer instruction override; the per-model behaviour (reasoning,
// verbosity, service tier) comes from the configured [modelsmanager.ModelInfo].
func (c *ResponsesModelClient) buildResponsesRequest(prompt Prompt) (api.ResponsesApiRequest, error) {
	info := c.cfg.ModelInfo

	instructions := c.instructionsFor(prompt)

	toolsJSON, err := tools.CreateToolsJSONForResponsesAPI(prompt.Tools)
	if err != nil {
		return api.ResponsesApiRequest{}, err
	}

	reasoning := buildReasoning(info, c.cfg.ReasoningEffort, c.cfg.ReasoningSummary)
	var include []string
	if reasoning != nil {
		include = []string{includeReasoningEncrypted}
	}

	verbosity := c.verbosityFor(info)
	text := buildTextControls(verbosity, prompt.OutputSchema, c.cfg.OutputSchemaStrict)

	cacheKey := c.promptCacheKey()
	serviceTier := info.ServiceTierForRequest(c.cfg.ServiceTier)

	installationID := c.cfg.InstallationID
	clientMetadata := map[string]string{
		clientMetadataInstallationID: installationID,
	}

	return api.ResponsesApiRequest{
		Model:             info.Slug,
		Instructions:      instructions,
		Input:             append([]protocol.ResponseItem(nil), prompt.Input...),
		Tools:             toolsJSON,
		ToolChoice:        "auto",
		ParallelToolCalls: c.cfg.ParallelToolCalls,
		Reasoning:         reasoning,
		Store:             c.cfg.Provider.IsAzureResponsesEndpoint(),
		Stream:            true,
		Include:           include,
		ServiceTier:       serviceTier,
		PromptCacheKey:    &cacheKey,
		Text:              text,
		ClientMetadata:    clientMetadata,
	}, nil
}

// instructionsFor returns the effective base instructions for the request,
// honoring a Prompt-level override. Mirrors the Rust use of
// `prompt.base_instructions.text`.
func (c *ResponsesModelClient) instructionsFor(prompt Prompt) string {
	if prompt.BaseInstructionsOverride != nil {
		return *prompt.BaseInstructionsOverride
	}
	return c.cfg.ModelInfo.BaseInstructions
}

// verbosityFor resolves the request verbosity from the model capability and the
// session override, mirroring the verbosity branch of build_responses_request.
// When the model does not support verbosity, nil is returned (the override is
// silently ignored, matching the Rust warn-and-drop behaviour).
func (c *ResponsesModelClient) verbosityFor(info modelsmanager.ModelInfo) *protocol.Verbosity {
	if !info.SupportVerbosity {
		return nil
	}
	if c.cfg.ModelVerbosity != nil {
		return c.cfg.ModelVerbosity
	}
	return info.DefaultVerbosity
}

// buildReasoning constructs the optional reasoning block, mirroring the Rust
// `ModelClient::build_reasoning`. Returns nil when the model does not support
// reasoning summaries.
func buildReasoning(info modelsmanager.ModelInfo, effort *protocol.ReasoningEffort, summary protocol.ReasoningSummary) *api.Reasoning {
	if !info.SupportsReasoningSummary {
		return nil
	}
	r := &api.Reasoning{}
	if effort != nil {
		e := effort.ForRequest()
		r.Effort = &e
	} else if info.DefaultReasoningLevel != nil {
		e := info.DefaultReasoningLevel.ForRequest()
		r.Effort = &e
	}
	if summary != protocol.ReasoningSummaryNone {
		s := summary
		r.Summary = &s
	}
	return r
}

// buildTextControls builds the optional text-format controls for a request,
// mirroring `create_text_param_for_request`. The output schema, when present,
// is wrapped into a json_schema text format.
func buildTextControls(verbosity *protocol.Verbosity, outputSchema []byte, strict bool) *api.TextControls {
	var schema *json.RawMessage
	if len(outputSchema) > 0 {
		raw := json.RawMessage(append([]byte(nil), outputSchema...))
		schema = &raw
	}
	return api.CreateTextParamForRequest(verbosity, schema, strict)
}
