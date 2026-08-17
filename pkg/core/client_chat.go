package core

// ChatModelClient: the chat-completions counterpart of ResponsesModelClient.
//
// codexgo extension (upstream 0.136 removed the chat wire protocol): providers
// configured with `wire_api = "chat"` (GLM, DeepSeek, and other OpenAI-
// compatible backends) stream turns over POST {base_url}/chat/completions.
// The conversation history ([]protocol.ResponseItem) is translated into chat
// messages, the function tool specs into chat tools, and the api layer
// re-emits the aggregated chat stream as the standard ResponseEvent sequence,
// so the turn runner is wire-protocol agnostic. See DEVIATIONS.md
// "wire_api chat".

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/client"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// ChatModelClient streams turns over the chat-completions wire protocol. It
// reuses ModelClientConfig; reasoning/verbosity/service-tier knobs that have no
// chat equivalent are ignored.
type ChatModelClient struct {
	cfg ModelClientConfig

	once       sync.Once
	chatClient *api.ChatCompletionsClient
}

// compile-time assertion that ChatModelClient satisfies ModelClient.
var _ ModelClient = (*ChatModelClient)(nil)

// NewChatModelClient constructs a session-scoped ChatModelClient. No network
// I/O happens until Stream is called.
func NewChatModelClient(cfg ModelClientConfig) (*ChatModelClient, error) {
	if cfg.Auth == nil {
		return nil, fmt.Errorf("core: ModelClientConfig.Auth is required")
	}
	if cfg.Provider.BaseURL == "" {
		return nil, fmt.Errorf("core: ModelClientConfig.Provider.BaseURL is required")
	}
	return &ChatModelClient{cfg: cfg}, nil
}

// ModelSlug returns the canonical slug of the targeted model.
func (c *ChatModelClient) ModelSlug() string { return c.cfg.ModelInfo.Slug }

// ContextWindow reports the model's effective context window, if known.
func (c *ChatModelClient) ContextWindow() *int64 { return c.cfg.ModelInfo.ContextWindow }

// Stream issues a single chat-completions request built from prompt and yields
// the aggregated response events. The channel closes when the stream ends.
func (c *ChatModelClient) Stream(ctx context.Context, prompt Prompt) (<-chan api.ResponseEvent, error) {
	request, err := c.buildChatRequest(prompt)
	if err != nil {
		return nil, fmt.Errorf("core: build chat request: %w", err)
	}

	stream, apiErr := c.client().StreamRequest(ctx, request, nil)
	if apiErr != nil {
		return nil, fmt.Errorf("core: chat stream request: %w", apiErr)
	}

	out := make(chan api.ResponseEvent, streamForwardBuffer)
	go forwardResponseStream(ctx, stream, out)
	return out, nil
}

// client lazily builds the api chat client.
func (c *ChatModelClient) client() *api.ChatCompletionsClient {
	c.once.Do(func() {
		transport := c.cfg.Transport
		if transport == nil {
			transport = client.NewHTTPClientTransport(http.DefaultClient)
		}
		c.chatClient = api.NewChatCompletionsClient(transport, c.cfg.Provider, c.cfg.Auth)
	})
	return c.chatClient
}

// buildChatRequest translates the Prompt into a ChatCompletionsRequest.
func (c *ChatModelClient) buildChatRequest(prompt Prompt) (api.ChatCompletionsRequest, error) {
	toolsJSON, err := tools.CreateToolsJSONForChatAPI(prompt.Tools)
	if err != nil {
		return api.ChatCompletionsRequest{}, err
	}

	messages := make([]api.ChatMessage, 0, len(prompt.Input)+1)
	if instructions := c.instructionsFor(prompt); instructions != "" {
		messages = append(messages, chatTextMessage("system", instructions))
	}
	messages = append(messages, chatMessagesFromItems(prompt.Input)...)

	toolChoice := ""
	if len(toolsJSON) > 0 {
		toolChoice = "auto"
	}

	return api.ChatCompletionsRequest{
		Model:      c.cfg.ModelInfo.Slug,
		Messages:   messages,
		Tools:      toolsJSON,
		ToolChoice: toolChoice,
		Stream:     true,
	}, nil
}

// instructionsFor returns the effective base instructions, honoring the
// Prompt-level override (same precedence as the Responses client).
func (c *ChatModelClient) instructionsFor(prompt Prompt) string {
	if prompt.BaseInstructionsOverride != nil {
		return *prompt.BaseInstructionsOverride
	}
	return c.cfg.ModelInfo.BaseInstructions
}

// chatTextMessage builds a plain text chat message.
func chatTextMessage(role, text string) api.ChatMessage {
	content := text
	return api.ChatMessage{Role: role, Content: &content}
}

// chatMessagesFromItems converts conversation history items into chat messages:
//
//   - message items keep their role, with "developer" mapped to "system" (the
//     chat protocol has no developer role) and content flattened to text
//     (image parts are dropped — chat-wire vendors here are text-first);
//   - consecutive function_call items merge into ONE assistant message whose
//     tool_calls array carries them all (strict backends require tool results
//     to follow an assistant message listing the matching tool_call ids);
//   - function_call_output items become role:"tool" results;
//   - reasoning and other Responses-only items are dropped (chat backends do
//     not accept them back).
func chatMessagesFromItems(items []protocol.ResponseItem) []api.ChatMessage {
	messages := make([]api.ChatMessage, 0, len(items))
	var pendingCalls []api.ChatToolCall

	flushCalls := func() {
		if len(pendingCalls) == 0 {
			return
		}
		messages = append(messages, api.ChatMessage{
			Role:      "assistant",
			Content:   nil,
			ToolCalls: pendingCalls,
		})
		pendingCalls = nil
	}

	for i := range items {
		item := items[i]
		switch item.Type {
		case protocol.ResponseItemKindMessage:
			flushCalls()
			role := item.Role
			if role == "developer" {
				role = "system"
			}
			messages = append(messages, chatTextMessage(role, contentItemsText(item.Content)))

		case protocol.ResponseItemKindFunctionCall:
			pendingCalls = append(pendingCalls, api.ChatToolCall{
				ID:   item.CallID,
				Type: "function",
				Function: api.ChatToolCallFunction{
					Name:      item.Name,
					Arguments: item.Arguments,
				},
			})

		case protocol.ResponseItemKindFunctionCallOutput:
			flushCalls()
			messages = append(messages, api.ChatMessage{
				Role:       "tool",
				Content:    chatStrPtr(functionCallOutputText(item.Output)),
				ToolCallID: item.CallID,
			})

		default:
			// Reasoning, custom tool calls, tool_search, web_search and other
			// Responses-only items have no chat representation.
		}
	}
	flushCalls()
	return messages
}

// contentItemsText flattens message content to its text parts.
func contentItemsText(content []protocol.ContentItem) string {
	var b strings.Builder
	for _, c := range content {
		switch c.Type {
		case protocol.ContentItemKindInputText, protocol.ContentItemKindOutputText:
			b.WriteString(c.Text)
		default:
			// Images and other modalities are dropped on the chat wire.
		}
	}
	return b.String()
}

// functionCallOutputText flattens a tool result payload to text.
func functionCallOutputText(output *protocol.FunctionCallOutputPayload) string {
	if output == nil {
		return ""
	}
	if output.Text != nil {
		return *output.Text
	}
	var b strings.Builder
	for _, item := range output.ContentItems {
		if item.Type == protocol.FunctionCallOutputContentItemKindInputText {
			b.WriteString(item.Text)
		}
	}
	return b.String()
}

// chatStrPtr returns a pointer to s.
func chatStrPtr(s string) *string { return &s }
