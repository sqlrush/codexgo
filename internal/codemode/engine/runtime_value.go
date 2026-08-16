package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dop251/goja"
)

const imageHelperExpectsMessage = "image expects a non-empty image URL string, an object with image_url and optional detail, or a raw MCP image block"
const codexImageDetailMetaKey = "codex/imageDetail"

// serializeOutputText converts a JS value into the string that text()/notify()
// append. Primitives stringify directly; everything else is JSON.stringify'd,
// mirroring codex's `serialize_output_text`.
func serializeOutputText(rt *goja.Runtime, value goja.Value) (string, error) {
	if value == nil {
		return "undefined", nil
	}
	if isPrimitiveForText(value) {
		return value.String(), nil
	}
	stringified, ok, err := jsonStringify(rt, value)
	if err != nil {
		return "", err
	}
	if !ok {
		// JSON.stringify returned undefined (e.g. a bare function); fall back to
		// the lossy string form, matching V8's to_rust_string_lossy fallback.
		return value.String(), nil
	}
	return stringified, nil
}

func isPrimitiveForText(value goja.Value) bool {
	if goja.IsUndefined(value) || goja.IsNull(value) {
		return true
	}
	switch value.ExportType().Kind().String() {
	case "bool", "string",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64":
		return true
	}
	// big.Int and similar export as non-primitive kinds but behave like numbers
	// for text(); detect them via the underlying export type name.
	exportType := value.ExportType()
	if exportType != nil && exportType.String() == "*big.Int" {
		return true
	}
	return false
}

// jsonStringify invokes JSON.stringify(value). The second return reports whether
// a string result was produced (false means JSON.stringify yielded undefined,
// e.g. for functions). Errors mirror codex's exception capture.
func jsonStringify(rt *goja.Runtime, value goja.Value) (string, bool, error) {
	jsonObj, ok := rt.Get("JSON").(*goja.Object)
	if !ok {
		return "", false, fmt.Errorf("JSON global unavailable")
	}
	stringify, ok := goja.AssertFunction(jsonObj.Get("stringify"))
	if !ok {
		return "", false, fmt.Errorf("JSON.stringify unavailable")
	}
	result, err := stringify(goja.Undefined(), value)
	if err != nil {
		return "", false, fmt.Errorf("%s", valueToErrorText(rt, err))
	}
	if goja.IsUndefined(result) {
		return "", false, nil
	}
	return result.String(), true, nil
}

// jsValueToJSON converts a JS value into a decoded JSON value (any). The second
// return reports whether a value was produced (false means JSON.stringify
// yielded undefined). Mirrors codex's `v8_value_to_json`.
func jsValueToJSON(rt *goja.Runtime, value goja.Value) (any, bool, error) {
	stringified, ok, err := jsonStringify(rt, value)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(stringified), &decoded); err != nil {
		return nil, false, fmt.Errorf("failed to serialize JavaScript value: %w", err)
	}
	return decoded, true, nil
}

// jsonToJSValue converts a decoded JSON value into a JS value, going through
// JSON.parse so object identity matches what scripts expect. Mirrors codex's
// `json_to_v8`.
func jsonToJSValue(rt *goja.Runtime, value any) (goja.Value, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal stored value: %w", err)
	}
	jsonObj, ok := rt.Get("JSON").(*goja.Object)
	if !ok {
		return nil, fmt.Errorf("JSON global unavailable")
	}
	parse, ok := goja.AssertFunction(jsonObj.Get("parse"))
	if !ok {
		return nil, fmt.Errorf("JSON.parse unavailable")
	}
	parsed, err := parse(goja.Undefined(), rt.ToValue(string(encoded)))
	if err != nil {
		return nil, fmt.Errorf("%s", valueToErrorText(rt, err))
	}
	return parsed, nil
}

// valueToErrorText extracts a human-readable error string from a goja error,
// preferring the JS error's `.stack` property. Mirrors codex's
// `value_to_error_text`.
func valueToErrorText(rt *goja.Runtime, err error) string {
	exception, ok := err.(*goja.Exception)
	if !ok {
		return err.Error()
	}
	value := exception.Value()
	if value == nil {
		return exception.String()
	}
	if object, ok := value.(*goja.Object); ok {
		if stack := object.Get("stack"); stack != nil {
			if _, isUndefined := stack.(interface{ Export() any }); isUndefined {
				if !goja.IsUndefined(stack) && !goja.IsNull(stack) {
					if _, isString := stack.Export().(string); isString {
						return stack.String()
					}
				}
			}
		}
	}
	return value.String()
}

