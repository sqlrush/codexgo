package modelsmanager

import (
	"context"
	"testing"
)

func TestOfflineModelInfoWithoutToolOutputOverride(t *testing.T) {
	ctx := context.Background()
	config := &ModelsManagerConfig{}
	manager := newOpenAIManagerForTests(t, newFakeEndpoint(nil))

	info := manager.GetModelInfo(ctx, "gpt-5.2", config)
	if info.TruncationPolicy != TruncationPolicyBytes(10_000) {
		t.Fatalf("expected bytes(10000), got %+v", info.TruncationPolicy)
	}
}

func TestOfflineModelInfoWithToolOutputOverride(t *testing.T) {
	ctx := context.Background()
	limit := 123
	config := &ModelsManagerConfig{ToolOutputTokenLimit: &limit}
	manager := newOpenAIManagerForTests(t, newFakeEndpoint(nil))

	info := manager.GetModelInfo(ctx, "gpt-5.4", config)
	if info.TruncationPolicy != TruncationPolicyTokens(123) {
		t.Fatalf("expected tokens(123), got %+v", info.TruncationPolicy)
	}
}
