package memories

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ToolName builds a namespaced tool identifier ("memories/<name>"), mirroring
// memory_tool_name / ToolName::namespaced.
func ToolName(name string) string {
	return MemoryToolsNamespace + "/" + name
}

// ToolCallResult is the parsed outcome of a memory tool call. ToModel holds the
// JSON the model receives. When Err is non-nil the call failed; Err.IsFatal
// distinguishes model-visible failures from fatal (IO) failures, mirroring
// backend_error_to_function_call.
type ToolCallResult struct {
	Output json.RawMessage
	Err    *BackendError
}

// clampMaxResults applies the requested-or-default value clamped to [1, max],
// mirroring clamp_max_results.
func clampMaxResults(requested *int, def, max int) int {
	value := def
	if requested != nil {
		value = *requested
	}
	if value < 1 {
		value = 1
	}
	if value > max {
		value = max
	}
	return value
}

// decodeArgs decodes tool arguments with deny_unknown_fields semantics, treating
// blank input as an empty object, mirroring parse_args. The returned error
// message mirrors serde's "unknown field" phrasing for unexpected keys.
func decodeArgs(arguments string, out any) error {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		trimmed = "{}"
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(trimmed)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return mapDecodeError(err)
	}
	return nil
}

// mapDecodeError rewrites encoding/json's "unknown field" error into serde-style
// phrasing so model-visible validation messages match the reference.
func mapDecodeError(err error) error {
	msg := err.Error()
	const prefix = "json: unknown field "
	if strings.HasPrefix(msg, prefix) {
		field := strings.Trim(strings.TrimPrefix(msg, prefix), "\"")
		return fmt.Errorf("unknown field `%s`", field)
	}
	return err
}

// listArgs mirrors the list tool ListArgs struct (deny_unknown_fields).
type listArgs struct {
	Path       *string `json:"path"`
	Cursor     *string `json:"cursor"`
	MaxResults *int    `json:"max_results"`
}

// readArgs mirrors the read tool ReadArgs struct (deny_unknown_fields).
type readArgs struct {
	Path       string `json:"path"`
	LineOffset *int   `json:"line_offset"`
	MaxLines   *int   `json:"max_lines"`
}

// searchArgs mirrors the search tool SearchArgs struct (deny_unknown_fields).
type searchArgs struct {
	Queries       []string         `json:"queries"`
	MatchMode     *SearchMatchMode `json:"match_mode"`
	Path          *string          `json:"path"`
	Cursor        *string          `json:"cursor"`
	ContextLines  *int             `json:"context_lines"`
	CaseSensitive *bool            `json:"case_sensitive"`
	Normalized    *bool            `json:"normalized"`
	MaxResults    *int             `json:"max_results"`
}

// addAdHocNoteArgs mirrors the add_ad_hoc_note tool AddAdHocNoteArgs struct.
type addAdHocNoteArgs struct {
	Filename string `json:"filename"`
	Note     string `json:"note"`
}

// CallList parses arguments and runs the list tool, mirroring ListTool::handle.
func CallList(ctx context.Context, backend Backend, arguments string) ToolCallResult {
	var args listArgs
	if err := decodeArgs(arguments, &args); err != nil {
		return respondToModel(err)
	}
	resp, err := backend.List(ctx, ListRequest{
		Path:       args.Path,
		Cursor:     args.Cursor,
		MaxResults: clampMaxResults(args.MaxResults, DefaultListMaxResults, MaxListResults),
	})
	return jsonResult(resp, err)
}

// CallRead parses arguments and runs the read tool, mirroring ReadTool::handle.
func CallRead(ctx context.Context, backend Backend, arguments string) ToolCallResult {
	var args readArgs
	if err := decodeArgs(arguments, &args); err != nil {
		return respondToModel(err)
	}
	lineOffset := 1
	if args.LineOffset != nil {
		lineOffset = *args.LineOffset
	}
	resp, err := backend.Read(ctx, ReadRequest{
		Path:       args.Path,
		LineOffset: lineOffset,
		MaxLines:   args.MaxLines,
		MaxTokens:  DefaultReadMaxTokens,
	})
	return jsonResult(resp, err)
}

// CallSearch parses arguments and runs the search tool, mirroring
// SearchTool::handle and SearchArgs::into_request.
func CallSearch(ctx context.Context, backend Backend, arguments string) ToolCallResult {
	var args searchArgs
	if err := decodeArgs(arguments, &args); err != nil {
		return respondToModel(err)
	}
	matchMode := AnyMode()
	if args.MatchMode != nil {
		matchMode = *args.MatchMode
	}
	contextLines := 0
	if args.ContextLines != nil {
		contextLines = *args.ContextLines
	}
	caseSensitive := true
	if args.CaseSensitive != nil {
		caseSensitive = *args.CaseSensitive
	}
	normalized := false
	if args.Normalized != nil {
		normalized = *args.Normalized
	}
	resp, err := backend.Search(ctx, SearchRequest{
		Queries:       args.Queries,
		MatchMode:     matchMode,
		Path:          args.Path,
		Cursor:        args.Cursor,
		ContextLines:  contextLines,
		CaseSensitive: caseSensitive,
		Normalized:    normalized,
		MaxResults:    clampMaxResults(args.MaxResults, DefaultSearchMaxResults, MaxSearchResults),
	})
	return jsonResult(resp, err)
}

// CallAddAdHocNote parses arguments and runs the add_ad_hoc_note tool, mirroring
// AddAdHocNoteTool::handle.
func CallAddAdHocNote(ctx context.Context, backend Backend, arguments string) ToolCallResult {
	var args addAdHocNoteArgs
	if err := decodeArgs(arguments, &args); err != nil {
		return respondToModel(err)
	}
	resp, err := backend.AddAdHocNote(ctx, AddAdHocNoteRequest{
		Filename: args.Filename,
		Note:     args.Note,
	})
	return jsonResult(resp, err)
}

// jsonResult marshals a successful response or maps a backend error into a
// ToolCallResult.
func jsonResult(resp any, err error) ToolCallResult {
	if err != nil {
		var backendErr *BackendError
		if be, ok := err.(*BackendError); ok {
			backendErr = be
		} else {
			backendErr = errIO(err)
		}
		return ToolCallResult{Err: backendErr}
	}
	encoded, marshalErr := json.Marshal(resp)
	if marshalErr != nil {
		return ToolCallResult{Err: errIO(marshalErr)}
	}
	return ToolCallResult{Output: encoded}
}

// respondToModel wraps an argument-validation error as a non-fatal,
// model-visible failure (parse_args errors map to RespondToModel).
func respondToModel(err error) ToolCallResult {
	return ToolCallResult{Err: &BackendError{Kind: ErrArgsParse, modelMessage: err.Error(), err: err}}
}
