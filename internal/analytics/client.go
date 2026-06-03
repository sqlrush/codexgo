package analytics

import (
	"context"
	"sync"
)

const (
	// analyticsEventsQueueSize bounds the in-memory fact queue. Mirrors Rust
	// ANALYTICS_EVENTS_QUEUE_SIZE.
	analyticsEventsQueueSize = 256
	// analyticsEventDedupeMaxKeys bounds the per-turn app/plugin dedup sets.
	// Mirrors Rust ANALYTICS_EVENT_DEDUPE_MAX_KEYS.
	analyticsEventDedupeMaxKeys = 4096
)

// Uploader sends a batch of fully-formed track-event requests to the analytics
// backend. It abstracts the codex auth/HTTP details so the client logic remains
// faithful to the Rust reducer/batching behavior while staying decoupled from
// the login crate. A nil Uploader disables uploads (events are still reduced).
type Uploader interface {
	// SendTrackEvents uploads the events. Implementations must honor the
	// "isolated request" batching contract: an [TrackEventRequest] whose
	// ShouldSendInIsolatedRequest reports true is sent on its own.
	SendTrackEvents(ctx context.Context, events []TrackEventRequest) error
}

// AnalyticsEventsClient is the public analytics entry point. It is DISABLED by
// default: construct with [NewAnalyticsEventsClient] passing analyticsEnabled,
// or use [DisabledClient]. Mirrors Rust `AnalyticsEventsClient`.
type AnalyticsEventsClient struct {
	queue *analyticsEventsQueue
}

// analyticsEventsQueue owns the background reduction goroutine and the dedup
// state. Mirrors Rust `AnalyticsEventsQueue`.
type analyticsEventsQueue struct {
	facts    chan analyticsFact
	uploader Uploader
	ctx      context.Context

	mu                    sync.Mutex
	appUsedEmittedKeys    map[appUsedKey]struct{}
	pluginUsedEmittedKeys map[appUsedKey]struct{}

	wg sync.WaitGroup
}

type appUsedKey struct {
	turnID string
	id     string
}

// NewAnalyticsEventsClient constructs a client. When analyticsEnabled is the
// pointer to false, the client is disabled (no queue is created). When it is nil
// or points to true, a queue and background reducer are created. This matches
// the Rust semantics: `(analytics_enabled != Some(false)).then(...)`.
//
// The returned cancel function stops the background goroutine and drains the
// queue; callers should defer it.
func NewAnalyticsEventsClient(ctx context.Context, analyticsEnabled *bool, uploader Uploader) (*AnalyticsEventsClient, func()) {
	if analyticsEnabled != nil && !*analyticsEnabled {
		return &AnalyticsEventsClient{queue: nil}, func() {}
	}
	q := newAnalyticsEventsQueue(ctx, uploader)
	return &AnalyticsEventsClient{queue: q}, q.shutdown
}

// DisabledClient returns a client that drops all events. Mirrors Rust
// `AnalyticsEventsClient::disabled`.
func DisabledClient() *AnalyticsEventsClient {
	return &AnalyticsEventsClient{queue: nil}
}

func newAnalyticsEventsQueue(ctx context.Context, uploader Uploader) *analyticsEventsQueue {
	q := &analyticsEventsQueue{
		facts:                 make(chan analyticsFact, analyticsEventsQueueSize),
		uploader:              uploader,
		ctx:                   ctx,
		appUsedEmittedKeys:    make(map[appUsedKey]struct{}),
		pluginUsedEmittedKeys: make(map[appUsedKey]struct{}),
	}
	q.wg.Add(1)
	go q.run()
	return q
}

func (q *analyticsEventsQueue) run() {
	defer q.wg.Done()
	reducer := newAnalyticsReducer()
	for fact := range q.facts {
		events := reducer.ingest(fact)
		q.sendTrackEvents(events)
	}
}

func (q *analyticsEventsQueue) sendTrackEvents(events []TrackEventRequest) {
	if len(events) == 0 || q.uploader == nil {
		return
	}
	for _, batch := range trackEventRequestBatches(events) {
		if len(batch) == 0 {
			continue
		}
		// Errors are logged-and-dropped in codex; here we ignore the error to
		// preserve best-effort, never-fail-the-caller behavior.
		_ = q.uploader.SendTrackEvents(q.ctx, batch)
	}
}

// shutdown closes the queue and waits for the reducer goroutine to drain.
func (q *analyticsEventsQueue) shutdown() {
	close(q.facts)
	q.wg.Wait()
}

// trySend enqueues a fact, dropping it if the queue is full (mirrors Rust
// `try_send` which logs a warning and drops on a full queue).
func (q *analyticsEventsQueue) trySend(fact analyticsFact) {
	select {
	case q.facts <- fact:
	default:
		// queue full: drop
	}
}