// normalizeOutputImage validates and normalizes an image() argument into an
// InputImage content item. Mirrors codex's `normalize_output_image`. The string
// error (not nil) signals a JS TypeError to throw.
func normalizeOutputImage(rt *goja.Runtime, value goja.Value, detailOverride *string) (FunctionCallOutputContentItem, error) {
	imageURL, detail, err := extractImageURLAndDetail(rt, value)
	if err != nil {
		return FunctionCallOutputContentItem{}, err
	}

	if imageURL == "" {
		return FunctionCallOutputContentItem{}, fmt.Errorf("%s", imageHelperExpectsMessage)
	}
	lower := strings.ToLower(imageURL)
	if !(strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:")) {
		return FunctionCallOutputContentItem{}, fmt.Errorf("image expects an http(s) or data URL")
	}

	effectiveDetail := detail
	if detailOverride != nil {
		effectiveDetail = detailOverride
	}
	var detailValue *ImageDetail
	if effectiveDetail != nil {
		normalized := strings.ToLower(*effectiveDetail)
		parsed, ok := ParseImageDetail(normalized)
		if !ok {
			return FunctionCallOutputContentItem{}, fmt.Errorf("image detail must be one of: auto, low, high, original")
		}
		detailValue = &parsed
	} else {
		def := DefaultImageDetail
		detailValue = &def
	}

	return NewImageItem(imageURL, detailValue), nil
}

func extractImageURLAndDetail(rt *goja.Runtime, value goja.Value) (string, *string, error) {
	if value == nil {
		return "", nil, fmt.Errorf("%s", imageHelperExpectsMessage)
	}
	if _, isString := value.Export().(string); isString && !goja.IsNull(value) && !goja.IsUndefined(value) {
		return value.String(), nil, nil
	}

	object, ok := value.(*goja.Object)
	if !ok || isArray(rt, object) {
		return "", nil, fmt.Errorf("%s", imageHelperExpectsMessage)
	}

	if url, detail, found, err := parseNonMCPOutputImage(rt, object); err != nil {
		return "", nil, err
	} else if found {
		return url, detail, nil
	}

	return parseMCPOutputImage(rt, value)
}

func isArray(rt *goja.Runtime, object *goja.Object) bool {
	arrayCtor, ok := rt.Get("Array").(*goja.Object)
	if !ok {
		return false
	}
	isArrayFn, ok := goja.AssertFunction(arrayCtor.Get("isArray"))
	if !ok {
		return false
	}
	result, err := isArrayFn(goja.Undefined(), object)
	if err != nil {
		return false
	}
	return result.ToBoolean()
}

func parseNonMCPOutputImage(rt *goja.Runtime, object *goja.Object) (string, *string, bool, error) {
	imageURL := object.Get("image_url")
	if imageURL == nil || goja.IsUndefined(imageURL) {
		return "", nil, false, nil
	}
	if _, isString := imageURL.Export().(string); !isString {
		return "", nil, false, fmt.Errorf("%s", imageHelperExpectsMessage)
	}
	detail, err := parseImageDetailValue(object.Get("detail"))
	if err != nil {
		return "", nil, false, err
	}
	return imageURL.String(), detail, true, nil
}

func parseImageDetailValue(value goja.Value) (*string, error) {
	if value == nil || goja.IsNull(value) || goja.IsUndefined(value) {
		return nil, nil
	}
	if _, isString := value.Export().(string); isString {
		str := value.String()
		return &str, nil
	}
	return nil, fmt.Errorf("image detail must be a string when provided")
}

func parseMCPOutputImage(rt *goja.Runtime, value goja.Value) (string, *string, error) {
	decoded, ok, err := jsValueToJSON(rt, value)
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, fmt.Errorf("%s", imageHelperExpectsMessage)
	}
	result, ok := decoded.(map[string]any)
	if !ok {
		return "", nil, fmt.Errorf("%s", imageHelperExpectsMessage)
	}
	itemType, ok := result["type"].(string)
	if !ok {
		return "", nil, fmt.Errorf("%s", imageHelperExpectsMessage)
	}
	if itemType != "image" {
		return "", nil, fmt.Errorf("image only accepts MCP image blocks, got \"%s\"", itemType)
	}
	data, ok := result["data"].(string)
	if !ok || data == "" {
		return "", nil, fmt.Errorf("image expected MCP image data")
	}

	var imageURL string
	if strings.HasPrefix(strings.ToLower(data), "data:") {
		imageURL = data
	} else {
		mimeType := "application/octet-stream"
		if mt, ok := result["mimeType"].(string); ok && mt != "" {
			mimeType = mt
		} else if mt, ok := result["mime_type"].(string); ok && mt != "" {
			mimeType = mt
		}
		imageURL = fmt.Sprintf("data:%s;base64,%s", mimeType, data)
	}

	var detail *string
	if meta, ok := result["_meta"].(map[string]any); ok {
		if rawDetail, ok := meta[codexImageDetailMetaKey].(string); ok {
			switch rawDetail {
			case "auto", "low", "high", "original":
				detail = &rawDetail
			}
		}
	}
	return imageURL, detail, nil
}
