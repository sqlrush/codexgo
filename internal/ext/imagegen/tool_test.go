package imagegen

import (
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

const (
	testResult     = "cG5n"
	testOutputHint = "Generated images are saved to /tmp as /tmp/call-1.png by default."
)

func args(action ImagegenAction, prompt string) ImagegenArgs {
	return ImagegenArgs{Prompt: prompt, Action: action}
}

func ptr[T any](v T) *T { return &v }

func generatedItem(result string) protocol.ResponseItem {
	return protocol.ResponseItem{
		Type:        protocol.ResponseItemKindImageGenerationCall,
		ImageID:     "id-" + result,
		ImageStatus: "completed",
		Result:      result,
	}
}

func userImageMessage(urls ...string) protocol.ResponseItem {
	content := make([]protocol.ContentItem, 0, len(urls))
	for _, url := range urls {
		content = append(content, protocol.ContentItem{
			Type:     protocol.ContentItemKindInputImage,
			ImageURL: url,
		})
	}
	return protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "user",
		Content: content,
	}
}

func userTextMessage(text string) protocol.ResponseItem {
	return protocol.ResponseItem{
		Type: protocol.ResponseItemKindMessage,
		Role: "user",
		Content: []protocol.ContentItem{
			{Type: protocol.ContentItemKindInputText, Text: text},
		},
	}
}

func imagegenFunctionCall(callID string) protocol.ResponseItem {
	return protocol.ResponseItem{
		Type:      protocol.ResponseItemKindFunctionCall,
		Name:      ToolName,
		Namespace: ptr(Namespace),
		Arguments: "{}",
		CallID:    callID,
	}
}

func generatedFunctionOutput(callID, result string) protocol.ResponseItem {
	detail := protocol.DefaultImageDetail
	success := true
	return protocol.ResponseItem{
		Type:   protocol.ResponseItemKindFunctionCallOutput,
		CallID: callID,
		Output: &protocol.FunctionCallOutputPayload{
			ContentItems: []protocol.FunctionCallOutputContentItem{
				{
					Type:     protocol.FunctionCallOutputContentItemKindInputImage,
					ImageURL: "data:image/png;base64," + result,
					Detail:   &detail,
				},
				{
					Type: protocol.FunctionCallOutputContentItemKindInputText,
					Text: "generated image save hint",
				},
			},
			Success: &success,
		},
	}
}

func editRequest(t *testing.T, prompt string, history []protocol.ResponseItem) ImageEditRequest {
	t.Helper()
	req, err := RequestForAction(args(ImagegenActionEdit, prompt), history)
	if err != nil {
		t.Fatalf("edit request should build: %v", err)
	}
	if req.Kind != ImageRequestKindEdit || req.Edit == nil {
		t.Fatalf("expected edit request, got kind %d", req.Kind)
	}
	return *req.Edit
}

func expectedEditRequest(prompt string, urls []string) ImageEditRequest {
	images := make([]ImageURL, len(urls))
	for i, url := range urls {
		images[i] = ImageURL{ImageURL: url}
	}
	return ImageEditRequest{
		Images:     images,
		Prompt:     prompt,
		Background: ptr(ImageBackgroundAuto),
		Model:      "gpt-image-2",
		N:          nil,
		Quality:    ptr(ImageQualityAuto),
		Size:       ptr("auto"),
	}
}

func TestUsesReservedImageGenNamespace(t *testing.T) {
	spec, err := ToolSpec()
	if err != nil {
		t.Fatalf("ToolSpec: %v", err)
	}
	if spec.Kind != tools.ToolSpecKindNamespace || spec.Namespace == nil {
		t.Fatalf("imagegen should advertise a namespace tool")
	}
	if spec.Namespace.Name != Namespace {
		t.Errorf("namespace = %q, want %q", spec.Namespace.Name, Namespace)
	}
	if len(spec.Namespace.Tools) != 1 {
		t.Fatalf("namespace should have one tool, got %d", len(spec.Namespace.Tools))
	}
	fn := spec.Namespace.Tools[0]
	if fn.Function.Name != ToolName {
		t.Errorf("function name = %q, want %q", fn.Function.Name, ToolName)
	}
	if fn.Function.Strict {
		t.Errorf("function strict should be false")
	}
}

