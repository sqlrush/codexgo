package imagegen

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// Namespace and tool name constants. Rust: IMAGE_GEN_NAMESPACE, IMAGEGEN_TOOL_NAME.
const (
	// Namespace is the reserved Responses-API namespace for image generation.
	Namespace = "image_gen"
	// ToolName is the image-generation tool name within the namespace.
	ToolName = "imagegen"
)

// imageModel is the fixed model used for all standalone image requests. Rust:
// IMAGE_MODEL.
const imageModel = "gpt-image-2"

// maxEditImages bounds how many images an edit request may include. Rust:
// MAX_EDIT_IMAGES.
const maxEditImages = 5

// ImagegenAction selects between generating a new image and editing an existing
// one. Rust: ImagegenAction (rename_all = "lowercase").
type ImagegenAction string

// ImagegenAction variants.
const (
	ImagegenActionGenerate ImagegenAction = "generate"
	ImagegenActionEdit     ImagegenAction = "edit"
)

// ImagegenArgs are the strict model-facing arguments. Rust: ImagegenArgs
// (deny_unknown_fields).
type ImagegenArgs struct {
	Prompt string         `json:"prompt"`
	Action ImagegenAction `json:"action"`
}

// ImageRequestKind discriminates an ImageRequest.
type ImageRequestKind int

// ImageRequestKind variants.
const (
	// ImageRequestKindGenerate is a standalone generation request.
	ImageRequestKindGenerate ImageRequestKind = iota
	// ImageRequestKindEdit is a standalone edit request.
	ImageRequestKindEdit
)

// ImageRequest is the resolved image API request for a model action. Rust:
// ImageRequest enum.
type ImageRequest struct {
	Kind     ImageRequestKind
	Generate *ImageGenerationRequest
	Edit     *ImageEditRequest
}

// RequestForAction maps the model-selected action to the fixed image API request
// parameters. Rust: request_for_action.
func RequestForAction(args ImagegenArgs, history []protocol.ResponseItem) (ImageRequest, error) {
	background := ImageBackgroundAuto
	quality := ImageQualityAuto
	size := "auto"
	switch args.Action {
	case ImagegenActionGenerate:
		return ImageRequest{
			Kind: ImageRequestKindGenerate,
			Generate: &ImageGenerationRequest{
				Prompt:     args.Prompt,
				Background: &background,
				Model:      imageModel,
				N:          nil,
				Quality:    &quality,
				Size:       &size,
			},
		}, nil
	case ImagegenActionEdit:
		images := editImages(history)
		if len(images) == 0 {
			return ImageRequest{}, fmt.Errorf("image edit requested without any usable image in conversation history")
		}
		return ImageRequest{
			Kind: ImageRequestKindEdit,
			Edit: &ImageEditRequest{
				Images:     images,
				Prompt:     args.Prompt,
				Background: &background,
				Model:      imageModel,
				N:          nil,
				Quality:    &quality,
				Size:       &size,
			},
		}, nil
	default:
		return ImageRequest{}, fmt.Errorf("imagegen: unknown action %q", args.Action)
	}
}

// editImages selects edit context using the hosted imagegen anchor and
// truncation behavior. Rust: edit_images.
func editImages(history []protocol.ResponseItem) []ImageURL {
	userImages, followUpStart := latestUploadedImages(history)

	var generatedImages []ImageURL
	for i := followUpStart; i < len(history); i++ {
		item := history[i]
		switch item.Type {
		case protocol.ResponseItemKindImageGenerationCall:
			if item.Result != "" {
				generatedImages = append(generatedImages, ImageURL{
					ImageURL: fmt.Sprintf("data:image/png;base64,%s", item.Result),
				})
			}
		case protocol.ResponseItemKindFunctionCallOutput:
			if isStandaloneImagegenOutput(history, item.CallID) && item.Output != nil {
				for _, content := range item.Output.ContentItems {
					if content.Type == protocol.FunctionCallOutputContentItemKindInputImage {
						generatedImages = append(generatedImages, ImageURL{ImageURL: content.ImageURL})
					}
				}
			}
		default:
			// Other item types contribute no edit context.
		}
	}
	return truncateImages(userImages, generatedImages)
}

