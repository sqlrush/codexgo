package hooks

import (
	"encoding/json"
	"fmt"
	"strings"
)

// trimmedReason returns the trimmed reason or nil when empty. Mirrors
// trimmed_reason.
func trimmedReason(reason string) *string {
	t := strings.TrimSpace(reason)
	if t == "" {
		return nil
	}
	return &t
}

func invalidBlockMessage(eventName string) string {
	return fmt.Sprintf("%s hook returned decision:block without a non-empty reason", eventName)
}

// --- session start / subagent start ---

func parseSessionStart(stdout string) (*sessionStartOutput, bool) {
	return parseStartOutput(stdout, "SessionStart")
}

func parseSubagentStart(stdout string) (*sessionStartOutput, bool) {
	return parseStartOutput(stdout, "SubagentStart")
}

func parseStartOutput(stdout, hookEventName string) (*sessionStartOutput, bool) {
	m, ok := objectFields(stdout)
	if !ok {
		return nil, false
	}
	if rejectUnknown(m, universalKeys("hookSpecificOutput")...) {
		return nil, false
	}
	universal, err := parseUniversalWire(m)
	if err != nil {
		return nil, false
	}
	additional, ok := parseHookSpecificAdditionalContext(m, hookEventName)
	if !ok {
		return nil, false
	}
	return &sessionStartOutput{universal: universal, additionalContext: additional}, true
}

