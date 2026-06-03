package otel

import (
	"fmt"
	"sort"
)

// validateTags validates every key/value pair. Mirrors Rust `validate_tags`.
func validateTags(tags map[string]string) error {
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := ValidateTagKey(k); err != nil {
			return err
		}
		if err := ValidateTagValue(tags[k]); err != nil {
			return err
		}
	}
	return nil
}

// ValidateMetricName validates a metric name. Mirrors Rust
// `validate_metric_name`.
func ValidateMetricName(name string) error {
	if name == "" {
		return ErrEmptyMetricName
	}
	for _, c := range name {
		if !isMetricChar(c) {
			return fmt.Errorf("%w: %s", errInvalidMetricName, name)
		}
	}
	return nil
}

// ValidateTagKey validates a tag key. Mirrors Rust `validate_tag_key`.
func ValidateTagKey(key string) error {
	return validateTagComponent(key, "tag key")
}

// ValidateTagValue validates a tag value. Mirrors Rust `validate_tag_value`.
func ValidateTagValue(value string) error {
	return validateTagComponent(value, "tag value")
}

func validateTagComponent(value, label string) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", label)
	}
	for _, c := range value {
		if !isTagChar(c) {
			return fmt.Errorf("%s contains invalid characters: %s", label, value)
		}
	}
	return nil
}

func isMetricChar(c rune) bool {
	return isASCIIAlphanumeric(c) || c == '.' || c == '_' || c == '-'
}

func isTagChar(c rune) bool {
	return isASCIIAlphanumeric(c) || c == '.' || c == '_' || c == '-' || c == '/'
}