// latestUploadedImages returns the images from the most recent user message that
// contains input images, plus the index just past that message. Rust: the
// latest_uploaded_images find_map plus the map_or_else fallback.
func latestUploadedImages(history []protocol.ResponseItem) ([]ImageURL, int) {
	for index := len(history) - 1; index >= 0; index-- {
		item := history[index]
		if item.Type != protocol.ResponseItemKindMessage || item.Role != "user" {
			continue
		}
		var images []ImageURL
		for _, content := range item.Content {
			if content.Type == protocol.ContentItemKindInputImage {
				images = append(images, ImageURL{ImageURL: content.ImageURL})
			}
		}
		if len(images) > 0 {
			return images, index + 1
		}
	}
	return nil, 0
}

// isStandaloneImagegenOutput reports whether callID corresponds to a prior
// standalone imagegen function call in history. Rust: the inner history.iter().any
// match in edit_images.
func isStandaloneImagegenOutput(history []protocol.ResponseItem, callID string) bool {
	for _, item := range history {
		if item.Type == protocol.ResponseItemKindFunctionCall &&
			item.Namespace != nil &&
			item.CallID == callID &&
			item.Name == ToolName &&
			*item.Namespace == Namespace {
			return true
		}
	}
	return false
}

// truncateImages truncates edit inputs while preserving the newest generated
// image when possible. Rust: truncate_images.
func truncateImages(userImages, generatedImages []ImageURL) []ImageURL {
	excess := saturatingSub(len(userImages)+len(generatedImages), maxEditImages)
	dropGenerated := minInt(excess, saturatingSub(len(generatedImages), 1))
	generatedImages = generatedImages[dropGenerated:]
	excess -= dropGenerated
	dropUser := minInt(excess, len(userImages))
	userImages = userImages[dropUser:]
	excess -= dropUser
	generatedImages = generatedImages[excess:]

	result := make([]ImageURL, 0, len(userImages)+len(generatedImages))
	result = append(result, userImages...)
	result = append(result, generatedImages...)
	return result
}

func saturatingSub(a, b int) int {
	if a < b {
		return 0
	}
	return a - b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ParseArgs parses the strict model-facing arguments for an image-generation
// call. Rust: parse_args (deny_unknown_fields rejects extra keys).
func ParseArgs(arguments string) (ImagegenArgs, error) {
	dec := json.NewDecoder(stringReader(arguments))
	dec.DisallowUnknownFields()
	var args ImagegenArgs
	if err := dec.Decode(&args); err != nil {
		return ImagegenArgs{}, fmt.Errorf("imagegen: parse arguments: %w", err)
	}
	return args, nil
}

// ToolSpec builds the namespace function schema exposed to the model. Rust:
// imagegen_tool_spec. The strict flag is false and the description is the
// embedded imagegen_description.md.
func ToolSpec() (tools.ToolSpec, error) {
	parameters, err := tools.ParseToolInputSchema(json.RawMessage(imagegenInputSchema))
	if err != nil {
		return tools.ToolSpec{}, fmt.Errorf("imagegen: parse input schema: %w", err)
	}
	return tools.NamespaceToolSpec(tools.ResponsesApiNamespace{
		Name:        Namespace,
		Description: tools.DefaultNamespaceDescription(Namespace),
		Tools: []tools.ResponsesApiNamespaceTool{
			tools.FunctionNamespaceTool(tools.ResponsesApiTool{
				Name:        ToolName,
				Description: imagegenDescription,
				Strict:      false,
				Parameters:  parameters,
			}),
		},
	}), nil
}
