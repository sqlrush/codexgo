package state

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
)

// userMessageBegin is the prefix codex injects ahead of a real user request.
// Mirrors the Rust `USER_MESSAGE_BEGIN` constant.
const userMessageBegin = "## My request for Codex:"

// imageOnlyUserMessagePlaceholder is the preview used when a user message has no
// text but does carry images.
const imageOnlyUserMessagePlaceholder = "[Image]"

// ApplyRolloutItem applies a single rollout item to metadata in place,
// mirroring the Rust `apply_rollout_item`. defaultProvider backfills the model
// provider when no provider has been observed yet.
func ApplyRolloutItem(metadata *ThreadMetadata, item *rollout.RolloutItem, defaultProvider string) {
	switch item.Kind {
	case rollout.RolloutItemKindSessionMeta:
		if item.SessionMeta != nil {
			applySessionMetaFromItem(metadata, item.SessionMeta)
		}
	case rollout.RolloutItemKindTurnContext:
		if item.TurnContext != nil {
			applyTurnContext(metadata, item.TurnContext)
		}
	case rollout.RolloutItemKindEventMsg:
		if item.EventMsg != nil {
			applyEventMsg(metadata, item.EventMsg)
		}
	case rollout.RolloutItemKindResponseItem:
		// Response items never affect mirrored metadata.
	case rollout.RolloutItemKindCompacted:
		// Compaction records never affect mirrored metadata.
	}
	if metadata.ModelProvider == "" {
		metadata.ModelProvider = defaultProvider
	}
}

