package hooks

import (
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// HookKey builds the persisted config-state key for one discovered hook handler.
// Mirrors hook_key: "{key_source}:{event_label}:{group_index}:{handler_index}".
func HookKey(keySource string, eventName protocol.HookEventName, groupIndex, handlerIndex int) string {
	return fmt.Sprintf("%s:%s:%d:%d", keySource, HookEventKeyLabel(eventName), groupIndex, handlerIndex)
}

// MergeHookStates folds per-layer hook-state maps into one effective map. Layers
// are supplied lowest-precedence first; later layers win field-by-field so a
// later partial state write does not erase an existing override. Keys are
// trimmed, and empty keys are dropped. Mirrors hook_states_from_stack's
// field-by-field merge (the layer-source filtering it performs over a
// ConfigLayerStack is the caller's responsibility here, since that stack type
// lives outside this package's dependencies).
func MergeHookStates(layers []map[string]config.HookStateToml) map[string]config.HookStateToml {
	states := make(map[string]config.HookStateToml)
	for _, layer := range layers {
		for rawKey, state := range layer {
			key := strings.TrimSpace(rawKey)
			if key == "" {
				continue
			}
			effective := states[key]
			if state.Enabled != nil {
				enabled := *state.Enabled
				effective.Enabled = &enabled
			}
			if state.TrustedHash != nil {
				hash := *state.TrustedHash
				effective.TrustedHash = &hash
			}
			states[key] = effective
		}
	}
	return states
}

// HookEnabled reports whether a hook handler is enabled given its managed status
// and persisted state. Managed hooks are always enabled; otherwise a hook is
// enabled unless its state explicitly sets enabled=false. Mirrors hook_enabled.
func HookEnabled(isManaged bool, state *config.HookStateToml) bool {
	if isManaged {
		return true
	}
	if state == nil || state.Enabled == nil {
		return true
	}
	return *state.Enabled
}

// HookTrustedHash returns the persisted trusted hash for a non-managed hook, or
// nil for managed hooks or when no hash is stored. Mirrors hook_trusted_hash.
func HookTrustedHash(isManaged bool, state *config.HookStateToml) *string {
	if isManaged || state == nil {
		return nil
	}
	return state.TrustedHash
}

// HookTrustStatus computes the trust status for a hook. Managed hooks are
// Managed; otherwise the status compares the current hash to any stored trusted
// hash. Mirrors hook_trust_status.
func HookTrustStatus(isManaged bool, currentHash string, trustedHash *string) protocol.HookTrustStatus {
	if isManaged {
		return protocol.HookTrustStatusManaged
	}
	if trustedHash == nil {
		return protocol.HookTrustStatusUntrusted
	}
	if *trustedHash == currentHash {
		return protocol.HookTrustStatusTrusted
	}
	return protocol.HookTrustStatusModified
}
