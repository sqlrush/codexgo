package config

import (
	"github.com/sqlrush/codexgo/internal/features"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// ProfileTui holds TUI settings allowed inside a named profile.
// deny_unknown_fields.
type ProfileTui struct {
	SessionPickerView *SessionPickerViewMode `json:"session_picker_view" toml:"session_picker_view"`
}

// ConfigProfile is a named bundle of common config options. deny_unknown_fields.
// AbsolutePathBuf fields are represented as *string.
type ConfigProfile struct {
	Model                                *string                     `json:"model" toml:"model"`
	ServiceTier                          *string                     `json:"service_tier" toml:"service_tier"`
	ModelProvider                        *string                     `json:"model_provider" toml:"model_provider"`
	ApprovalPolicy                       *protocol.AskForApproval    `json:"approval_policy" toml:"approval_policy"`
	ApprovalsReviewer                    *protocol.ApprovalsReviewer `json:"approvals_reviewer" toml:"approvals_reviewer"`
	SandboxMode                          *protocol.SandboxMode       `json:"sandbox_mode" toml:"sandbox_mode"`
	ModelReasoningEffort                 *protocol.ReasoningEffort   `json:"model_reasoning_effort" toml:"model_reasoning_effort"`
	PlanModeReasoningEffort              *protocol.ReasoningEffort   `json:"plan_mode_reasoning_effort" toml:"plan_mode_reasoning_effort"`
	ModelReasoningSummary                *protocol.ReasoningSummary  `json:"model_reasoning_summary" toml:"model_reasoning_summary"`
	ModelVerbosity                       *protocol.Verbosity         `json:"model_verbosity" toml:"model_verbosity"`
	ModelCatalogJSON                     *string                     `json:"model_catalog_json" toml:"model_catalog_json"`
	Personality                          *protocol.Personality       `json:"personality" toml:"personality"`
	ChatgptBaseURL                       *string                     `json:"chatgpt_base_url" toml:"chatgpt_base_url"`
	ModelInstructionsFile                *string                     `json:"model_instructions_file" toml:"model_instructions_file"`
	JsReplNodePath                       *string                     `json:"js_repl_node_path" toml:"js_repl_node_path"`
	JsReplNodeModuleDirs                 *[]string                   `json:"js_repl_node_module_dirs" toml:"js_repl_node_module_dirs"`
	ExperimentalCompactPromptFile        *string                     `json:"experimental_compact_prompt_file" toml:"experimental_compact_prompt_file"`
	IncludePermissionsInstructions       *bool                       `json:"include_permissions_instructions" toml:"include_permissions_instructions"`
	IncludeAppsInstructions              *bool                       `json:"include_apps_instructions" toml:"include_apps_instructions"`
	IncludeCollaborationModeInstructions *bool                       `json:"include_collaboration_mode_instructions" toml:"include_collaboration_mode_instructions"`
	IncludeEnvironmentContext            *bool                       `json:"include_environment_context" toml:"include_environment_context"`
	ExperimentalUseUnifiedExecTool       *bool                       `json:"experimental_use_unified_exec_tool" toml:"experimental_use_unified_exec_tool"`
	Tools                                *ToolsToml                  `json:"tools" toml:"tools"`
	WebSearch                            *protocol.WebSearchMode     `json:"web_search" toml:"web_search"`
	Analytics                            *AnalyticsConfigToml        `json:"analytics" toml:"analytics"`
	Tui                                  *ProfileTui                 `json:"tui" toml:"tui"`
	Windows                              *WindowsToml                `json:"windows" toml:"windows"`
	Features                             *features.FeaturesToml      `json:"features" toml:"features"`
	OssProvider                          *string                     `json:"oss_provider" toml:"oss_provider"`
}
