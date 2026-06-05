package cli

import (
	"encoding/json"
	"strings"
)

// jsonDetailValue is a doctor detail value that is either a single string or, for
// a repeated detail label, an ordered list of strings. It mirrors the untagged
// JsonDetailValue enum in doctor.rs: One serializes as a bare string, Many as a
// JSON array, so callers can read the common scalar case directly while repeated
// labels keep every value.
type jsonDetailValue struct {
	// One holds the single value when the label appeared exactly once.
	One *string
	// Many holds the ordered values when the label appeared more than once.
	Many []string
}

// push records an additional value for a label, promoting a One value to a Many
// list on the second occurrence. It mirrors JsonDetailValue::push.
func (v *jsonDetailValue) push(value string) {
	if v.One != nil {
		v.Many = []string{*v.One, value}
		v.One = nil
		return
	}
	v.Many = append(v.Many, value)
}

// MarshalJSON renders One as a bare string and Many as a JSON array, matching the
// untagged serialization of JsonDetailValue.
func (v jsonDetailValue) MarshalJSON() ([]byte, error) {
	if v.One != nil {
		return json.Marshal(*v.One)
	}
	return json.Marshal(v.Many)
}

// structuredJSONDetails converts "label: value" detail strings into a keyed map
// of detail values, mirroring structured_json_details in doctor.rs. Details that
// do not follow the "label: value" convention (no ": " separator or an empty
// label) are returned as notes instead of being silently dropped. Repeated labels
// collapse into an ordered array so the common scalar case stays a plain string.
//
// The returned map preserves insertion-relative key sets; key ordering in the
// final JSON is imposed by the sorted-key map marshaler in doctorCheck.MarshalJSON
// (Go sorts map[string] keys), matching codex's BTreeMap ordering.
func structuredJSONDetails(details []string) (map[string]jsonDetailValue, []string) {
	structured := make(map[string]jsonDetailValue, len(details))
	var notes []string
	for _, detail := range details {
		key, value, ok := strings.Cut(detail, ": ")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			notes = append(notes, detail)
			continue
		}
		existing, seen := structured[key]
		if seen {
			existing.push(value)
			structured[key] = existing
			continue
		}
		v := value
		structured[key] = jsonDetailValue{One: &v}
	}
	return structured, notes
}
