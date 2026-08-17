package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/tools"
)

// This file ports the multi-agent tool handlers from codex-core's
// tools/handlers/multi_agents/{spawn,send_input,resume_agent,wait,close_agent}.rs
// and multi_agents_common.rs. Each handler parses its arguments, runs the same
// depth checks, emits the same begin/end collab events, routes through the
// injected [CollabControl], and serializes the same result shape (the Rust
// SpawnAgentResult / SendInputResult / ResumeAgentResult / WaitAgentResult /
// CloseAgentResult JSON bodies via tool_output_json_text).
//
// STUB: role-config application (apply_role_to_config), model-override and
// service-tier validation against a live models-manager, and the depth-driven
// SpawnCsv/Collab feature disable are owned by the config/models area; the child
// config built here carries the runtime turn overrides + base instructions
// (build_agent_shared_config subset). See DEVIATIONS.

// defaultAgentMaxDepth mirrors the Rust DEFAULT_AGENT_MAX_DEPTH. The turn-running
// subset does not surface a per-config override, so the default bound applies.
const defaultAgentMaxDepth int32 = 1

const (
	// waitTimeoutDefaultMS / waitTimeoutMinMS / waitTimeoutMaxMS mirror
	// multi_agents_common.rs DEFAULT/MIN/MAX_WAIT_TIMEOUT_MS.
	waitTimeoutDefaultMS int64 = 30_000
	waitTimeoutMinMS     int64 = 10_000
	waitTimeoutMaxMS     int64 = 3_600_000
)

// ----------------------------------------------------------------------------
// shared argument + input parsing (multi_agents_common.rs)
// ----------------------------------------------------------------------------

// collabFunctionArguments extracts the JSON arguments string from a function
// payload, mirroring the Rust `function_arguments`.
func collabFunctionArguments(payload tools.ToolPayload) (string, error) {
	if payload.Kind != tools.ToolPayloadKindFunction {
		return "", tools.RespondToModelError("collab handler received unsupported payload")
	}
	return payload.Arguments, nil
}

// parseCollabArguments unmarshals the function arguments into v, surfacing a
// model-facing error on malformed JSON, mirroring the Rust `parse_arguments`.
func parseCollabArguments(arguments string, v any) error {
	if err := json.Unmarshal([]byte(arguments), v); err != nil {
		return tools.RespondToModelError(fmt.Sprintf("failed to parse function arguments: %v", err))
	}
	return nil
}

// parseAgentIDTarget parses a single agent-id target, mirroring the Rust
// `parse_agent_id_target` (ThreadId::from_string).
func parseAgentIDTarget(target string) (protocol.ThreadID, error) {
	if _, err := uuid.Parse(target); err != nil {
		return protocol.ThreadID{}, tools.RespondToModelError(fmt.Sprintf("invalid agent id %s: %v", target, err))
	}
	return protocol.NewThreadID(target), nil
}

