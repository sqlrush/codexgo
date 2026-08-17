package tools

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/internal/features"
	"github.com/sqlrush/codexgo/internal/modelsmanager"
	"github.com/sqlrush/codexgo/internal/protocol"
)

func webSearchModePtr(m protocol.WebSearchMode) *protocol.WebSearchMode { return &m }

// TestCreateWebSearchTool asserts the hosted web_search spec wire form mirrors
// create_web_search_tool: cached/live select external_web_access, disabled/nil
// omit the tool, text_and_image adds search_content_types.
func TestCreateWebSearchTool(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		opts     WebSearchToolOptions
		wantOK   bool
		wantJSON string
	}{
		{
			name: "cached text_and_image (gpt-5.5 default)",
			opts: WebSearchToolOptions{
				WebSearchMode:     webSearchModePtr(protocol.WebSearchModeCached),
				WebSearchToolType: modelsmanager.WebSearchToolTypeTextAndImage,
			},
			wantOK:   true,
			wantJSON: `{"type":"web_search","external_web_access":false,"search_content_types":["text","image"]}`,
		},
		{
			name: "live text-only",
			opts: WebSearchToolOptions{
				WebSearchMode:     webSearchModePtr(protocol.WebSearchModeLive),
				WebSearchToolType: modelsmanager.WebSearchToolTypeText,
			},
			wantOK:   true,
			wantJSON: `{"type":"web_search","external_web_access":true}`,
		},
		{
			name: "disabled mode omits the tool",
			opts: WebSearchToolOptions{
				WebSearchMode: webSearchModePtr(protocol.WebSearchModeDisabled),
			},
			wantOK: false,
		},
		{
			name:   "nil mode (provider without web search) omits the tool",
			opts:   WebSearchToolOptions{},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec, ok := CreateWebSearchTool(tt.opts)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			got, err := json.Marshal(spec)
			if err != nil {
				t.Fatalf("marshal web_search spec: %v", err)
			}
			if string(got) != tt.wantJSON {
				t.Errorf("web_search spec mismatch\n got: %s\nwant: %s", got, tt.wantJSON)
			}
		})
	}
}

// TestRequestUserInputAvailableModes ports the reference
// `request_user_input_modes_follow_default_mode_feature` case.
func TestRequestUserInputAvailableModes(t *testing.T) {
	t.Parallel()
	f := features.NewFeaturesWithDefaults()

	f.Disable(features.FeatureDefaultModeRequestUserInput)
	got := RequestUserInputAvailableModes(&f)
	if len(got) != 1 || got[0] != protocol.ModeKindPlan {
		t.Errorf("modes = %v, want [plan]", got)
	}

	f.Enable(features.FeatureDefaultModeRequestUserInput)
	got = RequestUserInputAvailableModes(&f)
	if len(got) != 2 || got[0] != protocol.ModeKindDefault || got[1] != protocol.ModeKindPlan {
		t.Errorf("modes = %v, want [default plan]", got)
	}
}

// TestRequestUserInputToolDescription pins the allowed-mode phrasing against
// request_user_input_tool_description / format_allowed_modes.
func TestRequestUserInputToolDescription(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		modes []protocol.ModeKind
		want  string
	}{
		{
			name:  "plan only (default)",
			modes: []protocol.ModeKind{protocol.ModeKindPlan},
			want:  "Request user input for one to three short questions and wait for the response. This tool is only available in Plan mode.",
		},
		{
			name:  "default + plan",
			modes: []protocol.ModeKind{protocol.ModeKindDefault, protocol.ModeKindPlan},
			want:  "Request user input for one to three short questions and wait for the response. This tool is only available in Default or Plan mode.",
		},
		{
			name:  "no modes",
			modes: nil,
			want:  "Request user input for one to three short questions and wait for the response. This tool is only available in no modes.",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := RequestUserInputToolDescription(tt.modes); got != tt.want {
				t.Errorf("description mismatch\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

// TestCreateRequestUserInputToolSpec asserts the request_user_input schema is
// byte-faithful to create_request_user_input_tool: sorted-key properties,
// nested question/option object schemas, strict:false.
func TestCreateRequestUserInputToolSpec(t *testing.T) {
	t.Parallel()
	desc := RequestUserInputToolDescription([]protocol.ModeKind{protocol.ModeKindPlan})
	spec := CreateRequestUserInputTool(desc)
	got, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal request_user_input spec: %v", err)
	}

	want := `{"type":"function","name":"request_user_input","description":"Request user input for one to three short questions and wait for the response. This tool is only available in Plan mode.","strict":false,"parameters":{"type":"object","properties":{"questions":{"type":"array","description":"Questions to show the user. Prefer 1 and do not exceed 3","items":{"type":"object","properties":{"header":{"type":"string","description":"Short header label shown in the UI (12 or fewer chars)."},"id":{"type":"string","description":"Stable identifier for mapping answers (snake_case)."},"options":{"type":"array","description":"Provide 2-3 mutually exclusive choices. Put the recommended option first and suffix its label with \"(Recommended)\". Do not include an \"Other\" option in this list; the client will add a free-form \"Other\" option automatically.","items":{"type":"object","properties":{"description":{"type":"string","description":"One short sentence explaining impact/tradeoff if selected."},"label":{"type":"string","description":"User-facing label (1-5 words)."}},"required":["label","description"],"additionalProperties":false}},"question":{"type":"string","description":"Single-sentence prompt shown to the user."}},"required":["id","header","question","options"],"additionalProperties":false}}},"required":["questions"],"additionalProperties":false}}`

	if string(got) != want {
		t.Errorf("request_user_input spec mismatch\n got: %s\nwant: %s", got, want)
	}
}