// RolloutItemAffectsThreadMetadata reports whether item can mutate mirrored
// thread metadata, mirroring the Rust `rollout_item_affects_thread_metadata`.
func RolloutItemAffectsThreadMetadata(item *rollout.RolloutItem) bool {
	switch item.Kind {
	case rollout.RolloutItemKindSessionMeta, rollout.RolloutItemKindTurnContext:
		return true
	case rollout.RolloutItemKindEventMsg:
		if item.EventMsg == nil {
			return false
		}
		switch item.EventMsg.Type {
		case protocol.EventMsgKindTokenCount,
			protocol.EventMsgKindUserMessage,
			protocol.EventMsgKindThreadGoalUpdated:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func applySessionMetaFromItem(metadata *ThreadMetadata, metaLine *rollout.SessionMetaLine) {
	meta := metaLine.Meta
	if metadata.ID != meta.ID {
		// Ignore session_meta lines that don't match the canonical thread ID,
		// e.g., forked rollouts that embed the source session metadata.
		return
	}
	metadata.ID = meta.ID
	metadata.Source = enumToString(meta.Source)
	metadata.ThreadSource = cloneThreadSourcePtr(meta.ThreadSource)
	metadata.AgentNickname = cloneStringPtr(meta.AgentNickname)
	metadata.AgentRole = cloneStringPtr(meta.AgentRole)
	metadata.AgentPath = cloneStringPtr(meta.AgentPath)
	if meta.ModelProvider != nil {
		metadata.ModelProvider = *meta.ModelProvider
	}
	if meta.CliVersion != "" {
		metadata.CliVersion = meta.CliVersion
	}
	if meta.Cwd != "" {
		metadata.Cwd = meta.Cwd
	}
	if metaLine.Git != nil {
		git := metaLine.Git
		if git.CommitHash != nil {
			sha := git.CommitHash.String()
			metadata.GitSha = &sha
		} else {
			metadata.GitSha = nil
		}
		metadata.GitBranch = cloneStringPtr(git.Branch)
		metadata.GitOriginURL = cloneStringPtr(git.RepositoryURL)
	}
}

// turnContextFields are the TurnContext fields read by metadata extraction.
//
// The Go rollout TurnContextItem retains only cwd/turn_id plus the raw object,
// so the additional fields are parsed here from the retained raw JSON.
type turnContextFields struct {
	Cwd               string          `json:"cwd"`
	Model             string          `json:"model"`
	Effort            *string         `json:"effort"`
	ApprovalPolicy    json.RawMessage `json:"approval_policy"`
	SandboxPolicy     json.RawMessage `json:"sandbox_policy"`
	PermissionProfile json.RawMessage `json:"permission_profile"`
}

func applyTurnContext(metadata *ThreadMetadata, turnCtx *rollout.TurnContextItem) {
	var fields turnContextFields
	if len(turnCtx.Raw) > 0 {
		_ = json.Unmarshal(turnCtx.Raw, &fields)
	}
	if metadata.Cwd == "" {
		// Prefer the typed cwd, falling back to the raw-decoded value.
		if turnCtx.Cwd != "" {
			metadata.Cwd = turnCtx.Cwd
		} else if fields.Cwd != "" {
			metadata.Cwd = fields.Cwd
		}
	}
	if fields.Model != "" {
		model := fields.Model
		metadata.Model = &model
	}
	metadata.ReasoningEffort = parseReasoningEffort(fields.Effort)
	metadata.SandboxPolicy = turnContextSandboxPolicy(fields)
	metadata.ApprovalMode = approvalPolicyToString(fields.ApprovalPolicy)
}

// turnContextSandboxPolicy mirrors `serde_json::to_string(&turn_ctx.permission_profile())`.
//
// When the TurnContext carries an explicit permission_profile (the dominant
// case for modern rollouts) it is re-serialized verbatim. When absent, the Rust
// code derives a profile from the legacy sandbox policy; that derivation lives
// in sandbox-layer code outside this package, so as a documented fallback the
// raw sandbox_policy object is serialized instead.
func turnContextSandboxPolicy(fields turnContextFields) string {
	if len(fields.PermissionProfile) > 0 && !isJSONNull(fields.PermissionProfile) {
		return compactJSON(fields.PermissionProfile)
	}
	if len(fields.SandboxPolicy) > 0 && !isJSONNull(fields.SandboxPolicy) {
		return compactJSON(fields.SandboxPolicy)
	}
	return ""
}

func applyEventMsg(metadata *ThreadMetadata, event *protocol.EventMsg) {
	switch event.Type {
	case protocol.EventMsgKindTokenCount:
		if event.TokenCount != nil && event.TokenCount.Info != nil {
			total := event.TokenCount.Info.TotalTokenUsage.TotalTokens
			if total < 0 {
				total = 0
			}
			metadata.TokensUsed = total
		}
	case protocol.EventMsgKindUserMessage:
		if event.UserMessage == nil {
			return
		}
		preview := userMessagePreview(event.UserMessage)
		if metadata.FirstUserMessage == nil {
			metadata.FirstUserMessage = cloneStringPtr(preview)
		}
		setPreviewIfEmpty(metadata, preview)
		if metadata.Title == "" {
			title := stripUserMessagePrefix(event.UserMessage.Message)
			if title != "" {
				metadata.Title = title
			}
		}
	case protocol.EventMsgKindThreadGoalUpdated:
		if event.ThreadGoalUpdated == nil {
			return
		}
		objective := strings.TrimSpace(event.ThreadGoalUpdated.Goal.Objective)
		if objective != "" {
			setPreviewIfEmpty(metadata, &objective)
		}
	}
}

func setPreviewIfEmpty(metadata *ThreadMetadata, preview *string) {
	if metadata.Preview == nil {
		metadata.Preview = cloneStringPtr(preview)
	}
}

func stripUserMessagePrefix(text string) string {
	if idx := strings.Index(text, userMessageBegin); idx >= 0 {
		return strings.TrimSpace(text[idx+len(userMessageBegin):])
	}
	return strings.TrimSpace(text)
}

func userMessagePreview(user *protocol.UserMessageEvent) *string {
	message := stripUserMessagePrefix(user.Message)
	if message != "" {
		return &message
	}
	hasImages := user.Images != nil && len(*user.Images) > 0
	if hasImages || len(user.LocalImages) > 0 {
		placeholder := imageOnlyUserMessagePlaceholder
		return &placeholder
	}
	return nil
}

func parseReasoningEffort(value *string) *protocol.ReasoningEffort {
	if value == nil {
		return nil
	}
	switch protocol.ReasoningEffort(*value) {
	case protocol.ReasoningEffortNone,
		protocol.ReasoningEffortMinimal,
		protocol.ReasoningEffortLow,
		protocol.ReasoningEffortMedium,
		protocol.ReasoningEffortHigh,
		protocol.ReasoningEffortXHigh:
		effort := protocol.ReasoningEffort(*value)
		return &effort
	default:
		return nil
	}
}

func approvalPolicyToString(raw json.RawMessage) string {
	if len(raw) == 0 || isJSONNull(raw) {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
	}
	return compactJSON(raw)
}

func isJSONNull(raw json.RawMessage) bool {
	return string(raw) == "null"
}

func compactJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}
