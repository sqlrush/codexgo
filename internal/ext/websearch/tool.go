package websearch

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// Namespace and tool name constants. Rust: WEB_NAMESPACE, RUN_TOOL_NAME.
const (
	// Namespace is the Responses-API namespace for standalone web search.
	Namespace = "web"
	// RunToolName is the web-run tool name within the namespace.
	RunToolName = "run"
)

// ParseCommands parses the model-provided web.run arguments. Empty arguments
// yield the default (all-None) SearchCommands. Rust: parse_commands.
func ParseCommands(arguments string) (SearchCommands, error) {
	if strings.TrimSpace(arguments) == "" {
		return SearchCommands{}, nil
	}
	var commands SearchCommands
	if err := json.Unmarshal([]byte(arguments), &commands); err != nil {
		return SearchCommands{}, fmt.Errorf("websearch: parse commands: %w", err)
	}
	return commands, nil
}

// CommandAction derives the WebSearchAction reported for a set of commands,
// preferring search > image > open > find, falling back to Other. Rust:
// command_action.
func CommandAction(commands SearchCommands) protocol.WebSearchAction {
	if commands.SearchQuery != nil {
		if action := queryAction(*commands.SearchQuery); action != nil {
			return *action
		}
	}
	if commands.ImageQuery != nil {
		if action := queryAction(*commands.ImageQuery); action != nil {
			return *action
		}
	}
	if commands.Open != nil && len(*commands.Open) > 0 {
		operation := (*commands.Open)[0]
		if url := literalURL(operation.RefID); url != nil {
			return protocol.WebSearchAction{
				Type: protocol.WebSearchActionKindOpenPage,
				URL:  url,
			}
		}
	}
	if commands.Find != nil && len(*commands.Find) > 0 {
		operation := (*commands.Find)[0]
		pattern := operation.Pattern
		return protocol.WebSearchAction{
			Type:    protocol.WebSearchActionKindFindInPage,
			URL:     literalURL(operation.RefID),
			Pattern: &pattern,
		}
	}
	return protocol.WebSearchAction{Type: protocol.WebSearchActionKindOther}
}

// queryAction maps a slice of queries to a Search action. Rust: query_action.
func queryAction(queries []SearchQuery) *protocol.WebSearchAction {
	switch len(queries) {
	case 0:
		return nil
	case 1:
		query := queries[0].Q
		return &protocol.WebSearchAction{
			Type:  protocol.WebSearchActionKindSearch,
			Query: &query,
		}
	default:
		qs := make([]string, len(queries))
		for i, q := range queries {
			qs[i] = q.Q
		}
		return &protocol.WebSearchAction{
			Type:    protocol.WebSearchActionKindSearch,
			Queries: &qs,
		}
	}
}

// literalURL returns the ref id when it parses as an absolute URL, else nil.
// Rust: literal_url (Url::parse(ref_id).is_ok()).
func literalURL(refID string) *string {
	if isAbsoluteURL(refID) {
		s := refID
		return &s
	}
	return nil
}

// ToolSpec builds the namespace function schema exposed to the model. The
// schema is parsed without compaction so field metadata/descriptions match the
// hosted tool definition. Rust: WebSearchTool::spec.
func ToolSpec() (tools.ToolSpec, error) {
	parameters, err := tools.ParseToolInputSchemaWithoutCompaction(json.RawMessage(commandsSchema))
	if err != nil {
		return tools.ToolSpec{}, fmt.Errorf("websearch: parse command schema: %w", err)
	}
	return tools.NamespaceToolSpec(tools.ResponsesApiNamespace{
		Name:        Namespace,
		Description: tools.DefaultNamespaceDescription(Namespace),
		Tools: []tools.ResponsesApiNamespaceTool{
			tools.FunctionNamespaceTool(tools.ResponsesApiTool{
				Name:        RunToolName,
				Description: webRunDescription,
				Strict:      false,
				Parameters:  parameters,
			}),
		},
	}), nil
}
