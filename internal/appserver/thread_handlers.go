package appserver

import (
	"context"
	"encoding/json"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
)

// handleThreadStart spawns a new thread, starts its event forwarder, and returns
// the thread metadata. It mirrors the Rust ThreadRequestProcessor::thread_start
// reduced to the turn-running subset.
func (p *Processor) handleThreadStart(ctx context.Context, conn *Conn, params *appserverproto.ThreadStartParams) (any, *RPCError) {
	cfg := p.threadStartConfig(conn, params)

	newThread, err := p.assembly.ThreadManager.StartThread(ctx, cfg)
	if err != nil {
		return nil, internalError("failed to start thread: %v", err)
	}

	p.registerForwarder(ctx, newThread)

	resp, rpcErr := p.threadStartResponse(newThread, cfg)
	if rpcErr != nil {
		return nil, rpcErr
	}
	return resp, nil
}

// handleThreadResume resumes a thread from supplied or persisted history, starts
// its forwarder, and returns the thread metadata.
func (p *Processor) handleThreadResume(ctx context.Context, conn *Conn, params *appserverproto.ThreadResumeParams) (any, *RPCError) {
	cfg := p.resumeForkConfig(
		conn,
		params.Model,
		params.ModelProvider,
		params.Cwd,
		params.RuntimeWorkspaceRoots,
		params.BaseInstructions,
		params.DeveloperInstructions,
	)

	var newThread core.NewThread
	var err error
	switch {
	case params.History != nil:
		threadID := protocol.NewThreadID(params.ThreadID)
		history, decErr := resumedHistoryFromRaw(threadID, params.Path, *params.History)
		if decErr != nil {
			return nil, invalidParams("invalid resume history: %v", decErr)
		}
		newThread, err = p.assembly.ThreadManager.ResumeThreadWithHistory(ctx, cfg, history)
	case params.Path != nil:
		newThread, err = p.assembly.ThreadManager.ResumeThreadFromRollout(ctx, cfg, *params.Path)
	default:
		return nil, invalidRequest("thread/resume requires either history or path")
	}
	if err != nil {
		return nil, internalError("failed to resume thread: %v", err)
	}

	p.registerForwarder(ctx, newThread)

	threadJSON, rpcErr := p.threadAggregate(newThread)
	if rpcErr != nil {
		return nil, rpcErr
	}
	return appserverproto.ThreadResumeResponse{
		Thread:            threadJSON,
		Model:             cfg.Model(),
		ModelProvider:     cfg.ProviderID,
		ServiceTier:       cfg.ServiceTier,
		Cwd:               protocol.AbsolutePath(cfg.Cwd),
		ApprovalPolicy:    defaultApprovalPolicy(),
		ApprovalsReviewer: defaultApprovalsReviewer(),
		Sandbox:           defaultSandboxPolicy(),
	}, nil
}

// handleThreadFork forks an existing thread, starts its forwarder, and returns
// the new thread metadata.
func (p *Processor) handleThreadFork(ctx context.Context, conn *Conn, params *appserverproto.ThreadForkParams) (any, *RPCError) {
	if params.Path == nil {
		return nil, invalidRequest("thread/fork requires a rollout path")
	}
	cfg := p.resumeForkConfig(
		conn,
		params.Model,
		params.ModelProvider,
		params.Cwd,
		params.RuntimeWorkspaceRoots,
		params.BaseInstructions,
		params.DeveloperInstructions,
	)

	newThread, err := p.assembly.ThreadManager.ForkThread(
		ctx,
		core.ForkInterrupted(),
		cfg,
		*params.Path,
		nil,
		params.PersistExtendedHistory,
	)
	if err != nil {
		return nil, internalError("failed to fork thread: %v", err)
	}

	p.registerForwarder(ctx, newThread)

	threadJSON, rpcErr := p.threadAggregate(newThread)
	if rpcErr != nil {
		return nil, rpcErr
	}
	return appserverproto.ThreadForkResponse{
		Thread:            threadJSON,
		Model:             cfg.Model(),
		ModelProvider:     cfg.ProviderID,
		ServiceTier:       cfg.ServiceTier,
		Cwd:               protocol.AbsolutePath(cfg.Cwd),
		ApprovalPolicy:    defaultApprovalPolicy(),
		ApprovalsReviewer: defaultApprovalsReviewer(),
		Sandbox:           defaultSandboxPolicy(),
	}, nil
}