// parseHookSpecificAdditionalContext decodes hookSpecificOutput with fields
// {hookEventName, additionalContext}. Returns (additionalContext, ok).
func parseHookSpecificAdditionalContext(m map[string]json.RawMessage, expectedEvent string) (*string, bool) {
	raw, ok := optRawField(m, "hookSpecificOutput")
	if !ok {
		return nil, true
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(raw, &inner); err != nil {
		return nil, false
	}
	if rejectUnknown(inner, "hookEventName", "additionalContext") {
		return nil, false
	}
	if !validHookEventName(inner) {
		return nil, false
	}
	additional, ok := optStringField(inner, "additionalContext")
	if !ok {
		return nil, false
	}
	return additional, true
}

// validHookEventName checks the hookEventName field decodes as one of the known
// event-name strings. Mirrors HookEventNameWire (a string enum).
func validHookEventName(m map[string]json.RawMessage) bool {
	raw, ok := m["hookEventName"]
	if !ok {
		// hook_event_name has no default and is required by the wire structs.
		return false
	}
	var name string
	if err := json.Unmarshal(raw, &name); err != nil {
		return false
	}
	for _, n := range HookEventNames {
		if n == name {
			return true
		}
	}
	return false
}

func universalKeys(extra ...string) []string {
	return append([]string{"continue", "stopReason", "suppressOutput", "systemMessage"}, extra...)
}

// --- pre tool use ---

func parsePreToolUse(stdout string) (*preToolUseOutput, bool) {
	m, ok := objectFields(stdout)
	if !ok {
		return nil, false
	}
	if rejectUnknown(m, universalKeys("decision", "reason", "hookSpecificOutput")...) {
		return nil, false
	}
	universal, err := parseUniversalWire(m)
	if err != nil {
		return nil, false
	}
	decision, ok := optStringField(m, "decision")
	if !ok {
		return nil, false
	}
	if decision != nil && *decision != "approve" && *decision != "block" {
		return nil, false
	}
	reason, ok := optStringField(m, "reason")
	if !ok {
		return nil, false
	}
	hso, ok := parsePreToolUseHookSpecific(m)
	if !ok {
		return nil, false
	}

	additionalContext := (*string)(nil)
	if hso != nil {
		additionalContext = hso.additionalContext
	}
	useHookSpecific := hso != nil &&
		(hso.permissionDecision != nil || hso.permissionDecisionReason != nil || hso.updatedInput != nil)

	var invalidReason *string
	if r := unsupportedPreToolUseUniversal(universal); r != nil {
		invalidReason = r
	} else if useHookSpecific {
		invalidReason = unsupportedPreToolUseHookSpecific(hso)
	} else {
		invalidReason = unsupportedPreToolUseLegacyDecision(decision, reason)
	}

	var blockReason *string
	if invalidReason == nil {
		if useHookSpecific {
			if hso.permissionDecision != nil && *hso.permissionDecision == "deny" {
				if hso.permissionDecisionReason != nil {
					blockReason = trimmedReason(*hso.permissionDecisionReason)
				}
			}
		} else if decision != nil && *decision == "block" {
			if reason != nil {
				blockReason = trimmedReason(*reason)
			}
		}
	}

	var updatedInput json.RawMessage
	if invalidReason == nil && hso != nil &&
		hso.permissionDecision != nil && *hso.permissionDecision == "allow" {
		updatedInput = hso.updatedInput
	}

	return &preToolUseOutput{
		universal:         universal,
		blockReason:       blockReason,
		additionalContext: additionalContext,
		updatedInput:      updatedInput,
		invalidReason:     invalidReason,
	}, true
}

type preToolUseHookSpecific struct {
	permissionDecision       *string
	permissionDecisionReason *string
	updatedInput             json.RawMessage
	additionalContext        *string
}

func parsePreToolUseHookSpecific(m map[string]json.RawMessage) (*preToolUseHookSpecific, bool) {
	raw, ok := optRawField(m, "hookSpecificOutput")
	if !ok {
		return nil, true
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(raw, &inner); err != nil {
		return nil, false
	}
	if rejectUnknown(inner, "hookEventName", "permissionDecision", "permissionDecisionReason", "updatedInput", "additionalContext") {
		return nil, false
	}
	if !validHookEventName(inner) {
		return nil, false
	}
	pd, ok := optStringField(inner, "permissionDecision")
	if !ok {
		return nil, false
	}
	if pd != nil && *pd != "allow" && *pd != "deny" && *pd != "ask" {
		return nil, false
	}
	pdr, ok := optStringField(inner, "permissionDecisionReason")
	if !ok {
		return nil, false
	}
	ac, ok := optStringField(inner, "additionalContext")
	if !ok {
		return nil, false
	}
	ui, _ := optRawField(inner, "updatedInput")
	return &preToolUseHookSpecific{
		permissionDecision:       pd,
		permissionDecisionReason: pdr,
		updatedInput:             ui,
		additionalContext:        ac,
	}, true
}

func unsupportedPreToolUseUniversal(u universalOutput) *string {
	switch {
	case !u.ContinueProcessing:
		return strPtr("PreToolUse hook returned unsupported continue:false")
	case u.StopReason != nil:
		return strPtr("PreToolUse hook returned unsupported stopReason")
	case u.SuppressOutput:
		return strPtr("PreToolUse hook returned unsupported suppressOutput")
	default:
		return nil
	}
}

func unsupportedPreToolUseHookSpecific(o *preToolUseHookSpecific) *string {
	if o.updatedInput != nil && !(o.permissionDecision != nil && *o.permissionDecision == "allow") {
		return strPtr("PreToolUse hook returned updatedInput without permissionDecision:allow")
	}
	if o.permissionDecision == nil {
		if o.permissionDecisionReason != nil {
			return strPtr("PreToolUse hook returned permissionDecisionReason without permissionDecision")
		}
		return nil
	}
	switch *o.permissionDecision {
	case "allow":
		if o.updatedInput == nil {
			return strPtr("PreToolUse hook returned unsupported permissionDecision:allow")
		}
		return nil
	case "ask":
		return strPtr("PreToolUse hook returned unsupported permissionDecision:ask")
	case "deny":
		if o.permissionDecisionReason == nil || trimmedReason(*o.permissionDecisionReason) == nil {
			return strPtr("PreToolUse hook returned permissionDecision:deny without a non-empty permissionDecisionReason")
		}
		return nil
	default:
		return nil
	}
}

func unsupportedPreToolUseLegacyDecision(decision *string, reason *string) *string {
	if decision == nil {
		if reason != nil {
			return strPtr("PreToolUse hook returned reason without decision")
		}
		return nil
	}
	switch *decision {
	case "approve":
		return strPtr("PreToolUse hook returned unsupported decision:approve")
	case "block":
		if reason == nil || trimmedReason(*reason) == nil {
			return strPtr(invalidBlockMessage("PreToolUse"))
		}
		return nil
	default:
		return nil
	}
}

// --- permission request ---

func parsePermissionRequest(stdout string) (*permissionRequestOutput, bool) {
	m, ok := objectFields(stdout)
	if !ok {
		return nil, false
	}
	if rejectUnknown(m, universalKeys("hookSpecificOutput")...) {
		return nil, false
	}
	universal, err := parseUniversalWire(m)
	if err != nil {
		return nil, false
	}
	decisionWire, ok := parsePermissionRequestDecisionWire(m)
	if !ok {
		return nil, false
	}

	var invalidReason *string
	if r := unsupportedPermissionRequestUniversal(universal); r != nil {
		invalidReason = r
	} else if decisionWire != nil {
		invalidReason = unsupportedPermissionRequestHookSpecific(decisionWire)
	}

	var decision *parsedPermissionRequestDecision
	if invalidReason == nil && decisionWire != nil {
		d := permissionRequestDecision(decisionWire)
		decision = &d
	}

	return &permissionRequestOutput{
		universal:     universal,
		decision:      decision,
		invalidReason: invalidReason,
	}, true
}

type permissionRequestDecisionWire struct {
	behavior           string
	updatedInput       json.RawMessage
	updatedPermissions json.RawMessage
	message            *string
	interrupt          bool
}

func parsePermissionRequestDecisionWire(m map[string]json.RawMessage) (*permissionRequestDecisionWire, bool) {
	raw, ok := optRawField(m, "hookSpecificOutput")
	if !ok {
		return nil, true
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(raw, &inner); err != nil {
		return nil, false
	}
	if rejectUnknown(inner, "hookEventName", "decision") {
		return nil, false
	}
	if !validHookEventName(inner) {
		return nil, false
	}
	decisionRaw, ok := optRawField(inner, "decision")
	if !ok {
		return nil, true
	}
	var dm map[string]json.RawMessage
	if err := json.Unmarshal(decisionRaw, &dm); err != nil {
		return nil, false
	}
	if rejectUnknown(dm, "behavior", "updatedInput", "updatedPermissions", "message", "interrupt") {
		return nil, false
	}
	behaviorRaw, ok := dm["behavior"]
	if !ok {
		return nil, false
	}
	var behavior string
	if err := json.Unmarshal(behaviorRaw, &behavior); err != nil {
		return nil, false
	}
	if behavior != "allow" && behavior != "deny" {
		return nil, false
	}
	message, ok := optStringField(dm, "message")
	if !ok {
		return nil, false
	}
	interrupt := false
	if raw, present := dm["interrupt"]; present {
		if err := json.Unmarshal(raw, &interrupt); err != nil {
			return nil, false
		}
	}
	ui, _ := optRawField(dm, "updatedInput")
	up, _ := optRawField(dm, "updatedPermissions")
	return &permissionRequestDecisionWire{
		behavior:           behavior,
		updatedInput:       ui,
		updatedPermissions: up,
		message:            message,
		interrupt:          interrupt,
	}, true
}

func unsupportedPermissionRequestUniversal(u universalOutput) *string {
	switch {
	case !u.ContinueProcessing:
		return strPtr("PermissionRequest hook returned unsupported continue:false")
	case u.StopReason != nil:
		return strPtr("PermissionRequest hook returned unsupported stopReason")
	case u.SuppressOutput:
		return strPtr("PermissionRequest hook returned unsupported suppressOutput")
	default:
		return nil
	}
}

func unsupportedPermissionRequestHookSpecific(d *permissionRequestDecisionWire) *string {
	switch {
	case d.updatedInput != nil:
		return strPtr("PermissionRequest hook returned unsupported updatedInput")
	case d.updatedPermissions != nil:
		return strPtr("PermissionRequest hook returned unsupported updatedPermissions")
	case d.interrupt:
		return strPtr("PermissionRequest hook returned unsupported interrupt:true")
	default:
		return nil
	}
}

func permissionRequestDecision(d *permissionRequestDecisionWire) parsedPermissionRequestDecision {
	if d.behavior == "allow" {
		return parsedPermissionRequestDecision{kind: permissionRequestDecisionAllow}
	}
	msg := "PermissionRequest hook denied approval"
	if d.message != nil {
		if t := trimmedReason(*d.message); t != nil {
			msg = *t
		}
	}
	return parsedPermissionRequestDecision{kind: permissionRequestDecisionDeny, message: msg}
}

// --- post tool use ---

func parsePostToolUse(stdout string) (*postToolUseOutput, bool) {
	m, ok := objectFields(stdout)
	if !ok {
		return nil, false
	}
	if rejectUnknown(m, universalKeys("decision", "reason", "hookSpecificOutput")...) {
		return nil, false
	}
	universal, err := parseUniversalWire(m)
	if err != nil {
		return nil, false
	}
	decision, ok := optStringField(m, "decision")
	if !ok {
		return nil, false
	}
	if decision != nil && *decision != "block" {
		return nil, false
	}
	reason, ok := optStringField(m, "reason")
	if !ok {
		return nil, false
	}
	hso, ok := parsePostToolUseHookSpecific(m)
	if !ok {
		return nil, false
	}

	var invalidReason *string
	if r := unsupportedPostToolUseUniversal(universal); r != nil {
		invalidReason = r
	} else if hso != nil {
		invalidReason = unsupportedPostToolUseHookSpecific(hso)
	}

	shouldBlock := decision != nil && *decision == "block"
	var invalidBlockReason *string
	if shouldBlock && (reason == nil || strings.TrimSpace(deref(reason)) == "") {
		invalidBlockReason = strPtr(invalidBlockMessage("PostToolUse"))
	} else if !shouldBlock && universal.ContinueProcessing && reason != nil {
		invalidBlockReason = strPtr("PostToolUse hook returned reason without decision")
	}

	var additionalContext *string
	if hso != nil {
		additionalContext = hso.additionalContext
	}

	return &postToolUseOutput{
		universal:          universal,
		shouldBlock:        shouldBlock && invalidReason == nil && invalidBlockReason == nil,
		reason:             reason,
		invalidBlockReason: invalidBlockReason,
		additionalContext:  additionalContext,
		invalidReason:      invalidReason,
	}, true
}

type postToolUseHookSpecific struct {
	additionalContext    *string
	updatedMCPToolOutput json.RawMessage
}

func parsePostToolUseHookSpecific(m map[string]json.RawMessage) (*postToolUseHookSpecific, bool) {
	raw, ok := optRawField(m, "hookSpecificOutput")
	if !ok {
		return nil, true
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(raw, &inner); err != nil {
		return nil, false
	}
	if rejectUnknown(inner, "hookEventName", "additionalContext", "updatedMCPToolOutput") {
		return nil, false
	}
	if !validHookEventName(inner) {
		return nil, false
	}
	ac, ok := optStringField(inner, "additionalContext")
	if !ok {
		return nil, false
	}
	out, _ := optRawField(inner, "updatedMCPToolOutput")
	return &postToolUseHookSpecific{additionalContext: ac, updatedMCPToolOutput: out}, true
}

func unsupportedPostToolUseUniversal(u universalOutput) *string {
	if u.SuppressOutput {
		return strPtr("PostToolUse hook returned unsupported suppressOutput")
	}
	return nil
}

func unsupportedPostToolUseHookSpecific(o *postToolUseHookSpecific) *string {
	if o.updatedMCPToolOutput != nil {
		return strPtr("PostToolUse hook returned unsupported updatedMCPToolOutput")
	}
	return nil
}

// --- user prompt submit ---

func parseUserPromptSubmit(stdout string) (*userPromptSubmitOutput, bool) {
	m, ok := objectFields(stdout)
	if !ok {
		return nil, false
	}
	if rejectUnknown(m, universalKeys("decision", "reason", "hookSpecificOutput")...) {
		return nil, false
	}
	universal, err := parseUniversalWire(m)
	if err != nil {
		return nil, false
	}
	decision, ok := optStringField(m, "decision")
	if !ok {
		return nil, false
	}
	if decision != nil && *decision != "block" {
		return nil, false
	}
	reason, ok := optStringField(m, "reason")
	if !ok {
		return nil, false
	}
	additionalContext, ok := parseHookSpecificAdditionalContext(m, "UserPromptSubmit")
	if !ok {
		return nil, false
	}

	shouldBlock := decision != nil && *decision == "block"
	var invalidBlockReason *string
	if shouldBlock && (reason == nil || strings.TrimSpace(deref(reason)) == "") {
		invalidBlockReason = strPtr(invalidBlockMessage("UserPromptSubmit"))
	}

	return &userPromptSubmitOutput{
		universal:          universal,
		shouldBlock:        shouldBlock && invalidBlockReason == nil,
		reason:             reason,
		invalidBlockReason: invalidBlockReason,
		additionalContext:  additionalContext,
	}, true
}

// --- stop / subagent stop ---

func parseStop(stdout string) (*stopOutput, bool) {
	return parseStopLike(stdout, "Stop")
}

func parseSubagentStop(stdout string) (*stopOutput, bool) {
	return parseStopLike(stdout, "SubagentStop")
}

func parseStopLike(stdout, eventName string) (*stopOutput, bool) {
	m, ok := objectFields(stdout)
	if !ok {
		return nil, false
	}
	if rejectUnknown(m, universalKeys("decision", "reason")...) {
		return nil, false
	}
	universal, err := parseUniversalWire(m)
	if err != nil {
		return nil, false
	}
	decision, ok := optStringField(m, "decision")
	if !ok {
		return nil, false
	}
	if decision != nil && *decision != "block" {
		return nil, false
	}
	reason, ok := optStringField(m, "reason")
	if !ok {
		return nil, false
	}

	shouldBlock := decision != nil && *decision == "block"
	var invalidBlockReason *string
	if shouldBlock && (reason == nil || strings.TrimSpace(deref(reason)) == "") {
		invalidBlockReason = strPtr(invalidBlockMessage(eventName))
	}

	return &stopOutput{
		universal:          universal,
		shouldBlock:        shouldBlock && invalidBlockReason == nil,
		reason:             reason,
		invalidBlockReason: invalidBlockReason,
	}, true
}

// --- compact ---

func parsePreCompact(stdout string) (*preCompactOutput, bool) {
	m, ok := objectFields(stdout)
	if !ok {
		return nil, false
	}
	if rejectUnknown(m, universalKeys()...) {
		return nil, false
	}
	universal, err := parseUniversalWire(m)
	if err != nil {
		return nil, false
	}
	return &preCompactOutput{universal: universal}, true
}

func parsePostCompact(stdout string) (*statelessHookOutput, bool) {
	m, ok := objectFields(stdout)
	if !ok {
		return nil, false
	}
	if rejectUnknown(m, universalKeys()...) {
		return nil, false
	}
	universal, err := parseUniversalWire(m)
	if err != nil {
		return nil, false
	}
	return &statelessHookOutput{universal: universal}, true
}

func strPtr(s string) *string { return &s }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