// shouldEnqueueAppUsed dedups app_used events per (turn, connector). Mirrors
// Rust `should_enqueue_app_used`.
func (q *analyticsEventsQueue) shouldEnqueueAppUsed(tracking TrackEventsContext, app AppInvocation) bool {
	if app.ConnectorID == nil {
		return true
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.appUsedEmittedKeys) >= analyticsEventDedupeMaxKeys {
		q.appUsedEmittedKeys = make(map[appUsedKey]struct{})
	}
	key := appUsedKey{turnID: tracking.TurnID, id: *app.ConnectorID}
	if _, ok := q.appUsedEmittedKeys[key]; ok {
		return false
	}
	q.appUsedEmittedKeys[key] = struct{}{}
	return true
}

// shouldEnqueuePluginUsed dedups plugin_used events per (turn, plugin). Mirrors
// Rust `should_enqueue_plugin_used`.
func (q *analyticsEventsQueue) shouldEnqueuePluginUsed(tracking TrackEventsContext, pluginKey string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pluginUsedEmittedKeys) >= analyticsEventDedupeMaxKeys {
		q.pluginUsedEmittedKeys = make(map[appUsedKey]struct{})
	}
	key := appUsedKey{turnID: tracking.TurnID, id: pluginKey}
	if _, ok := q.pluginUsedEmittedKeys[key]; ok {
		return false
	}
	q.pluginUsedEmittedKeys[key] = struct{}{}
	return true
}

// recordFact enqueues a fact when the client is enabled. Mirrors Rust
// `record_fact`.
func (c *AnalyticsEventsClient) recordFact(fact analyticsFact) {
	if c.queue != nil {
		c.queue.trySend(fact)
	}
}

// Enabled reports whether the client will emit events.
func (c *AnalyticsEventsClient) Enabled() bool {
	return c.queue != nil
}

// TrackSkillInvocations records a batch of skill invocations. Mirrors Rust
// `track_skill_invocations`.
func (c *AnalyticsEventsClient) TrackSkillInvocations(tracking TrackEventsContext, invocations []SkillInvocation) {
	if len(invocations) == 0 {
		return
	}
	c.recordFact(analyticsFact{
		kind: factSkillInvoked,
		skillInvoked: &skillInvokedInput{
			tracking:    tracking,
			invocations: invocations,
		},
	})
}

// TrackHookRun records a hook run. Mirrors Rust `track_hook_run`.
func (c *AnalyticsEventsClient) TrackHookRun(tracking TrackEventsContext, hook HookRunFact) {
	c.recordFact(analyticsFact{
		kind:    factHookRun,
		hookRun: &hookRunInput{tracking: tracking, hook: hook},
	})
}

// TrackTurnTokenUsage records turn token usage. Mirrors Rust
// `track_turn_token_usage`.
func (c *AnalyticsEventsClient) TrackTurnTokenUsage(fact TurnTokenUsageFact) {
	c.recordFact(analyticsFact{
		kind:           factTurnTokenUsage,
		turnTokenUsage: &fact,
	})
}

// TrackGuardianReview records a completed guardian review. Mirrors Rust
// `track_guardian_review`.
func (c *AnalyticsEventsClient) TrackGuardianReview(tracking *GuardianReviewTrackContext, result GuardianReviewAnalyticsResult, completedAtMs uint64) {
	params := tracking.EventParams(result, completedAtMs)
	c.recordFact(analyticsFact{
		kind:           factGuardianReview,
		guardianReview: &params,
	})
}

// TrackAppMentioned records app mentions. Mirrors Rust `track_app_mentioned`.
func (c *AnalyticsEventsClient) TrackAppMentioned(tracking TrackEventsContext, mentions []AppInvocation) {
	if len(mentions) == 0 {
		return
	}
	c.recordFact(analyticsFact{
		kind:         factAppMentioned,
		appMentioned: &appMentionedInput{tracking: tracking, mentions: mentions},
	})
}

// TrackAppUsed records an app use, deduped per turn+connector. Mirrors Rust
// `track_app_used`.
func (c *AnalyticsEventsClient) TrackAppUsed(tracking TrackEventsContext, app AppInvocation) {
	if c.queue == nil {
		return
	}
	if !c.queue.shouldEnqueueAppUsed(tracking, app) {
		return
	}
	c.recordFact(analyticsFact{
		kind:    factAppUsed,
		appUsed: &appUsedInput{tracking: tracking, app: app},
	})
}

// TrackAcceptedLineFingerprints records accepted-line-fingerprint facts.
func (c *AnalyticsEventsClient) TrackAcceptedLineFingerprints(input AcceptedLineFingerprintEventInput) {
	c.recordFact(analyticsFact{
		kind:          factAcceptedLines,
		acceptedLines: &input,
	})
}
