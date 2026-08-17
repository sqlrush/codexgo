package tools

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// ToolSpecKind discriminates a ToolSpec. Mirrors the Rust enum variants, which
// serialize via the `type` tag.
type ToolSpecKind int

// ToolSpecKind variants.
const (
	// ToolSpecKindFunction is a function tool ("function").
	ToolSpecKindFunction ToolSpecKind = iota
	// ToolSpecKindNamespace is a namespace of tools ("namespace").
	ToolSpecKindNamespace
	// ToolSpecKindToolSearch is the tool_search tool ("tool_search").
	ToolSpecKindToolSearch
	// ToolSpecKindImageGeneration is the image_generation tool.
	ToolSpecKindImageGeneration
	// ToolSpecKindWebSearch is the web_search tool.
	ToolSpecKindWebSearch
	// ToolSpecKindFreeform is a custom/freeform tool ("custom").
	ToolSpecKindFreeform
)

// ToolSpec serializes to a valid "Tool" in the OpenAI Responses API. Mirrors
// Rust `ToolSpec` (`#[serde(tag = "type")]`). Exactly one variant field is
// populated per Kind.
type ToolSpec struct {
	Kind ToolSpecKind

	// Function variant ("function").
	Function *ResponsesApiTool
	// Namespace variant ("namespace").
	Namespace *ResponsesApiNamespace
	// ToolSearch variant ("tool_search").
	ToolSearch *ToolSearchSpec
	// ImageGeneration variant ("image_generation").
	ImageGeneration *ImageGenerationSpec
	// WebSearch variant ("web_search").
	WebSearch *WebSearchSpec
	// Freeform variant ("custom").
	Freeform *FreeformTool
}

// ToolSearchSpec holds the tool_search tool fields.
type ToolSearchSpec struct {
	Execution   string
	Description string
	Parameters  JsonSchema
}

// ImageGenerationSpec holds the image_generation tool fields.
type ImageGenerationSpec struct {
	OutputFormat string
}

// WebSearchSpec holds the web_search tool fields. Every field uses
// `skip_serializing_if = "Option::is_none"` in Rust, so absent fields are
// omitted from the wire form.
type WebSearchSpec struct {
	ExternalWebAccess  *bool
	Filters            *ResponsesApiWebSearchFilters
	UserLocation       *ResponsesApiWebSearchUserLocation
	SearchContextSize  *protocol.WebSearchContextSize
	SearchContentTypes []string
}

// FunctionToolSpec constructs a Function ToolSpec.
func FunctionToolSpec(tool ResponsesApiTool) ToolSpec {
	return ToolSpec{Kind: ToolSpecKindFunction, Function: &tool}
}

// NamespaceToolSpec constructs a Namespace ToolSpec.
func NamespaceToolSpec(namespace ResponsesApiNamespace) ToolSpec {
	return ToolSpec{Kind: ToolSpecKindNamespace, Namespace: &namespace}
}

// ToolSearchToolSpec constructs a ToolSearch ToolSpec.
func ToolSearchToolSpec(spec ToolSearchSpec) ToolSpec {
	return ToolSpec{Kind: ToolSpecKindToolSearch, ToolSearch: &spec}
}

// ImageGenerationToolSpec constructs an ImageGeneration ToolSpec.
func ImageGenerationToolSpec(spec ImageGenerationSpec) ToolSpec {
	return ToolSpec{Kind: ToolSpecKindImageGeneration, ImageGeneration: &spec}
}

// WebSearchToolSpec constructs a WebSearch ToolSpec.
func WebSearchToolSpec(spec WebSearchSpec) ToolSpec {
	return ToolSpec{Kind: ToolSpecKindWebSearch, WebSearch: &spec}
}

// FreeformToolSpec constructs a Freeform ToolSpec.
func FreeformToolSpec(tool FreeformTool) ToolSpec {
	return ToolSpec{Kind: ToolSpecKindFreeform, Freeform: &tool}
}

// Name returns the model-visible name of the tool. Mirrors Rust `ToolSpec::name`.
func (s ToolSpec) Name() string {
	switch s.Kind {
	case ToolSpecKindFunction:
		return s.Function.Name
	case ToolSpecKindNamespace:
		return s.Namespace.Name
	case ToolSpecKindToolSearch:
		return "tool_search"
	case ToolSpecKindImageGeneration:
		return "image_generation"
	case ToolSpecKindWebSearch:
		return "web_search"
	case ToolSpecKindFreeform:
		return s.Freeform.Name
	default:
		return ""
	}
}

// ToolSpecFromLoadable converts a LoadableToolSpec into a ToolSpec. Mirrors the
// Rust `From<LoadableToolSpec>` impl.
func ToolSpecFromLoadable(spec LoadableToolSpec) ToolSpec {
	switch spec.Kind {
	case LoadableToolSpecKindFunction:
		return FunctionToolSpec(*spec.Function)
	case LoadableToolSpecKindNamespace:
		return NamespaceToolSpec(*spec.Namespace)
	default:
		return ToolSpec{}
	}
}