// parseAgentIDTargets parses a non-empty list of agent-id targets, mirroring the
// Rust `parse_agent_id_targets`.
func parseAgentIDTargets(targets []string) ([]protocol.ThreadID, error) {
	if len(targets) == 0 {
		return nil, tools.RespondToModelError("agent ids must be non-empty")
	}
	out := make([]protocol.ThreadID, 0, len(targets))
	for _, target := range targets {
		id, err := parseAgentIDTarget(target)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// parseCollabInput builds the initial Op from a message or items, mirroring the
// Rust `parse_collab_input` (exactly one of message/items, non-empty).
func parseCollabInput(message *string, items []protocol.UserInput, itemsPresent bool) (protocol.Op, error) {
	switch {
	case message != nil && itemsPresent:
		return protocol.Op{}, tools.RespondToModelError("Provide either message or items, but not both")
	case message == nil && !itemsPresent:
		return protocol.Op{}, tools.RespondToModelError("Provide one of: message or items")
	case message != nil:
		if strings.TrimSpace(*message) == "" {
			return protocol.Op{}, tools.RespondToModelError("Empty message can't be sent to an agent")
		}
		return protocol.Op{Type: protocol.OpUserInput, Items: []protocol.UserInput{
			{Type: protocol.UserInputKindText, Text: *message},
		}}, nil
	default:
		if len(items) == 0 {
			return protocol.Op{}, tools.RespondToModelError("Items can't be empty")
		}
		return protocol.Op{Type: protocol.OpUserInput, Items: items}, nil
	}
}

// renderCollabInputPreview renders the last-task preview for an initial op,
// mirroring the Rust `render_input_preview`.
func renderCollabInputPreview(op protocol.Op) string {
	switch op.Type {
	case protocol.OpUserInput:
		lines := make([]string, 0, len(op.Items))
		for _, item := range op.Items {
			lines = append(lines, renderCollabUserInputItem(item))
		}
		return strings.Join(lines, "\n")
	case protocol.OpInterAgentCommunication:
		if op.Communication != nil {
			return op.Communication.Content
		}
		return ""
	default:
		return ""
	}
}

func renderCollabUserInputItem(item protocol.UserInput) string {
	switch item.Type {
	case protocol.UserInputKindText:
		return item.Text
	case protocol.UserInputKindImage:
		return "[image]"
	case protocol.UserInputKindLocalImage:
		return fmt.Sprintf("[local_image:%s]", item.Path)
	case protocol.UserInputKindSkill:
		return fmt.Sprintf("[skill:$%s](%s)", item.Name, item.Path)
	case protocol.UserInputKindMention:
		return fmt.Sprintf("[mention:$%s](%s)", item.Name, item.MentionPath)
	default:
		return "[input]"
	}
}

// ----------------------------------------------------------------------------
// depth + session source helpers (multi_agents_common.rs / agent helpers)
// ----------------------------------------------------------------------------

// turnSessionSource extracts the rollout session source from a turn context, or
// the default CLI source when absent/typed otherwise.
func turnSessionSource(tc *TurnContext) rollout.SessionSource {
	if tc == nil {
		return rollout.DefaultSessionSource()
	}
	switch v := tc.SessionSource.(type) {
	case rollout.SessionSource:
		return v
	case *rollout.SessionSource:
		if v != nil {
			return *v
		}
	}
	return rollout.DefaultSessionSource()
}

// nextThreadSpawnDepth returns the depth for a child spawned from a thread whose
// session source is the given value, mirroring the Rust `next_thread_spawn_depth`.
func nextThreadSpawnDepth(source rollout.SessionSource) int32 {
	depth := sessionSpawnDepth(source)
	if depth == int32(^uint32(0)>>1) {
		return depth
	}
	return depth + 1
}

func sessionSpawnDepth(source rollout.SessionSource) int32 {
	if source.Kind == rollout.SessionSourceKindSubAgent &&
		source.SubAgent != nil &&
		source.SubAgent.Kind == rollout.SubAgentSourceKindThreadSpawn &&
		source.SubAgent.ThreadSpawn != nil {
		return source.SubAgent.ThreadSpawn.Depth
	}
	return 0
}

// exceedsThreadSpawnDepthLimit mirrors the Rust `exceeds_thread_spawn_depth_limit`.
func exceedsThreadSpawnDepthLimit(depth, maxDepth int32) bool { return depth > maxDepth }

// buildThreadSpawnSource builds the child's thread-spawn session source,
// mirroring the Rust `thread_spawn_source` (task_name omitted in the v1 path, so
// no agent path is derived here — the control plane reserves it).
func buildThreadSpawnSource(parentThreadID protocol.ThreadID, depth int32, agentRole *string) rollout.SessionSource {
	return rollout.SessionSource{
		Kind: rollout.SessionSourceKindSubAgent,
		SubAgent: &rollout.SubAgentSource{
			Kind: rollout.SubAgentSourceKindThreadSpawn,
			ThreadSpawn: &rollout.ThreadSpawnSource{
				ParentThreadID: parentThreadID,
				Depth:          depth,
				AgentRole:      cloneStringPtr(agentRole),
			},
		},
	}
}

// buildAgentSpawnConfig builds the child config snapshot for a spawn, mirroring
// the runtime-override subset of the Rust `build_agent_spawn_config` /
// `build_agent_shared_config`: it starts from the parent session configuration,
// overlays the live turn's model / reasoning / cwd / approval policy / base
// instructions, and resets the session source (the control plane sets it).
//
// STUB: model-provider refresh, developer/compact prompt threading from the turn,
// and role-config application are owned by the config/models area.
func buildAgentSpawnConfig(sess *Session, tc *TurnContext) SessionConfiguration {
	cfg := sess.CloneConfiguration()
	if tc != nil {
		if tc.ModelSlug != "" {
			cfg.CollaborationMode.Settings.Model = tc.ModelSlug
		}
		if tc.ReasoningEffort != nil {
			cfg.CollaborationMode.Settings.ReasoningEffort = clonePtr(tc.ReasoningEffort)
		}
		if tc.Cwd != "" {
			cfg.Cwd = tc.Cwd
		}
		cfg.ApprovalPolicy = tc.ApprovalPolicy
		if tc.BaseInstructions != "" {
			cfg.BaseInstructions = tc.BaseInstructions
		}
	}
	return cfg
}

// ----------------------------------------------------------------------------
// event + error helpers
// ----------------------------------------------------------------------------

func collabAgentMetadataOrEmpty(md *CollabAgentMetadata) CollabAgentMetadata {
	if md == nil {
		return CollabAgentMetadata{}
	}
	return *md
}

func cloneStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// defaultReasoningEffortRaw is the JSON for ReasoningEffort::default(), used in
// the spawn begin/end events when the model omits an effort. serde serializes the
// default ReasoningEffort enum; codex's default is "medium" (matching
// ReasoningEffort::default()). It is computed once.
func reasoningEffortEventRaw(effort *protocol.ReasoningEffort) json.RawMessage {
	value := protocol.ReasoningEffortMedium
	if effort != nil {
		value = *effort
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`"medium"`)
	}
	return raw
}

// ----------------------------------------------------------------------------
// spawn_agent (spawn.rs)
// ----------------------------------------------------------------------------

type spawnAgentArgs struct {
	Message         *string                   `json:"message"`
	Items           []protocol.UserInput      `json:"items"`
	AgentType       *string                   `json:"agent_type"`
	Model           *string                   `json:"model"`
	ReasoningEffort *protocol.ReasoningEffort `json:"reasoning_effort"`
	ServiceTier     *string                   `json:"service_tier"`
	ForkContext     bool                      `json:"fork_context"`
	itemsPresent    bool                      `json:"-"`
}

func (a *spawnAgentArgs) UnmarshalJSON(data []byte) error {
	type alias spawnAgentArgs
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var tmp alias
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*a = spawnAgentArgs(tmp)
	_, a.itemsPresent = raw["items"]
	return nil
}

func handleSpawnAgent(ctx context.Context, control CollabControl, h *ToolHandlerContext) (tools.ToolOutput, error) {
	arguments, err := collabFunctionArguments(h.Payload)
	if err != nil {
		return nil, err
	}
	var args spawnAgentArgs
	if perr := parseCollabArguments(arguments, &args); perr != nil {
		return nil, perr
	}

	roleName := trimmedRole(args.AgentType)
	initialOp, perr := parseCollabInput(args.Message, args.Items, args.itemsPresent)
	if perr != nil {
		return nil, perr
	}
	prompt := renderCollabInputPreview(initialOp)

	sessionSource := turnSessionSource(h.Turn)
	childDepth := nextThreadSpawnDepth(sessionSource)
	if exceedsThreadSpawnDepthLimit(childDepth, defaultAgentMaxDepth) {
		return nil, tools.RespondToModelError("Agent depth limit reached. Solve the task yourself.")
	}

	if args.ForkContext {
		if rerr := rejectFullForkSpawnOverrides(roleName, args.Model, args.ReasoningEffort); rerr != nil {
			return nil, rerr
		}
	}

	model := ""
	if args.Model != nil {
		model = *args.Model
	}
	emitCollabEvent(h, protocol.EventMsg{
		Type: protocol.EventMsgKindCollabAgentSpawnBegin,
		CollabAgentSpawnBegin: &protocol.CollabAgentSpawnBeginEvent{
			CallID:          h.CallID,
			StartedAt:       NowUnixMillis(),
			SenderThreadID:  senderThreadID(h),
			Prompt:          prompt,
			Model:           model,
			ReasoningEffort: reasoningEffortEventRaw(args.ReasoningEffort),
		},
	})

	cfg := buildAgentSpawnConfig(h.Session, h.Turn)
	if args.ServiceTier != nil {
		cfg.ServiceTier = cloneStringPtr(args.ServiceTier)
	}

	source := buildThreadSpawnSource(senderThreadID(h), childDepth, roleName)
	var environments *[]protocol.TurnEnvironmentSelection
	if h.Turn != nil {
		envs := append([]protocol.TurnEnvironmentSelection(nil), h.Turn.Environments...)
		environments = &envs
	}

	result, spawnErr := control.SpawnAgent(ctx, CollabSpawnRequest{
		Configuration:     cfg,
		InitialOp:         initialOp,
		Source:            source,
		ForkContext:       args.ForkContext,
		ParentRolloutPath: parentRolloutPath(h),
		Environments:      environments,
	})

	var (
		newThreadID *protocol.ThreadID
		status      = protocol.AgentStatus{Kind: protocol.AgentStatusNotFound}
		metadata    CollabAgentMetadata
	)
	if spawnErr == nil {
		id := result.ThreadID
		newThreadID = &id
		status = result.Status
		metadata = result.Metadata
	}

	// Prefer the live config snapshot for the effective model/nickname/role.
	var snapshot *CollabAgentConfigSnapshot
	if newThreadID != nil {
		snapshot = control.AgentConfigSnapshot(*newThreadID)
	}
	newNickname, newRole := metadata.AgentNickname, metadata.AgentRole
	effectiveModel := model
	effectiveEffort := args.ReasoningEffort
	if snapshot != nil {
		if src := snapshot.Source; src.Kind != "" {
			if n := sourceNickname(src); n != nil {
				newNickname = n
			}
			if r := sourceRole(src); r != nil {
				newRole = r
			}
		}
		if snapshot.Model != "" {
			effectiveModel = snapshot.Model
		}
		if snapshot.ReasoningEffort != nil {
			effectiveEffort = snapshot.ReasoningEffort
		}
	}

	emitCollabEvent(h, protocol.EventMsg{
		Type: protocol.EventMsgKindCollabAgentSpawnEnd,
		CollabAgentSpawnEnd: &protocol.CollabAgentSpawnEndEvent{
			CallID:           h.CallID,
			CompletedAt:      NowUnixMillis(),
			SenderThreadID:   senderThreadID(h),
			NewThreadID:      newThreadID,
			NewAgentNickname: cloneStringPtr(newNickname),
			NewAgentRole:     cloneStringPtr(newRole),
			Prompt:           prompt,
			Model:            effectiveModel,
			ReasoningEffort:  reasoningEffortEventRaw(effectiveEffort),
			Status:           status,
		},
	})

	if spawnErr != nil {
		return nil, collabSpawnError(spawnErr)
	}

	return collabJSONOutput(spawnAgentResult{
		AgentID:  result.ThreadID.String(),
		Nickname: cloneStringPtr(newNickname),
	}, "spawn_agent"), nil
}

type spawnAgentResult struct {
	AgentID  string  `json:"agent_id"`
	Nickname *string `json:"nickname"`
}

func trimmedRole(agentType *string) *string {
	if agentType == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*agentType)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// rejectFullForkSpawnOverrides mirrors the Rust `reject_full_fork_spawn_overrides`.
func rejectFullForkSpawnOverrides(role, model *string, effort *protocol.ReasoningEffort) error {
	if role != nil || model != nil || effort != nil {
		return tools.RespondToModelError("Full-history forked agents inherit the parent agent type, model, and reasoning effort; omit agent_type, model, and reasoning_effort, or spawn without a full-history fork.")
	}
	return nil
}

// ----------------------------------------------------------------------------
// send_input (send_input.rs)
// ----------------------------------------------------------------------------

type sendInputArgs struct {
	Target       string               `json:"target"`
	Message      *string              `json:"message"`
	Items        []protocol.UserInput `json:"items"`
	Interrupt    bool                 `json:"interrupt"`
	itemsPresent bool                 `json:"-"`
}

func (a *sendInputArgs) UnmarshalJSON(data []byte) error {
	type alias sendInputArgs
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var tmp alias
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*a = sendInputArgs(tmp)
	_, a.itemsPresent = raw["items"]
	return nil
}

func handleSendInput(ctx context.Context, control CollabControl, h *ToolHandlerContext) (tools.ToolOutput, error) {
	arguments, err := collabFunctionArguments(h.Payload)
	if err != nil {
		return nil, err
	}
	var args sendInputArgs
	if perr := parseCollabArguments(arguments, &args); perr != nil {
		return nil, perr
	}
	receiver, perr := parseAgentIDTarget(args.Target)
	if perr != nil {
		return nil, perr
	}
	initialOp, perr := parseCollabInput(args.Message, args.Items, args.itemsPresent)
	if perr != nil {
		return nil, perr
	}
	prompt := renderCollabInputPreview(initialOp)
	receiverAgent := collabAgentMetadataOrEmpty(control.GetAgentMetadata(receiver))

	if args.Interrupt {
		if _, ierr := control.InterruptAgent(ctx, receiver); ierr != nil {
			return nil, collabAgentError(receiver, ierr)
		}
	}

	emitCollabEvent(h, protocol.EventMsg{
		Type: protocol.EventMsgKindCollabAgentInteractionBegin,
		CollabAgentInteractionBegin: &protocol.CollabAgentInteractionBeginEvent{
			CallID:           h.CallID,
			StartedAt:        NowUnixMillis(),
			SenderThreadID:   senderThreadID(h),
			ReceiverThreadID: receiver,
			Prompt:           prompt,
		},
	})

	subID, sendErr := control.SendInput(ctx, receiver, initialOp)
	status := control.GetStatus(receiver)

	emitCollabEvent(h, protocol.EventMsg{
		Type: protocol.EventMsgKindCollabAgentInteractionEnd,
		CollabAgentInteractionEnd: &protocol.CollabAgentInteractionEndEvent{
			CallID:                h.CallID,
			CompletedAt:           NowUnixMillis(),
			SenderThreadID:        senderThreadID(h),
			ReceiverThreadID:      receiver,
			ReceiverAgentNickname: cloneStringPtr(receiverAgent.AgentNickname),
			ReceiverAgentRole:     cloneStringPtr(receiverAgent.AgentRole),
			Prompt:                prompt,
			Status:                status,
		},
	})

	if sendErr != nil {
		return nil, collabAgentError(receiver, sendErr)
	}
	return collabJSONOutput(sendInputResult{SubmissionID: subID}, "send_input"), nil
}

type sendInputResult struct {
	SubmissionID string `json:"submission_id"`
}

// ----------------------------------------------------------------------------
// resume_agent (resume_agent.rs)
// ----------------------------------------------------------------------------

type resumeAgentArgs struct {
	ID string `json:"id"`
}

func handleResumeAgent(ctx context.Context, control CollabControl, h *ToolHandlerContext) (tools.ToolOutput, error) {
	arguments, err := collabFunctionArguments(h.Payload)
	if err != nil {
		return nil, err
	}
	var args resumeAgentArgs
	if perr := parseCollabArguments(arguments, &args); perr != nil {
		return nil, perr
	}
	receiver, perr := parseAgentIDTarget(args.ID)
	if perr != nil {
		return nil, perr
	}
	receiverAgent := collabAgentMetadataOrEmpty(control.GetAgentMetadata(receiver))

	sessionSource := turnSessionSource(h.Turn)
	childDepth := nextThreadSpawnDepth(sessionSource)
	if exceedsThreadSpawnDepthLimit(childDepth, defaultAgentMaxDepth) {
		return nil, tools.RespondToModelError("Agent depth limit reached. Solve the task yourself.")
	}

	emitCollabEvent(h, protocol.EventMsg{
		Type: protocol.EventMsgKindCollabResumeBegin,
		CollabResumeBegin: &protocol.CollabResumeBeginEvent{
			CallID:                h.CallID,
			StartedAt:             NowUnixMillis(),
			SenderThreadID:        senderThreadID(h),
			ReceiverThreadID:      receiver,
			ReceiverAgentNickname: cloneStringPtr(receiverAgent.AgentNickname),
			ReceiverAgentRole:     cloneStringPtr(receiverAgent.AgentRole),
		},
	})

	status := control.GetStatus(receiver)
	var resumeErr error
	if status.Kind == protocol.AgentStatusNotFound {
		cfg := buildAgentSpawnConfig(h.Session, h.Turn)
		cfg.BaseInstructions = "" // resume sources base instructions from rollout/session metadata
		source := buildThreadSpawnSource(senderThreadID(h), childDepth, nil)
		rerr := control.ResumeAgent(ctx, CollabResumeRequest{
			Configuration: cfg,
			ThreadID:      receiver,
			Source:        source,
		})
		status = control.GetStatus(receiver)
		if rerr == nil {
			if md := control.GetAgentMetadata(receiver); md != nil {
				receiverAgent = *md
			}
		} else {
			resumeErr = collabAgentError(receiver, rerr)
		}
	}

	emitCollabEvent(h, protocol.EventMsg{
		Type: protocol.EventMsgKindCollabResumeEnd,
		CollabResumeEnd: &protocol.CollabResumeEndEvent{
			CallID:                h.CallID,
			CompletedAt:           NowUnixMillis(),
			SenderThreadID:        senderThreadID(h),
			ReceiverThreadID:      receiver,
			ReceiverAgentNickname: cloneStringPtr(receiverAgent.AgentNickname),
			ReceiverAgentRole:     cloneStringPtr(receiverAgent.AgentRole),
			Status:                status,
		},
	})

	if resumeErr != nil {
		return nil, resumeErr
	}
	return collabJSONOutput(resumeAgentResult{Status: status}, "resume_agent"), nil
}

type resumeAgentResult struct {
	Status protocol.AgentStatus `json:"status"`
}

// ----------------------------------------------------------------------------
// close_agent (close_agent.rs)
// ----------------------------------------------------------------------------

type closeAgentArgs struct {
	Target string `json:"target"`
}

func handleCloseAgent(ctx context.Context, control CollabControl, h *ToolHandlerContext) (tools.ToolOutput, error) {
	arguments, err := collabFunctionArguments(h.Payload)
	if err != nil {
		return nil, err
	}
	var args closeAgentArgs
	if perr := parseCollabArguments(arguments, &args); perr != nil {
		return nil, perr
	}
	agentID, perr := parseAgentIDTarget(args.Target)
	if perr != nil {
		return nil, perr
	}
	receiverMeta := control.GetAgentMetadata(agentID)
	receiverAgent := collabAgentMetadataOrEmpty(receiverMeta)

	emitCollabEvent(h, protocol.EventMsg{
		Type: protocol.EventMsgKindCollabCloseBegin,
		CollabCloseBegin: &protocol.CollabCloseBeginEvent{
			CallID:           h.CallID,
			StartedAt:        NowUnixMillis(),
			SenderThreadID:   senderThreadID(h),
			ReceiverThreadID: agentID,
		},
	})

	// Capture the previous status before shutting the agent down. subscribe_status
	// borrows the live watch value; when the agent is not live we fall back to
	// get_status (mirroring the Rust ThreadNotFound branches).
	status, sch, unsub, serr := control.SubscribeStatus(ctx, agentID)
	if serr == nil {
		if unsub != nil {
			unsub()
		}
		_ = sch
	} else if errors.Is(serr, ErrThreadNotFound) {
		status = control.GetStatus(agentID)
	} else {
		status = control.GetStatus(agentID)
		emitCollabEvent(h, protocol.EventMsg{
			Type: protocol.EventMsgKindCollabCloseEnd,
			CollabCloseEnd: &protocol.CollabCloseEndEvent{
				CallID:                h.CallID,
				CompletedAt:           NowUnixMillis(),
				SenderThreadID:        senderThreadID(h),
				ReceiverThreadID:      agentID,
				ReceiverAgentNickname: cloneStringPtr(receiverAgent.AgentNickname),
				ReceiverAgentRole:     cloneStringPtr(receiverAgent.AgentRole),
				Status:                status,
			},
		})
		return nil, collabAgentError(agentID, serr)
	}

	closeErr := control.CloseAgent(ctx, agentID)

	emitCollabEvent(h, protocol.EventMsg{
		Type: protocol.EventMsgKindCollabCloseEnd,
		CollabCloseEnd: &protocol.CollabCloseEndEvent{
			CallID:                h.CallID,
			CompletedAt:           NowUnixMillis(),
			SenderThreadID:        senderThreadID(h),
			ReceiverThreadID:      agentID,
			ReceiverAgentNickname: cloneStringPtr(receiverAgent.AgentNickname),
			ReceiverAgentRole:     cloneStringPtr(receiverAgent.AgentRole),
			Status:                status,
		},
	})

	if closeErr != nil {
		return nil, collabAgentError(agentID, closeErr)
	}
	return collabJSONOutput(closeAgentResult{PreviousStatus: status}, "close_agent"), nil
}

type closeAgentResult struct {
	PreviousStatus protocol.AgentStatus `json:"previous_status"`
}

// ----------------------------------------------------------------------------
// wait_agent (wait.rs)
// ----------------------------------------------------------------------------

type waitArgs struct {
	Targets   []string `json:"targets"`
	TimeoutMS *int64   `json:"timeout_ms"`
}

func handleWaitAgent(ctx context.Context, control CollabControl, h *ToolHandlerContext) (tools.ToolOutput, error) {
	arguments, err := collabFunctionArguments(h.Payload)
	if err != nil {
		return nil, err
	}
	var args waitArgs
	if perr := parseCollabArguments(arguments, &args); perr != nil {
		return nil, perr
	}
	receivers, perr := parseAgentIDTargets(args.Targets)
	if perr != nil {
		return nil, perr
	}

	receiverAgents := make([]protocol.CollabAgentRef, 0, len(receivers))
	targetByThreadID := make(map[string]string, len(receivers))
	for _, id := range receivers {
		md := collabAgentMetadataOrEmpty(control.GetAgentMetadata(id))
		target := id.String()
		if md.AgentPath != nil {
			target = md.AgentPath.String()
		}
		targetByThreadID[id.String()] = target
		receiverAgents = append(receiverAgents, protocol.CollabAgentRef{
			ThreadID:      id,
			AgentNickname: cloneStringPtr(md.AgentNickname),
			AgentRole:     cloneStringPtr(md.AgentRole),
		})
	}

	timeoutMS := waitTimeoutDefaultMS
	if args.TimeoutMS != nil {
		timeoutMS = *args.TimeoutMS
	}
	if timeoutMS <= 0 {
		return nil, tools.RespondToModelError("timeout_ms must be greater than zero")
	}
	timeoutMS = clampInt64(timeoutMS, waitTimeoutMinMS, waitTimeoutMaxMS)

	emitCollabEvent(h, protocol.EventMsg{
		Type: protocol.EventMsgKindCollabWaitingBegin,
		CollabWaitingBegin: &protocol.CollabWaitingBeginEvent{
			StartedAt:         NowUnixMillis(),
			SenderThreadID:    senderThreadID(h),
			ReceiverThreadIDs: append([]protocol.ThreadID(nil), receivers...),
			ReceiverAgents:    receiverAgents,
			CallID:            h.CallID,
		},
	})
	// 0.147 lifecycle item (spec 50 D0.7): the wait call starts in_progress and
	// completes as failed when any target ended Errored/NotFound, so a
	// sub-agent failure is surfaced to the parent instead of an empty success.
	emitCollabToolCallItem(h, protocol.CollabAgentToolWait, receivers, receiverAgents, protocol.CollabAgentToolCallStatusInProgress, nil, false)

	statuses, steered, werr := collectWaitStatuses(ctx, control, h, receivers, receiverAgents, timeoutMS)
	if werr != nil {
		return nil, werr
	}

	// An empty result is a timeout unless a steer interrupted the wait
	// (0.147 WaitOutcome::Steered), which is neither a timeout nor a final status.
	timedOut := len(statuses) == 0 && !steered
	statusesByID := make(map[string]protocol.AgentStatus, len(statuses))
	statusMap := make(map[string]protocol.AgentStatus, len(statuses))
	for _, entry := range statuses {
		statusesByID[entry.threadID.String()] = entry.status
		if target, ok := targetByThreadID[entry.threadID.String()]; ok {
			statusMap[target] = entry.status
		}
	}

	emitCollabEvent(h, protocol.EventMsg{
		Type: protocol.EventMsgKindCollabWaitingEnd,
		CollabWaitingEnd: &protocol.CollabWaitingEndEvent{
			SenderThreadID: senderThreadID(h),
			CallID:         h.CallID,
			CompletedAt:    NowUnixMillis(),
			AgentStatuses:  buildWaitAgentStatuses(statusesByID, receiverAgents),
			Statuses:       statusesByID,
		},
	})
	emitCollabToolCallItem(h, protocol.CollabAgentToolWait, receivers, receiverAgents, protocol.WaitToolCallStatus(statusesByID), statusesByID, true)

	return collabJSONOutput(waitAgentResult{Status: statusMap, TimedOut: timedOut}, "wait_agent"), nil
}

type waitAgentResult struct {
	Status   map[string]protocol.AgentStatus `json:"status"`
	TimedOut bool                            `json:"timed_out"`
}

type waitStatusEntry struct {
	threadID protocol.ThreadID
	status   protocol.AgentStatus
}

// waitSubscription bundles a per-target status subscription used while waiting.
type waitSubscription struct {
	id    protocol.ThreadID
	ch    <-chan protocol.AgentStatus
	unsub func()
}

// collectWaitStatuses subscribes to each target's status and collects the final
// statuses, returning early once at least one target reaches a final status or
// the deadline elapses. It mirrors the Rust wait loop: any already-final status
// short-circuits the wait; otherwise the first transition to a final status wins
// and other already-final results are drained without blocking.
func collectWaitStatuses(
	ctx context.Context,
	control CollabControl,
	h *ToolHandlerContext,
	receivers []protocol.ThreadID,
	receiverAgents []protocol.CollabAgentRef,
	timeoutMS int64,
) (entries []waitStatusEntry, steered bool, err error) {
	var subs []waitSubscription
	var initialFinal []waitStatusEntry
	defer func() {
		for _, s := range subs {
			if s.unsub != nil {
				s.unsub()
			}
		}
	}()

	for _, id := range receivers {
		status, ch, unsub, serr := control.SubscribeStatus(ctx, id)
		switch {
		case serr == nil:
			if isFinalAgentStatus(status) {
				initialFinal = append(initialFinal, waitStatusEntry{threadID: id, status: status})
			}
			subs = append(subs, waitSubscription{id: id, ch: ch, unsub: unsub})
		case errors.Is(serr, ErrThreadNotFound):
			initialFinal = append(initialFinal, waitStatusEntry{threadID: id, status: protocol.AgentStatus{Kind: protocol.AgentStatusNotFound}})
		default:
			// Emit the waiting-end event with the current status before surfacing
			// the error, mirroring the Rust early-return branch.
			statuses := map[string]protocol.AgentStatus{id.String(): control.GetStatus(id)}
			emitCollabEvent(h, protocol.EventMsg{
				Type: protocol.EventMsgKindCollabWaitingEnd,
				CollabWaitingEnd: &protocol.CollabWaitingEndEvent{
					SenderThreadID: senderThreadID(h),
					CallID:         h.CallID,
					CompletedAt:    NowUnixMillis(),
					AgentStatuses:  buildWaitAgentStatuses(statuses, receiverAgents),
					Statuses:       statuses,
				},
			})
			emitCollabToolCallItem(h, protocol.CollabAgentToolWait, receivers, receiverAgents, protocol.WaitToolCallStatus(statuses), statuses, true)
			return nil, false, collabAgentError(id, serr)
		}
	}

	if len(initialFinal) > 0 {
		return initialFinal, false, nil
	}

	deadline := time.NewTimer(time.Duration(timeoutMS) * time.Millisecond)
	defer deadline.Stop()

	// A steer (user input for the waiting parent) interrupts the wait so the
	// parent can react, mirroring the 0.147 `WaitOutcome::Steered` (spec 50
	// D0.2). Subscribe before blocking; pending steer input ends the wait at once.
	steerCh, pendingActivity, unsubscribeActivity := subscribeWaitActivity(h)
	defer unsubscribeActivity()
	if pendingActivity != nil && *pendingActivity == InputQueueActivitySteer {
		return nil, true, nil
	}

	// Wait for the first target to reach a final status (or the deadline / ctx /
	// a steer).
	first, ok, steered := waitFirstFinal(ctx, control, subs, deadline.C, steerCh)
	if !ok {
		return nil, steered, nil
	}
	results := []waitStatusEntry{first}
	// Drain any other targets that have already become final without blocking.
	for _, s := range subs {
		if s.id == first.threadID {
			continue
		}
		if entry, done := drainFinal(control, s.id, s.ch); done {
			results = append(results, entry)
		}
	}
	return results, false, nil
}

// waitFirstFinal blocks until one subscription reports a final status, the
// deadline elapses, ctx is cancelled, or a steer arrives. It returns (entry,
// true, false) on a final status, (zero, false, true) on a steer and (zero,
// false, false) otherwise.
func waitFirstFinal(
	ctx context.Context,
	control CollabControl,
	subs []waitSubscription,
	deadline <-chan time.Time,
	activity <-chan InputQueueActivity,
) (entry waitStatusEntry, ok bool, steered bool) {
	resultCh := make(chan waitStatusEntry, len(subs))
	stop := make(chan struct{})
	defer close(stop)
	for _, s := range subs {
		go watchUntilFinal(ctx, control, s.id, s.ch, resultCh, stop)
	}
	for {
		select {
		case entry := <-resultCh:
			return entry, true, false
		case <-deadline:
			return waitStatusEntry{}, false, false
		case <-ctx.Done():
			return waitStatusEntry{}, false, false
		case a, open := <-activity:
			if open && a == InputQueueActivitySteer {
				return waitStatusEntry{}, false, true
			}
			if !open {
				activity = nil
			}
			// Mailbox activity does not end a v1 wait.
		}
	}
}

// subscribeWaitActivity subscribes the waiting parent to its session's input
// queue; a nil session yields a never-firing channel.
func subscribeWaitActivity(h *ToolHandlerContext) (<-chan InputQueueActivity, *InputQueueActivity, func()) {
	if h == nil || h.Session == nil {
		return nil, nil, func() {}
	}
	var turnState *TurnState
	if at := h.Session.ActiveTurn(); at != nil {
		turnState = at.State
	}
	return h.Session.InputQueue().SubscribeActivity(turnState)
}

// watchUntilFinal forwards the first final status observed on ch (or the latest
// status when ch closes) to out, mirroring the Rust `wait_for_final_status`.
func watchUntilFinal(
	ctx context.Context,
	control CollabControl,
	id protocol.ThreadID,
	ch <-chan protocol.AgentStatus,
	out chan<- waitStatusEntry,
	stop <-chan struct{},
) {
	for {
		select {
		case status, ok := <-ch:
			if !ok {
				latest := control.GetStatus(id)
				if isFinalAgentStatus(latest) {
					select {
					case out <- waitStatusEntry{threadID: id, status: latest}:
					case <-stop:
					}
				}
				return
			}
			if isFinalAgentStatus(status) {
				select {
				case out <- waitStatusEntry{threadID: id, status: status}:
				case <-stop:
				}
				return
			}
		case <-ctx.Done():
			return
		case <-stop:
			return
		}
	}
}

// drainFinal non-blockingly checks whether id has reached a final status, either
// already buffered on ch or via a fresh GetStatus.
func drainFinal(control CollabControl, id protocol.ThreadID, ch <-chan protocol.AgentStatus) (waitStatusEntry, bool) {
	for {
		select {
		case status, ok := <-ch:
			if !ok {
				latest := control.GetStatus(id)
				if isFinalAgentStatus(latest) {
					return waitStatusEntry{threadID: id, status: latest}, true
				}
				return waitStatusEntry{}, false
			}
			if isFinalAgentStatus(status) {
				return waitStatusEntry{threadID: id, status: status}, true
			}
		default:
			latest := control.GetStatus(id)
			if isFinalAgentStatus(latest) {
				return waitStatusEntry{threadID: id, status: latest}, true
			}
			return waitStatusEntry{}, false
		}
	}
}

// buildWaitAgentStatuses orders the status entries by receiver then by extra
// thread-id, mirroring the Rust `build_wait_agent_statuses`.
func buildWaitAgentStatuses(statuses map[string]protocol.AgentStatus, receiverAgents []protocol.CollabAgentRef) []protocol.CollabAgentStatusEntry {
	if len(statuses) == 0 {
		return nil
	}
	entries := make([]protocol.CollabAgentStatusEntry, 0, len(statuses))
	seen := make(map[string]struct{}, len(receiverAgents))
	for _, ref := range receiverAgents {
		seen[ref.ThreadID.String()] = struct{}{}
		if status, ok := statuses[ref.ThreadID.String()]; ok {
			entries = append(entries, protocol.CollabAgentStatusEntry{
				ThreadID:      ref.ThreadID,
				AgentNickname: cloneStringPtr(ref.AgentNickname),
				AgentRole:     cloneStringPtr(ref.AgentRole),
				Status:        status,
			})
		}
	}
	var extras []protocol.CollabAgentStatusEntry
	for idStr, status := range statuses {
		if _, ok := seen[idStr]; ok {
			continue
		}
		extras = append(extras, protocol.CollabAgentStatusEntry{
			ThreadID: protocol.NewThreadID(idStr),
			Status:   status,
		})
	}
	sort.Slice(extras, func(i, j int) bool {
		return extras[i].ThreadID.String() < extras[j].ThreadID.String()
	})
	entries = append(entries, extras...)
	return entries
}

func clampInt64(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ----------------------------------------------------------------------------
// shared output + error mapping
// ----------------------------------------------------------------------------

// collabJSONOutput serializes a collab result to its JSON text body, mirroring
// the Rust `tool_output_json_text` / FunctionToolOutput::from_text(success=true).
func collabJSONOutput(value any, toolName string) tools.ToolOutput {
	raw, err := json.Marshal(value)
	if err != nil {
		raw = []byte(fmt.Sprintf("%q", fmt.Sprintf("failed to serialize %s result: %v", toolName, err)))
	}
	return NewTextToolOutput(string(raw), boolPtr(true))
}

// collabSpawnError maps a control-plane spawn error onto a model-facing error,
// mirroring the Rust `collab_spawn_error`.
func collabSpawnError(err error) error {
	var fce *tools.FunctionCallError
	if errors.As(err, &fce) {
		return err
	}
	if errors.Is(err, ErrCollabManagerUnavailable) {
		return tools.RespondToModelError("collab manager unavailable")
	}
	return tools.RespondToModelError(fmt.Sprintf("collab spawn failed: %v", err))
}

// collabAgentError maps a control-plane agent error onto a model-facing error,
// mirroring the Rust `collab_agent_error`.
func collabAgentError(agentID protocol.ThreadID, err error) error {
	var fce *tools.FunctionCallError
	if errors.As(err, &fce) {
		return err
	}
	switch {
	case errors.Is(err, ErrThreadNotFound):
		return tools.RespondToModelError(fmt.Sprintf("agent with id %s not found", agentID))
	case errors.Is(err, ErrInternalAgentDied):
		return tools.RespondToModelError(fmt.Sprintf("agent with id %s is closed", agentID))
	case errors.Is(err, ErrCollabManagerUnavailable):
		return tools.RespondToModelError("collab manager unavailable")
	default:
		return tools.RespondToModelError(fmt.Sprintf("collab tool failed: %v", err))
	}
}

// senderThreadID returns the sender (current) thread id for a collab event. It
// is the session's thread id, mirroring the Rust `session.conversation_id`.
func senderThreadID(h *ToolHandlerContext) protocol.ThreadID {
	if h != nil && h.Session != nil {
		return h.Session.ThreadID()
	}
	return protocol.ThreadID{}
}

// parentRolloutPath returns the originating session's local rollout path, when
// filesystem-backed, or nil. It is threaded into a forked spawn so the control
// plane can read the parent's stored history, mirroring the Rust fork path that
// snapshots the parent thread's persisted rollout.
func parentRolloutPath(h *ToolHandlerContext) *string {
	if h == nil || h.Session == nil {
		return nil
	}
	return h.Session.RolloutPath()
}

// emitCollabEvent sends a collab event correlated with the turn's sub id,
// mirroring the Rust `session.send_event(&turn, ..)`.
func emitCollabEvent(h *ToolHandlerContext, msg protocol.EventMsg) {
	if h == nil || h.Session == nil {
		return
	}
	subID := ""
	if h.Turn != nil {
		subID = h.Turn.SubID
	}
	h.Session.SendEvent(subID, msg)
}

// sourceNickname / sourceRole read the nickname/role from a thread-spawn session
// source, if present.
func sourceNickname(src rollout.SessionSource) *string {
	if src.SubAgent != nil && src.SubAgent.ThreadSpawn != nil {
		return cloneStringPtr(src.SubAgent.ThreadSpawn.AgentNickname)
	}
	return nil
}

func sourceRole(src rollout.SessionSource) *string {
	if src.SubAgent != nil && src.SubAgent.ThreadSpawn != nil {
		return cloneStringPtr(src.SubAgent.ThreadSpawn.AgentRole)
	}
	return nil
}

// emitCollabToolCallItem emits the 0.147 CollabAgentToolCall lifecycle item for
// a collab tool call: ItemStarted while in_progress, ItemCompleted with the
// final status (completed / failed) and the targets' states. Mirrors the
// emit_turn_item_started / emit_turn_item_completed pairs in the upstream
// multi_agents handlers (spec 50 D0.7).
func emitCollabToolCallItem(h *ToolHandlerContext, tool protocol.CollabAgentTool, receivers []protocol.ThreadID, receiverAgents []protocol.CollabAgentRef, status protocol.CollabAgentToolCallStatus, states map[string]protocol.AgentStatus, completed bool) {
	if h == nil || h.Session == nil || h.Turn == nil {
		return
	}
	if states == nil {
		states = map[string]protocol.AgentStatus{}
	}
	item := protocol.TurnItem{
		Type: protocol.TurnItemKindCollabAgentToolCall,
		CollabAgentToolCall: &protocol.CollabAgentToolCallItem{
			ID:                h.CallID,
			Tool:              tool,
			Status:            status,
			SenderThreadID:    senderThreadID(h),
			ReceiverThreadIDs: append([]protocol.ThreadID(nil), receivers...),
			ReceiverAgents:    append([]protocol.CollabAgentRef(nil), receiverAgents...),
			AgentsStates:      states,
		},
	}
	if completed {
		EmitTurnItemCompleted(h.Session, h.Turn, item)
		return
	}
	EmitTurnItemStarted(h.Session, h.Turn, item)
}
