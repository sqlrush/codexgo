package multiagent

import (
	"math"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
)

// threadSpawnSource builds a thread-spawn session source for tests.
func threadSpawnSource(parent protocol.ThreadID, depth int32, path *protocol.AgentPath, nickname, role *string) rollout.SessionSource {
	return rollout.SessionSource{
		Kind: rollout.SessionSourceKindSubAgent,
		SubAgent: &rollout.SubAgentSource{
			Kind: rollout.SubAgentSourceKindThreadSpawn,
			ThreadSpawn: &rollout.ThreadSpawnSource{
				ParentThreadID: parent,
				Depth:          depth,
				AgentPath:      path,
				AgentNickname:  nickname,
				AgentRole:      role,
			},
		},
	}
}

func TestNextThreadSpawnDepth(t *testing.T) {
	tests := []struct {
		name   string
		source rollout.SessionSource
		want   int32
	}{
		{name: "cli source starts at one", source: rollout.NewCliSource(), want: 1},
		{name: "thread spawn increments", source: threadSpawnSource(tid(1), 3, nil, nil, nil), want: 4},
		{name: "saturates at max int32", source: threadSpawnSource(tid(1), math.MaxInt32, nil, nil, nil), want: math.MaxInt32},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextThreadSpawnDepth(tt.source); got != tt.want {
				t.Fatalf("NextThreadSpawnDepth = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExceedsThreadSpawnDepthLimit(t *testing.T) {
	tests := []struct {
		depth, max int32
		want       bool
	}{
		{depth: 1, max: 3, want: false},
		{depth: 3, max: 3, want: false},
		{depth: 4, max: 3, want: true},
	}
	for _, tt := range tests {
		if got := ExceedsThreadSpawnDepthLimit(tt.depth, tt.max); got != tt.want {
			t.Fatalf("ExceedsThreadSpawnDepthLimit(%d,%d) = %v, want %v", tt.depth, tt.max, got, tt.want)
		}
	}
}

func TestThreadSpawnParentThreadID(t *testing.T) {
	parent := tid(42)
	if got := threadSpawnParentThreadID(threadSpawnSource(parent, 2, nil, nil, nil)); got == nil || *got != parent {
		t.Fatalf("parent = %v, want %v", got, parent)
	}
	if got := threadSpawnParentThreadID(rollout.NewCliSource()); got != nil {
		t.Fatalf("non-spawn source parent = %v, want nil", got)
	}
}

func TestAgentMetadataCloneIsDeep(t *testing.T) {
	id := tid(1)
	path := mustPath(t, "/root/x")
	md := AgentMetadata{
		AgentID:         &id,
		AgentPath:       &path,
		AgentNickname:   strptr("Euclid"),
		AgentRole:       strptr("reviewer"),
		LastTaskMessage: strptr("task"),
	}
	cp := md.clone()
	*cp.AgentNickname = "changed"
	*cp.LastTaskMessage = "changed"
	if *md.AgentNickname != "Euclid" || *md.LastTaskMessage != "task" {
		t.Fatalf("clone mutated original: %+v", md)
	}
}