// MarshalJSON emits the internally-tagged wire shape with the "type" tag first,
// matching serde's output for each variant.
func (s ToolSpec) MarshalJSON() ([]byte, error) {
	switch s.Kind {
	case ToolSpecKindFunction:
		if s.Function == nil {
			return nil, fmt.Errorf("ToolSpec function variant has nil tool")
		}
		return marshalTaggedTool("function", *s.Function)
	case ToolSpecKindNamespace:
		if s.Namespace == nil {
			return nil, fmt.Errorf("ToolSpec namespace variant has nil namespace")
		}
		return marshalTaggedNamespace("namespace", *s.Namespace)
	case ToolSpecKindToolSearch:
		if s.ToolSearch == nil {
			return nil, fmt.Errorf("ToolSpec tool_search variant has nil spec")
		}
		m := orderedMap{}
		m.set("type", "tool_search")
		m.set("execution", s.ToolSearch.Execution)
		m.set("description", s.ToolSearch.Description)
		m.set("parameters", s.ToolSearch.Parameters)
		return m.MarshalJSON()
	case ToolSpecKindImageGeneration:
		if s.ImageGeneration == nil {
			return nil, fmt.Errorf("ToolSpec image_generation variant has nil spec")
		}
		m := orderedMap{}
		m.set("type", "image_generation")
		m.set("output_format", s.ImageGeneration.OutputFormat)
		return m.MarshalJSON()
	case ToolSpecKindWebSearch:
		if s.WebSearch == nil {
			return nil, fmt.Errorf("ToolSpec web_search variant has nil spec")
		}
		return marshalWebSearch(*s.WebSearch)
	case ToolSpecKindFreeform:
		if s.Freeform == nil {
			return nil, fmt.Errorf("ToolSpec custom variant has nil tool")
		}
		m := orderedMap{}
		m.set("type", "custom")
		m.set("name", s.Freeform.Name)
		m.set("description", s.Freeform.Description)
		m.set("format", s.Freeform.Format)
		return m.MarshalJSON()
	default:
		return nil, fmt.Errorf("unknown ToolSpec kind: %d", s.Kind)
	}
}

func marshalWebSearch(spec WebSearchSpec) ([]byte, error) {
	m := orderedMap{}
	m.set("type", "web_search")
	if spec.ExternalWebAccess != nil {
		m.set("external_web_access", *spec.ExternalWebAccess)
	}
	if spec.Filters != nil {
		m.set("filters", *spec.Filters)
	}
	if spec.UserLocation != nil {
		m.set("user_location", *spec.UserLocation)
	}
	if spec.SearchContextSize != nil {
		m.set("search_context_size", *spec.SearchContextSize)
	}
	if spec.SearchContentTypes != nil {
		m.set("search_content_types", spec.SearchContentTypes)
	}
	return m.MarshalJSON()
}

// CreateToolsJSONForResponsesAPI serializes tool specs into JSON values that are
// compatible with Function Calling in the Responses API. Mirrors Rust
// `create_tools_json_for_responses_api`.
func CreateToolsJSONForResponsesAPI(tools []ToolSpec) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(tools))
	for i, tool := range tools {
		raw, err := json.Marshal(tool)
		if err != nil {
			return nil, fmt.Errorf("serialize tool %d (%s): %w", i, tool.Name(), err)
		}
		out = append(out, raw)
	}
	return out, nil
}

// ResponsesApiWebSearchFilters constrains web search domains. Mirrors Rust
// `ResponsesApiWebSearchFilters`. `allowed_domains` is omitted when nil.
type ResponsesApiWebSearchFilters struct {
	AllowedDomains *[]string `json:"allowed_domains,omitempty"`
}

// ResponsesApiWebSearchFiltersFromConfig converts a protocol WebSearchFilters.
// Mirrors the Rust `From<ConfigWebSearchFilters>` impl.
func ResponsesApiWebSearchFiltersFromConfig(filters protocol.WebSearchFilters) ResponsesApiWebSearchFilters {
	return ResponsesApiWebSearchFilters{AllowedDomains: filters.AllowedDomains}
}

// ResponsesApiWebSearchUserLocation describes an approximate user location for
// web search. Mirrors Rust `ResponsesApiWebSearchUserLocation`. Optional fields
// are omitted when nil.
type ResponsesApiWebSearchUserLocation struct {
	Type     protocol.WebSearchUserLocationType `json:"type"`
	Country  *string                            `json:"country,omitempty"`
	Region   *string                            `json:"region,omitempty"`
	City     *string                            `json:"city,omitempty"`
	Timezone *string                            `json:"timezone,omitempty"`
}

// ResponsesApiWebSearchUserLocationFromConfig converts a protocol
// WebSearchUserLocation. Mirrors the Rust `From<ConfigWebSearchUserLocation>`.
func ResponsesApiWebSearchUserLocationFromConfig(loc protocol.WebSearchUserLocation) ResponsesApiWebSearchUserLocation {
	return ResponsesApiWebSearchUserLocation{
		Type:     loc.Type,
		Country:  loc.Country,
		Region:   loc.Region,
		City:     loc.City,
		Timezone: loc.Timezone,
	}
}
