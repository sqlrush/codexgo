package engine

import (
	"encoding/json"
	"fmt"
)

// MarshalJSON renders the externally-tagged JSON shape that codex's
// `#[derive(Serialize)]` produces for `RuntimeResponse`. Each variant is wrapped
// in an object keyed by its PascalCase variant name, e.g.
//
//	{"Result":{"cell_id":"1","content_items":[],"error_text":null}}
//
// content_items is always present (an empty array, never null) and error_text is
// emitted as null when absent, matching serde's default Option<String> handling
// (the field has no skip_serializing_if attribute).
func (r RuntimeResponse) MarshalJSON() ([]byte, error) {
	items := r.ContentItems
	if items == nil {
		items = []FunctionCallOutputContentItem{}
	}
	switch r.Kind {
	case RuntimeResponseYielded:
		return json.Marshal(map[string]any{
			"Yielded": struct {
				CellID       CellID                          `json:"cell_id"`
				ContentItems []FunctionCallOutputContentItem `json:"content_items"`
			}{CellID: r.CellID, ContentItems: items},
		})
	case RuntimeResponseTerminated:
		return json.Marshal(map[string]any{
			"Terminated": struct {
				CellID       CellID                          `json:"cell_id"`
				ContentItems []FunctionCallOutputContentItem `json:"content_items"`
			}{CellID: r.CellID, ContentItems: items},
		})
	case RuntimeResponseResult:
		return json.Marshal(map[string]any{
			"Result": struct {
				CellID       CellID                          `json:"cell_id"`
				ContentItems []FunctionCallOutputContentItem `json:"content_items"`
				ErrorText    *string                         `json:"error_text"`
			}{CellID: r.CellID, ContentItems: items, ErrorText: r.ErrorText},
		})
	default:
		return nil, fmt.Errorf("unknown runtime response kind %d", r.Kind)
	}
}

// newResultResponse builds a Result RuntimeResponse, mirroring the construction
// used throughout codex's service layer.
func newResultResponse(cellID CellID, items []FunctionCallOutputContentItem, errorText *string) RuntimeResponse {
	return RuntimeResponse{
		Kind:         RuntimeResponseResult,
		CellID:       cellID,
		ContentItems: items,
		ErrorText:    errorText,
	}
}

// newYieldedResponse builds a Yielded RuntimeResponse.
func newYieldedResponse(cellID CellID, items []FunctionCallOutputContentItem) RuntimeResponse {
	return RuntimeResponse{
		Kind:         RuntimeResponseYielded,
		CellID:       cellID,
		ContentItems: items,
	}
}

// newTerminatedResponse builds a Terminated RuntimeResponse.
func newTerminatedResponse(cellID CellID, items []FunctionCallOutputContentItem) RuntimeResponse {
	return RuntimeResponse{
		Kind:         RuntimeResponseTerminated,
		CellID:       cellID,
		ContentItems: items,
	}
}