// threadStartResponse assembles the thread/start response from a spawned thread.
func (p *Processor) threadStartResponse(newThread core.NewThread, cfg core.SessionConfiguration) (appserverproto.ThreadStartResponse, *RPCError) {
	threadJSON, rpcErr := p.threadAggregate(newThread)
	if rpcErr != nil {
		return appserverproto.ThreadStartResponse{}, rpcErr
	}
	return appserverproto.ThreadStartResponse{
		Thread:            threadJSON,
		Model:             cfg.Model(),
		ModelProvider:     cfg.ProviderID,
		ServiceTier:       cfg.ServiceTier,
		Cwd:               protocol.AbsolutePath(cfg.Cwd),
		ApprovalPolicy:    defaultApprovalPolicy(),
		ApprovalsReviewer: defaultApprovalsReviewer(),
		Sandbox:           defaultSandboxPolicy(),
	}, nil
}

// threadAggregate builds the `thread` aggregate carried (as raw JSON) in the
// thread/start, thread/resume, and thread/fork responses. The full v2 Thread
// type is sibling-owned; here we emit the minimal, wire-shaped object clients
// rely on at spawn time: the thread id and an empty turns view.
func (p *Processor) threadAggregate(newThread core.NewThread) (json.RawMessage, *RPCError) {
	agg := map[string]any{
		"id":     newThread.ThreadID.String(),
		"object": "thread",
		"turns":  []any{},
	}
	raw, err := json.Marshal(agg)
	if err != nil {
		return nil, internalError("marshal thread aggregate: %v", err)
	}
	return raw, nil
}

// defaultSandboxPolicy returns the sandbox policy reported in thread responses
// for the turn-running subset. It is read-only with no network access, the
// codex-conservative default for a freshly spawned thread; richer sandbox
// resolution (workspace-write derivation from config) is owned by the
// config/permissions area agents.
func defaultSandboxPolicy() protocol.SandboxPolicy {
	return protocol.NewReadOnlySandboxPolicy(false)
}

// defaultApprovalPolicy returns the approval policy reported in thread responses.
// It is on-request, the codex default; per-thread approval derivation is owned
// by the config/permissions area agents.
func defaultApprovalPolicy() appserverproto.AskForApprovalV2 {
	return appserverproto.AskForApprovalV2{Kind: appserverproto.AskForApprovalV2OnRequest}
}

// defaultApprovalsReviewer returns the approvals reviewer reported in thread
// responses (the user, by default).
func defaultApprovalsReviewer() appserverproto.ApprovalsReviewerV2 {
	return appserverproto.ApprovalsReviewerV2User
}

// resumedHistoryFromRaw decodes the resume history items (raw rollout items) into
// a [rollout.InitialHistory] for the resumed thread.
func resumedHistoryFromRaw(threadID protocol.ThreadID, path *string, raw []json.RawMessage) (rollout.InitialHistory, error) {
	items := make([]rollout.RolloutItem, 0, len(raw))
	for _, r := range raw {
		var item rollout.RolloutItem
		if err := json.Unmarshal(r, &item); err != nil {
			return rollout.InitialHistory{}, err
		}
		items = append(items, item)
	}
	return rollout.InitialHistory{
		Kind: rollout.InitialHistoryKindResumed,
		Resumed: &rollout.ResumedHistory{
			ConversationID: threadID,
			History:        items,
			RolloutPath:    path,
		},
	}, nil
}
