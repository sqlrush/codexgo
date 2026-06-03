package otel

// Default metric tag keys. Mirrors codex-rs/otel/src/metrics/tags.rs.
const (
	AppVersionTag    = "app.version"
	AuthModeTag      = "auth_mode"
	ModelTag         = "model"
	OriginatorTag    = "originator"
	ServiceNameTag   = "service_name"
	SessionSourceTag = "session_source"
)

const otherOriginatorTagValue = "other"

var knownOriginatorTagValues = []string{
	"codex_desktop",
	"codex-app-server",
	"codex_mcp_server",
	"codex_cli_rs",
	"codex-tui",
	"codex_vscode",
	"none",
	"codex_exec",
	"codex-cli",
	"codex_sdk_ts",
	"codex-app-server-sdk",
}

// BoundedOriginatorTagValue returns a known low-cardinality originator tag
// value, or "other". Mirrors Rust `bounded_originator_tag_value`.
func BoundedOriginatorTagValue(originator string) string {
	sanitized := SanitizeMetricTagValue(originator)
	for _, known := range knownOriginatorTagValues {
		if known == sanitized {
			return known
		}
	}
	return otherOriginatorTagValue
}

// Tag is a single metric tag key/value pair. Go has no borrowed tuple, so the
// ordered slice of [Tag] replaces the Rust Vec<(&str, &str)>.
type Tag struct {
	Key   string
	Value string
}

// SessionMetricTagValues collects the per-session default tag values. Mirrors
// Rust `SessionMetricTagValues`. Optional values are nil pointers.
type SessionMetricTagValues struct {
	AuthMode      *string
	SessionSource string
	Originator    string
	ServiceName   *string
	Model         string
	AppVersion    string
}

// IntoTags produces the ordered, validated tag list. Mirrors Rust
// `SessionMetricTagValues::into_tags`. The order is fixed:
// auth_mode, session_source, originator, service_name, model, app.version.
func (s SessionMetricTagValues) IntoTags() ([]Tag, error) {
	tags := make([]Tag, 0, 6)
	push := func(key string, value *string) error {
		if value == nil {
			return nil
		}
		if err := ValidateTagKey(key); err != nil {
			return err
		}
		if err := ValidateTagValue(*value); err != nil {
			return err
		}
		tags = append(tags, Tag{Key: key, Value: *value})
		return nil
	}

	if err := push(AuthModeTag, s.AuthMode); err != nil {
		return nil, err
	}
	if err := push(SessionSourceTag, &s.SessionSource); err != nil {
		return nil, err
	}
	if err := push(OriginatorTag, &s.Originator); err != nil {
		return nil, err
	}
	if err := push(ServiceNameTag, s.ServiceName); err != nil {
		return nil, err
	}
	if err := push(ModelTag, &s.Model); err != nil {
		return nil, err
	}
	if err := push(AppVersionTag, &s.AppVersion); err != nil {
		return nil, err
	}
	return tags, nil
}