func TestGenerateUsesFixedRequestDefaults(t *testing.T) {
	req, err := RequestForAction(args(ImagegenActionGenerate, "paint a moonlit lake"), nil)
	if err != nil {
		t.Fatalf("generation request should build: %v", err)
	}
	want := ImageRequest{
		Kind: ImageRequestKindGenerate,
		Generate: &ImageGenerationRequest{
			Prompt:     "paint a moonlit lake",
			Background: ptr(ImageBackgroundAuto),
			Model:      "gpt-image-2",
			N:          nil,
			Quality:    ptr(ImageQualityAuto),
			Size:       ptr("auto"),
		},
	}
	if !reflect.DeepEqual(req, want) {
		t.Errorf("req = %#v, want %#v", req, want)
	}
}

func TestEditMatchesContextSelectorAfterLatestUserAnchor(t *testing.T) {
	history := []protocol.ResponseItem{
		generatedItem("g1"),
		generatedItem("g2"),
		generatedItem("g3"),
		userImageMessage("data:image/png;base64,u1", "data:image/png;base64,u2"),
		generatedItem("g4"),
		generatedItem("g5"),
		generatedItem("g6"),
		generatedItem("g7"),
	}
	got := editRequest(t, "change the lighting", history)
	want := expectedEditRequest("change the lighting", []string{
		"data:image/png;base64,u1",
		"data:image/png;base64,u2",
		"data:image/png;base64,g5",
		"data:image/png;base64,g6",
		"data:image/png;base64,g7",
	})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestEditPreservesGeneratedImageWhenUserAnchorFillsLimit(t *testing.T) {
	history := []protocol.ResponseItem{
		userImageMessage(
			"data:image/png;base64,a",
			"data:image/png;base64,b",
			"data:image/png;base64,c",
			"data:image/png;base64,d",
			"data:image/png;base64,e",
		),
		generatedItem("generated"),
	}
	got := editRequest(t, "edit the last generated image", history)
	want := expectedEditRequest("edit the last generated image", []string{
		"data:image/png;base64,b",
		"data:image/png;base64,c",
		"data:image/png;base64,d",
		"data:image/png;base64,e",
		"data:image/png;base64,generated",
	})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestEditUsesLatestUserUploadBeforeTextOnlyFollowUp(t *testing.T) {
	history := []protocol.ResponseItem{
		userImageMessage("data:image/png;base64,user"),
		userTextMessage("edit this image"),
	}
	got := editRequest(t, "change the lighting", history)
	want := expectedEditRequest("change the lighting", []string{"data:image/png;base64,user"})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestEditReusesImagesFromPriorStandaloneCalls(t *testing.T) {
	history := []protocol.ResponseItem{
		imagegenFunctionCall("imagegen-1"),
		generatedFunctionOutput("imagegen-1", "standalone"),
	}
	got := editRequest(t, "change the lighting", history)
	want := expectedEditRequest("change the lighting", []string{"data:image/png;base64,standalone"})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestEditKeepsNewestStandaloneGeneratedWhenOverLimit(t *testing.T) {
	var history []protocol.ResponseItem
	for i := 1; i <= 6; i++ {
		callID := "imagegen-" + itoa(i)
		history = append(history, imagegenFunctionCall(callID))
		history = append(history, generatedFunctionOutput(callID, itoa(i)))
	}
	got := editRequest(t, "change the lighting", history)
	want := expectedEditRequest("change the lighting", []string{
		"data:image/png;base64,2",
		"data:image/png;base64,3",
		"data:image/png;base64,4",
		"data:image/png;base64,5",
		"data:image/png;base64,6",
	})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestEditWithoutImageHistoryReturnsError(t *testing.T) {
	_, err := RequestForAction(args(ImagegenActionEdit, "change the lighting"), nil)
	if err == nil {
		t.Fatalf("edit should require image context")
	}
	want := "image edit requested without any usable image in conversation history"
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestGeneratedOutputReturnsImageAndHint(t *testing.T) {
	output := GeneratedImageOutput{
		Result:     testResult,
		OutputHint: ptr(testOutputHint),
	}
	item := output.ToResponseItem("call-1")
	if item.Kind != tools.ResponseInputItemKindFunctionCallOutput {
		t.Fatalf("expected function call output, got kind %d", item.Kind)
	}
	detail := protocol.DefaultImageDetail
	want := []protocol.FunctionCallOutputContentItem{
		{
			Type:     protocol.FunctionCallOutputContentItemKindInputImage,
			ImageURL: "data:image/png;base64," + testResult,
			Detail:   &detail,
		},
		{
			Type: protocol.FunctionCallOutputContentItemKindInputText,
			Text: testOutputHint,
		},
	}
	if !reflect.DeepEqual(item.Output.ContentItems, want) {
		t.Errorf("content = %#v, want %#v", item.Output.ContentItems, want)
	}
}

// itoa is a tiny base-10 integer formatter for small positive test ids.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
