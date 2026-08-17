package localexec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/internal/applypatch"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// ----------------------------------------------------------------------------
// apply_patch
// ----------------------------------------------------------------------------

type applyPatchExecutor struct {
	fs applypatch.FileSystem
}

func (applyPatchExecutor) Name() protocol.ToolName { return protocol.PlainToolName("apply_patch") }

func (applyPatchExecutor) Spec(*core.TurnContext) (tools.ToolSpec, bool) {
	// codex advertises apply_patch as a FREEFORM (custom) grammar tool, not a
	// function (create_apply_patch_freeform_tool). The handler already accepts the
	// custom raw-text payload via extractPatch.
	return tools.CreateApplyPatchFreeformTool(), true
}

func (applyPatchExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction || p.Kind == tools.ToolPayloadKindCustom
}

// applyPatchArgs is the apply_patch argument shape: the patch text under "input".
type applyPatchArgs struct {
	Input string `json:"input"`
}

func (a applyPatchExecutor) Handle(_ context.Context, h *core.ToolHandlerContext) (tools.ToolOutput, error) {
	patch, err := a.extractPatch(h.Payload)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(patch) == "" {
		return nil, tools.RespondToModelError("apply_patch requires a non-empty patch")
	}

	cwd, cerr := abspath.FromAbsolutePath(h.Turn.Cwd)
	if cerr != nil {
		return nil, tools.RespondToModelError(fmt.Sprintf("invalid cwd for apply_patch: %v", cerr))
	}
	fs := a.fs
	if fs == nil {
		fs = applypatch.OSFileSystem{}
	}

	var stdout, stderr bytes.Buffer
	_, applyErr := applypatch.ApplyPatch(patch, cwd, &stdout, &stderr, fs)
	if applyErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = applyErr.Error()
		}
		// STUB: approval escalation on sandbox-denied writes is deferred; a
		// failed apply is surfaced to the model verbatim.
		return nil, tools.RespondToModelError(msg)
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		out = "Patch applied."
	}
	return applyPatchToolOutput{text: out}, nil
}

// extractPatch resolves the patch text from a Function (JSON {"input": ...}) or
// Custom (raw input) payload.
func (applyPatchExecutor) extractPatch(p tools.ToolPayload) (string, error) {
	switch p.Kind {
	case tools.ToolPayloadKindCustom:
		return p.Input, nil
	case tools.ToolPayloadKindFunction:
		var parsed applyPatchArgs
		if err := json.Unmarshal([]byte(p.Arguments), &parsed); err != nil {
			// Tolerate a bare-string argument payload.
			var bare string
			if jerr := json.Unmarshal([]byte(p.Arguments), &bare); jerr == nil {
				return bare, nil
			}
			return "", tools.RespondToModelError(fmt.Sprintf("failed to parse apply_patch arguments: %v", err))
		}
		return parsed.Input, nil
	default:
		return "", tools.FatalError("apply_patch invoked with incompatible payload")
	}
}

// applyPatchToolOutput is the Go analogue of the Rust `ApplyPatchToolOutput`.
type applyPatchToolOutput struct {
	tools.DefaultToolOutput
	text string
}

func (o applyPatchToolOutput) LogPreview() string      { return core.TelemetryPreview(o.text) }
func (o applyPatchToolOutput) SuccessForLogging() bool { return true }

func (o applyPatchToolOutput) ToResponseItem(callID string, payload tools.ToolPayload) tools.ResponseInputItem {
	out := protocol.FunctionCallOutputPayload{Text: &o.text, Success: boolPtr(true)}
	if payload.Kind == tools.ToolPayloadKindCustom {
		return tools.CustomToolCallOutputInput(callID, nil, out)
	}
	return tools.FunctionCallOutputInput(callID, out)
}

func (o applyPatchToolOutput) PostToolUseResponse(string, tools.ToolPayload) (json.RawMessage, bool) {
	return mustJSON(o.text), true
}

func (o applyPatchToolOutput) CodeModeResult(tools.ToolPayload) json.RawMessage {
	return mustJSON(map[string]any{})
}
