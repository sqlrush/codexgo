package analytics

// factKind enumerates the custom analytics facts that this port reduces into
// upload requests. It corresponds to the subset of Rust `CustomAnalyticsFact`
// that the named task surface exercises.
type factKind int

const (
	factSkillInvoked factKind = iota
	factHookRun
	factTurnTokenUsage
	factGuardianReview
	factAppMentioned
	factAppUsed
	factAcceptedLines
)

// analyticsFact is the internal queued fact. Mirrors the relevant arms of Rust
// `AnalyticsFact::Custom(CustomAnalyticsFact::...)`.
type analyticsFact struct {
	kind           factKind
	skillInvoked   *skillInvokedInput
	hookRun        *hookRunInput
	turnTokenUsage *TurnTokenUsageFact
	guardianReview *GuardianReviewEventParams
	appMentioned   *appMentionedInput
	appUsed        *appUsedInput
	acceptedLines  *AcceptedLineFingerprintEventInput
}

type skillInvokedInput struct {
	tracking    TrackEventsContext
	invocations []SkillInvocation
}

type hookRunInput struct {
	tracking TrackEventsContext
	hook     HookRunFact
}

type appMentionedInput struct {
	tracking TrackEventsContext
	mentions []AppInvocation
}

type appUsedInput struct {
	tracking TrackEventsContext
	app      AppInvocation
}

// analyticsReducer converts facts into one or more upload requests. The Rust
// reducer is async and stateful (it resolves auth/runtime lazily); this port
// keeps the conversion pure and synchronous because the named facts do not need
// cross-fact state.
type analyticsReducer struct {
	runtime CodexRuntimeMetadata
}

func newAnalyticsReducer() *analyticsReducer {
	return &analyticsReducer{runtime: CurrentRuntimeMetadata()}
}

// ingest reduces a single fact into upload requests. Mirrors Rust
// `AnalyticsReducer::ingest` for the supported fact variants.
func (r *analyticsReducer) ingest(fact analyticsFact) []TrackEventRequest {
	switch fact.kind {
	case factSkillInvoked:
		return r.reduceSkillInvoked(fact.skillInvoked)
	case factHookRun:
		return r.reduceHookRun(fact.hookRun)
	case factTurnTokenUsage:
		return r.reduceTurnTokenUsage(fact.turnTokenUsage)
	case factGuardianReview:
		return r.reduceGuardianReview(fact.guardianReview)
	case factAppMentioned:
		return r.reduceAppMentioned(fact.appMentioned)
	case factAppUsed:
		return r.reduceAppUsed(fact.appUsed)
	case factAcceptedLines:
		return r.reduceAcceptedLines(fact.acceptedLines)
	default:
		return nil
	}
}

func (r *analyticsReducer) reduceSkillInvoked(in *skillInvokedInput) []TrackEventRequest {
	if in == nil {
		return nil
	}
	events := make([]TrackEventRequest, 0, len(in.invocations))
	for _, invocation := range in.invocations {
		threadID := in.tracking.ThreadID
		turnID := in.tracking.TurnID
		model := in.tracking.ModelSlug
		scope := string(invocation.SkillScope)
		invokeType := invocation.InvocationType
		params := SkillInvocationEventParams{
			ProductClientID: nil,
			SkillScope:      &scope,
			PluginID:        invocation.PluginID,
			RepoURL:         nil,
			ThreadID:        &threadID,
			TurnID:          &turnID,
			InvokeType:      &invokeType,
			ModelSlug:       &model,
		}
		events = append(events, TrackEventRequest{
			kind: trackSkillInvocation,
			skillInvocation: &SkillInvocationEventRequest{
				EventType:   "codex_skill_invocation",
				SkillID:     invocation.SkillName,
				SkillName:   invocation.SkillName,
				EventParams: params,
			},
		})
	}
	return events
}

func (r *analyticsReducer) reduceHookRun(in *hookRunInput) []TrackEventRequest {
	if in == nil {
		return nil
	}
	return []TrackEventRequest{{
		kind: trackHookRun,
		hookRun: &CodexHookRunEventRequest{
			EventType:   "codex_hook_run",
			EventParams: codexHookRunMetadata(in.tracking, in.hook),
		},
	}}
}

func (r *analyticsReducer) reduceTurnTokenUsage(in *TurnTokenUsageFact) []TrackEventRequest {
	if in == nil {
		return nil
	}
	input := in.TokenUsage.InputTokens
	cached := in.TokenUsage.CachedInputTokens
	output := in.TokenUsage.OutputTokens
	reasoning := in.TokenUsage.ReasoningOutputTokens
	total := in.TokenUsage.TotalTokens
	return []TrackEventRequest{{
		kind: trackTurnTokenUsage,
		turnTokenUsage: &CodexTurnTokenUsageEventRequest{
			EventType: "codex_turn_token_usage",
			EventParams: CodexTurnTokenUsageEventParams{
				ThreadID:              in.ThreadID,
				TurnID:                in.TurnID,
				InputTokens:           &input,
				CachedInputTokens:     &cached,
				OutputTokens:          &output,
				ReasoningOutputTokens: &reasoning,
				TotalTokens:           &total,
			},
		},
	}}
}

func (r *analyticsReducer) reduceGuardianReview(in *GuardianReviewEventParams) []TrackEventRequest {
	if in == nil {
		return nil
	}
	return []TrackEventRequest{{
		kind: trackGuardianReview,
		guardianReview: &GuardianReviewEventRequest{
			EventType: "codex_guardian_review",
			EventParams: GuardianReviewEventPayload{
				SessionID: in.ThreadID,
				AppServerClient: CodexAppServerClientMetadata{
					ProductClientID: "",
					RpcTransport:    AppServerRpcTransportInProcess,
				},
				Runtime:        r.runtime,
				GuardianReview: *in,
			},
		},
	}}
}

func (r *analyticsReducer) reduceAppMentioned(in *appMentionedInput) []TrackEventRequest {
	if in == nil {
		return nil
	}
	events := make([]TrackEventRequest, 0, len(in.mentions))
	for _, mention := range in.mentions {
		events = append(events, TrackEventRequest{
			kind: trackAppMentioned,
			appMentioned: &CodexAppMentionedEventRequest{
				EventType:   "codex_app_mentioned",
				EventParams: codexAppMetadata(in.tracking, mention, ""),
			},
		})
	}
	return events
}

func (r *analyticsReducer) reduceAppUsed(in *appUsedInput) []TrackEventRequest {
	if in == nil {
		return nil
	}
	return []TrackEventRequest{{
		kind: trackAppUsed,
		appUsed: &CodexAppUsedEventRequest{
			EventType:   "codex_app_used",
			EventParams: codexAppMetadata(in.tracking, in.app, ""),
		},
	}}
}

func (r *analyticsReducer) reduceAcceptedLines(in *AcceptedLineFingerprintEventInput) []TrackEventRequest {
	if in == nil {
		return nil
	}
	requests := AcceptedLineFingerprintEventRequests(*in)
	events := make([]TrackEventRequest, 0, len(requests))
	for _, req := range requests {
		events = append(events, TrackEventRequest{
			kind:          trackAcceptedLines,
			acceptedLines: req,
		})
	}
	return events
}
