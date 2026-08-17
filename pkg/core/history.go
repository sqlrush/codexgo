package core

import (
	"sync"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// ConversationHistory is the thread-safe, session-scoped record of conversation
// items shared across every turn. It is the engine-facing analogue of the Rust
// `ContextManager`'s item store: an ordered list of [protocol.ResponseItem]s
// plus the latest token-usage snapshot.
//
// Immutability contract: accessors return COPIES of the underlying slices so
// callers can never mutate shared state in place. Mutators take a lock and
// append/replace the internal slice, mirroring the "return a new copy" rule for
// observers while keeping a single owned store inside the session.
//
// STUB: history truncation policy, reference-context-item tracking, and the
// token-usage breakdown by category are deferred to the context-manager area
// agent. Only the core record/clone/replace and token aggregation needed by the
// turn path are implemented here.
type ConversationHistory struct {
	mu        sync.RWMutex
	items     []protocol.ResponseItem
	tokenInfo *protocol.TokenUsageInfo
}

// NewConversationHistory returns an empty history.
func NewConversationHistory() *ConversationHistory {
	return &ConversationHistory{}
}

// RecordItems appends items to the history. The supplied slice is not retained;
// each element is copied into the internal store.
func (h *ConversationHistory) RecordItems(items []protocol.ResponseItem) {
	if len(items) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.items = append(h.items, items...)
}

// Items returns a copy of the current history in order.
func (h *ConversationHistory) Items() []protocol.ResponseItem {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return cloneItems(h.items)
}

// Len reports the number of items currently in history.
func (h *ConversationHistory) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.items)
}

// Replace swaps the entire history for a new ordered set of items. This is used
// by compaction and rollout reconstruction. The supplied slice is copied.
func (h *ConversationHistory) Replace(items []protocol.ResponseItem) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.items = cloneItems(items)
}

// Clone returns a deep-ish copy of the history (its item slice and token info)
// suitable for snapshotting before a turn mutates it.
func (h *ConversationHistory) Clone() *ConversationHistory {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := &ConversationHistory{items: cloneItems(h.items)}
	if h.tokenInfo != nil {
		info := *h.tokenInfo
		out.tokenInfo = &info
	}
	return out
}

// SetTokenInfo records the latest token-usage snapshot.
func (h *ConversationHistory) SetTokenInfo(info *protocol.TokenUsageInfo) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if info == nil {
		h.tokenInfo = nil
		return
	}
	cp := *info
	h.tokenInfo = &cp
}

// TokenInfo returns a copy of the latest token-usage snapshot, or nil.
func (h *ConversationHistory) TokenInfo() *protocol.TokenUsageInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.tokenInfo == nil {
		return nil
	}
	cp := *h.tokenInfo
	return &cp
}

// UpdateTokenInfoFromUsage folds a single turn's usage into the running total,
// mirroring the Rust `ContextManager::update_token_info`. It sums the per-field
// counts into TotalTokenUsage, records the latest turn under LastTokenUsage, and
// stamps the model context window when provided.
func (h *ConversationHistory) UpdateTokenInfoFromUsage(usage protocol.TokenUsage, modelContextWindow *int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	prev := protocol.TokenUsage{}
	if h.tokenInfo != nil {
		prev = h.tokenInfo.TotalTokenUsage
	}
	total := addTokenUsage(prev, usage)
	window := modelContextWindow
	if window == nil && h.tokenInfo != nil {
		window = h.tokenInfo.ModelContextWindow
	}
	h.tokenInfo = &protocol.TokenUsageInfo{
		TotalTokenUsage:    total,
		LastTokenUsage:     usage,
		ModelContextWindow: window,
	}
}

// cloneItems returns a shallow copy of an item slice. ResponseItem values are
// copied by value; their nested slices are shared, which is safe because core
// never mutates an item after recording it (new turns append new items).
func cloneItems(in []protocol.ResponseItem) []protocol.ResponseItem {
	if len(in) == 0 {
		return nil
	}
	out := make([]protocol.ResponseItem, len(in))
	copy(out, in)
	return out
}

// addTokenUsage returns the element-wise sum of two token-usage records.
func addTokenUsage(a, b protocol.TokenUsage) protocol.TokenUsage {
	return protocol.TokenUsage{
		InputTokens:           a.InputTokens + b.InputTokens,
		CachedInputTokens:     a.CachedInputTokens + b.CachedInputTokens,
		OutputTokens:          a.OutputTokens + b.OutputTokens,
		ReasoningOutputTokens: a.ReasoningOutputTokens + b.ReasoningOutputTokens,
		TotalTokens:           a.TotalTokens + b.TotalTokens,
	}
}

// isModelGeneratedItem mirrors the Rust `is_model_generated_item`: items the
// model produced (assistant messages, reasoning, tool calls, compaction
// markers) as opposed to inputs the client added.
func isModelGeneratedItem(item protocol.ResponseItem) bool {
	switch item.Type {
	case protocol.ResponseItemKindMessage:
		return item.Role == "assistant"
	case protocol.ResponseItemKindReasoning, protocol.ResponseItemKindFunctionCall, protocol.ResponseItemKindToolSearchCall,
		protocol.ResponseItemKindWebSearchCall, protocol.ResponseItemKindImageGenerationCall, protocol.ResponseItemKindCustomToolCall,
		protocol.ResponseItemKindLocalShellCall, protocol.ResponseItemKindCompaction, protocol.ResponseItemKindContextCompaction:
		return true
	default:
		return false
	}
}

// itemsAfterLastModelGeneratedItem returns the trailing items the model has not
// yet seen (recorded after its last output). The caller must hold h.mu.
func (h *ConversationHistory) itemsAfterLastModelGeneratedItem() []protocol.ResponseItem {
	start := len(h.items)
	for i := len(h.items) - 1; i >= 0; i-- {
		if isModelGeneratedItem(h.items[i]) {
			start = i + 1
			break
		}
	}
	return h.items[start:]
}

// GetTotalTokenUsage returns the tokens in the active context window: the last
// request's total plus, when the server already accounts past reasoning
// (serverReasoningIncluded), an estimate for items recorded after the model's
// last output. Mirrors the Rust `ContextManager::get_total_token_usage`.
func (h *ConversationHistory) GetTotalTokenUsage(serverReasoningIncluded bool) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	var last int64
	if h.tokenInfo != nil {
		last = h.tokenInfo.LastTokenUsage.TotalTokens
	}
	if !serverReasoningIncluded {
		return last
	}
	return last + approxHistoryTokens(h.itemsAfterLastModelGeneratedItem())
}
