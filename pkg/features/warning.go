package features

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// UnstableFeaturesWarningEvent builds a protocol.Event carrying a WarningEvent
// when under-development features are effectively enabled and the warning is not
// suppressed. It returns (nil, false) when there is nothing to warn about.
// Mirrors the Rust `unstable_features_warning_event`.
//
// effectiveFeatures is the decoded `[features]` table (key -> decoded TOML
// value); only entries whose value is the boolean `true` are considered, and
// only those that resolve to an under-development feature that is actually
// enabled in `features` are reported.
func UnstableFeaturesWarningEvent(
	effectiveFeatures map[string]any,
	suppressUnstableFeaturesWarning bool,
	features *Features,
	configPath string,
) (*protocol.Event, bool) {
	if suppressUnstableFeaturesWarning {
		return nil, false
	}

	var underDevelopmentKeys []string
	if effectiveFeatures != nil {
		// Iterate in sorted key order to match the Rust BTreeMap iteration so
		// the joined message is deterministic.
		keys := make([]string, 0, len(effectiveFeatures))
		for key := range effectiveFeatures {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			b, ok := effectiveFeatures[key].(bool)
			if !ok || !b {
				continue
			}
			spec, ok := specByKey(key)
			if !ok {
				continue
			}
			if !features.Enabled(spec.ID) {
				continue
			}
			if spec.Stage.Kind == StageUnderDevelopment {
				underDevelopmentKeys = append(underDevelopmentKeys, spec.Key)
			}
		}
	}

	if len(underDevelopmentKeys) == 0 {
		return nil, false
	}

	joined := strings.Join(underDevelopmentKeys, ", ")
	message := fmt.Sprintf(
		"Under-development features enabled: %s. Under-development features are incomplete and may behave unpredictably. To suppress this warning, set `suppress_unstable_features_warning = true` in %s.",
		joined, configPath,
	)

	event := &protocol.Event{
		ID: "",
		Msg: protocol.EventMsg{
			Type:    protocol.EventMsgKindWarning,
			Warning: &protocol.WarningEvent{Message: message},
		},
	}
	return event, true
}
