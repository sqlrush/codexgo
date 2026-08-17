package modelsmanager

import (
	"strings"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// nonNilStrings returns the input slice, or an empty (non-nil) slice when nil so
// that JSON serialization emits `[]` rather than `null`, matching Rust's
// serialization of an empty `Vec<String>`.
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// nonNilPresets returns the input slice, or an empty (non-nil) slice when nil.
func nonNilPresets(in []ReasoningEffortPreset) []ReasoningEffortPreset {
	if in == nil {
		return []ReasoningEffortPreset{}
	}
	return in
}

// nonNilTiers returns the input slice, or an empty (non-nil) slice when nil.
func nonNilTiers(in []ModelServiceTier) []ModelServiceTier {
	if in == nil {
		return []ModelServiceTier{}
	}
	return in
}

// nonNilModalities returns the input slice, or an empty (non-nil) slice when
// nil.
func nonNilModalities(in []protocol.InputModality) []protocol.InputModality {
	if in == nil {
		return []protocol.InputModality{}
	}
	return in
}

// cloneStringPtr returns a copy of the pointed-to string, or nil when src is
// nil. Returning a fresh pointer preserves immutability of the source.
func cloneStringPtr(src *string) *string {
	if src == nil {
		return nil
	}
	value := *src
	return &value
}

// containsSubstring reports whether s contains substr.
func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}

// replaceAll replaces every occurrence of old with replacement in s.
func replaceAll(s, old, replacement string) string {
	return strings.ReplaceAll(s, old, replacement)
}

// strPtr returns a pointer to the provided string. Useful for constructing
// optional fields.
func strPtr(s string) *string {
	return &s
}

// int64Ptr returns a pointer to the provided int64.
func int64Ptr(v int64) *int64 {
	return &v
}
