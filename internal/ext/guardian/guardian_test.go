package guardian

import (
	"context"
	"errors"
	"testing"

	"github.com/sqlrush/codexgo/internal/ext/extensionapi"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// hostConfig is a stand-in for the host configuration type the registry is built
// with.
type hostConfig struct{}

// spawnRequest and spawnHandle are the host-owned subagent request/handle types.
type spawnRequest struct{ prompt string }

type spawnHandle struct{ threadID protocol.ThreadID }

type recordingSpawner struct {
	gotThreadID protocol.ThreadID
	gotRequest  spawnRequest
	err         error
}

func (s *recordingSpawner) SpawnSubagent(_ context.Context, forkedFrom protocol.ThreadID, request spawnRequest) (spawnHandle, error) {
	s.gotThreadID = forkedFrom
	s.gotRequest = request
	if s.err != nil {
		return spawnHandle{}, s.err
	}
	return spawnHandle{threadID: forkedFrom}, nil
}

func TestOnThreadStartInsertsContext(t *testing.T) {
	ext := NewGuardianExtension[hostConfig](&recordingSpawner{})
	const threadUUID = "11111111-1111-1111-1111-111111111111"
	store := extensionapi.NewExtensionData(threadUUID)

	ext.OnThreadStart(context.Background(), extensionapi.ThreadStartInput[hostConfig]{
		ThreadStore: store,
	})

	ctx, ok := extensionapi.ExtensionDataGet[GuardianThreadContext](store)
	if !ok {
		t.Fatal("guardian thread context not inserted")
	}
	if ctx.ForkedFromThreadID().String() != threadUUID {
		t.Fatalf("forked-from thread id = %q, want %q", ctx.ForkedFromThreadID(), threadUUID)
	}
}

func TestOnThreadStartEmptyLevelID(t *testing.T) {
	ext := NewGuardianExtension[hostConfig](&recordingSpawner{})
	store := extensionapi.NewExtensionData("")
	ext.OnThreadStart(context.Background(), extensionapi.ThreadStartInput[hostConfig]{
		ThreadStore: store,
	})
	if _, ok := extensionapi.ExtensionDataGet[GuardianThreadContext](store); ok {
		t.Fatal("context should not be inserted for empty level id")
	}
}

func TestSpawnSubagentDelegates(t *testing.T) {
	spawner := &recordingSpawner{}
	ext := NewGuardianExtension[hostConfig](spawner)
	threadID := protocol.NewThreadID("22222222-2222-2222-2222-222222222222")

	handle, err := ext.SpawnSubagent(context.Background(), threadID, spawnRequest{prompt: "review"})
	if err != nil {
		t.Fatalf("SpawnSubagent err = %v", err)
	}
	if handle.threadID != threadID {
		t.Fatalf("handle thread = %v, want %v", handle.threadID, threadID)
	}
	if spawner.gotThreadID != threadID || spawner.gotRequest.prompt != "review" {
		t.Fatalf("spawner saw thread=%v request=%+v", spawner.gotThreadID, spawner.gotRequest)
	}
}

func TestSpawnSubagentPropagatesError(t *testing.T) {
	wantErr := errors.New("spawn failed")
	ext := NewGuardianExtension[hostConfig](&recordingSpawner{err: wantErr})
	_, err := ext.SpawnSubagent(context.Background(), protocol.NewThreadID("t"), spawnRequest{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestInstallRegistersThreadLifecycleContributor(t *testing.T) {
	builder := extensionapi.NewExtensionRegistryBuilder[hostConfig]()
	Install[hostConfig](builder, &recordingSpawner{})
	reg := builder.Build()
	contributors := reg.ThreadLifecycleContributors()
	if len(contributors) != 1 {
		t.Fatalf("registered contributors = %d, want 1", len(contributors))
	}

	// The registered contributor inserts the guardian context on thread start.
	const threadUUID = "33333333-3333-3333-3333-333333333333"
	store := extensionapi.NewExtensionData(threadUUID)
	contributors[0].OnThreadStart(context.Background(), extensionapi.ThreadStartInput[hostConfig]{ThreadStore: store})
	if _, ok := extensionapi.ExtensionDataGet[GuardianThreadContext](store); !ok {
		t.Fatal("installed contributor did not insert guardian context")
	}
}

// guardianExtensionSatisfiesInterface is a compile-time assertion that the
// extension implements the generic thread-lifecycle contributor interface.
var _ extensionapi.ThreadLifecycleContributor[hostConfig] = (*GuardianExtension[hostConfig, spawnRequest, spawnHandle])(nil)

type captureSink struct {
	events []protocol.Event
}

func (c *captureSink) Emit(e protocol.Event) { c.events = append(c.events, e) }

func TestAssessmentEmitterEmitsEvent(t *testing.T) {
	sink := &captureSink{}
	emitter := NewAssessmentEmitter(sink)

	risk := protocol.GuardianRiskHigh
	rationale := "rm -rf detected"
	assessment := protocol.GuardianAssessmentEvent{
		ID:          "assessment-1",
		TurnID:      "turn-1",
		StartedAtMs: 1000,
		Status:      protocol.GuardianStatusDenied,
		RiskLevel:   &risk,
		Rationale:   &rationale,
		Action: protocol.GuardianAssessmentAction{
			Kind:    protocol.GuardianActionCommand,
			Source:  protocol.GuardianCommandSourceShell,
			Command: "rm -rf /",
		},
	}
	emitter.Emit("evt-id", assessment)

	if len(sink.events) != 1 {
		t.Fatalf("emitted events = %d, want 1", len(sink.events))
	}
	got := sink.events[0]
	if got.ID != "evt-id" {
		t.Fatalf("event id = %q, want evt-id", got.ID)
	}
	if got.Msg.Type != protocol.EventMsgKindGuardianAssessment {
		t.Fatalf("event type = %q, want guardian_assessment", got.Msg.Type)
	}
	if got.Msg.GuardianAssessment == nil || got.Msg.GuardianAssessment.ID != "assessment-1" {
		t.Fatalf("guardian assessment payload = %+v", got.Msg.GuardianAssessment)
	}
	if got.Msg.GuardianAssessment.Status != protocol.GuardianStatusDenied {
		t.Fatalf("status = %q, want denied", got.Msg.GuardianAssessment.Status)
	}
}

func TestAssessmentEmitterNilSink(t *testing.T) {
	emitter := NewAssessmentEmitter(nil)
	// Should not panic with a nil sink.
	emitter.Emit("x", protocol.GuardianAssessmentEvent{ID: "a"})
}
