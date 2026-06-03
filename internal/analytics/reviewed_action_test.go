package analytics

import (
	"encoding/json"
	"testing"
)

func TestGuardianReviewedActionRoundTrip(t *testing.T) {
	t.Parallel()

	connector := "connector-1"
	tests := []struct {
		name   string
		action GuardianReviewedAction
		want   map[string]interface{}
	}{
		{
			name:   "apply_patch_has_only_type",
			action: GuardianReviewedAction{Kind: GuardianReviewedActionApplyPatch},
			want:   map[string]interface{}{"type": "apply_patch"},
		},
		{
			name:   "request_permissions_has_only_type",
			action: GuardianReviewedAction{Kind: GuardianReviewedActionRequestPermissions},
			want:   map[string]interface{}{"type": "request_permissions"},
		},
		{
			name: "network_access_includes_port",
			action: GuardianReviewedAction{
				Kind:     GuardianReviewedActionNetworkAccess,
				Protocol: json.RawMessage(`"https"`),
				Port:     443,
			},
			want: map[string]interface{}{
				"type":     "network_access",
				"protocol": "https",
				"port":     float64(443),
			},
		},
		{
			name: "mcp_tool_call_includes_fields",
			action: GuardianReviewedAction{
				Kind:        GuardianReviewedActionMcpToolCall,
				Server:      "srv",
				ToolName:    "do_thing",
				ConnectorID: &connector,
			},
			want: map[string]interface{}{
				"type":           "mcp_tool_call",
				"server":         "srv",
				"tool_name":      "do_thing",
				"connector_id":   "connector-1",
				"connector_name": nil,
				"tool_title":     nil,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(tt.action)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got map[string]interface{}
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal to map: %v", err)
			}
			if got["type"] != tt.want["type"] {
				t.Errorf("type: got %v want %v", got["type"], tt.want["type"])
			}
			for k, want := range tt.want {
				if got[k] != want {
					t.Errorf("field %q: got %#v want %#v", k, got[k], want)
				}
			}

			// Round-trip back to a value and confirm the discriminant survives.
			var back GuardianReviewedAction
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("unmarshal to value: %v", err)
			}
			if back.Kind != tt.action.Kind {
				t.Errorf("kind: got %q want %q", back.Kind, tt.action.Kind)
			}
		})
	}
}
