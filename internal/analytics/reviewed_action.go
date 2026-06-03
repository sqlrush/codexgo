package analytics

import (
	"encoding/json"
	"fmt"
)

// MarshalJSON serializes a [GuardianReviewedAction] as an internally-tagged
// enum with a "type" discriminator and only the fields for the active variant.
// Mirrors the Rust serde representation (tag = "type", rename_all = "snake_case").
func (a GuardianReviewedAction) MarshalJSON() ([]byte, error) {
	m := map[string]interface{}{"type": string(a.Kind)}
	switch a.Kind {
	case GuardianReviewedActionShell:
		m["sandbox_permissions"] = a.SandboxPermissions
		m["additional_permissions"] = a.AdditionalPermissions
	case GuardianReviewedActionUnifiedExec:
		m["sandbox_permissions"] = a.SandboxPermissions
		m["additional_permissions"] = a.AdditionalPermissions
		m["tty"] = a.TTY
	case GuardianReviewedActionExecve:
		m["source"] = a.Source
		m["program"] = a.Program
		m["additional_permissions"] = a.AdditionalPermissions
	case GuardianReviewedActionApplyPatch:
		// no extra fields
	case GuardianReviewedActionNetworkAccess:
		m["protocol"] = a.Protocol
		m["port"] = a.Port
	case GuardianReviewedActionMcpToolCall:
		m["server"] = a.Server
		m["tool_name"] = a.ToolName
		m["connector_id"] = a.ConnectorID
		m["connector_name"] = a.ConnectorName
		m["tool_title"] = a.ToolTitle
	case GuardianReviewedActionRequestPermissions:
		// no extra fields
	default:
		return nil, fmt.Errorf("analytics: unknown GuardianReviewedAction kind %q", a.Kind)
	}
	return json.Marshal(m)
}

// UnmarshalJSON deserializes an internally-tagged [GuardianReviewedAction].
func (a *GuardianReviewedAction) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("analytics: decode GuardianReviewedAction tag: %w", err)
	}
	a.Kind = GuardianReviewedActionKind(probe.Type)

	switch a.Kind {
	case GuardianReviewedActionShell:
		var v struct {
			SandboxPermissions    json.RawMessage `json:"sandbox_permissions"`
			AdditionalPermissions json.RawMessage `json:"additional_permissions"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("analytics: decode shell action: %w", err)
		}
		a.SandboxPermissions = rawOrNil(v.SandboxPermissions)
		a.AdditionalPermissions = rawOrNil(v.AdditionalPermissions)
	case GuardianReviewedActionUnifiedExec:
		var v struct {
			SandboxPermissions    json.RawMessage `json:"sandbox_permissions"`
			AdditionalPermissions json.RawMessage `json:"additional_permissions"`
			TTY                   bool            `json:"tty"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("analytics: decode unified_exec action: %w", err)
		}
		a.SandboxPermissions = rawOrNil(v.SandboxPermissions)
		a.AdditionalPermissions = rawOrNil(v.AdditionalPermissions)
		a.TTY = v.TTY
	case GuardianReviewedActionExecve:
		var v struct {
			Source                json.RawMessage `json:"source"`
			Program               string          `json:"program"`
			AdditionalPermissions json.RawMessage `json:"additional_permissions"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("analytics: decode execve action: %w", err)
		}
		a.Source = rawOrNil(v.Source)
		a.Program = v.Program
		a.AdditionalPermissions = rawOrNil(v.AdditionalPermissions)
	case GuardianReviewedActionApplyPatch, GuardianReviewedActionRequestPermissions:
		// no extra fields
	case GuardianReviewedActionNetworkAccess:
		var v struct {
			Protocol json.RawMessage `json:"protocol"`
			Port     uint16          `json:"port"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("analytics: decode network_access action: %w", err)
		}
		a.Protocol = rawOrNil(v.Protocol)
		a.Port = v.Port
	case GuardianReviewedActionMcpToolCall:
		var v struct {
			Server        string  `json:"server"`
			ToolName      string  `json:"tool_name"`
			ConnectorID   *string `json:"connector_id"`
			ConnectorName *string `json:"connector_name"`
			ToolTitle     *string `json:"tool_title"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("analytics: decode mcp_tool_call action: %w", err)
		}
		a.Server = v.Server
		a.ToolName = v.ToolName
		a.ConnectorID = v.ConnectorID
		a.ConnectorName = v.ConnectorName
		a.ToolTitle = v.ToolTitle
	default:
		return fmt.Errorf("analytics: unknown GuardianReviewedAction kind %q", a.Kind)
	}
	return nil
}

func rawOrNil(raw json.RawMessage) interface{} {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}
