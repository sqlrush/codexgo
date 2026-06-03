package cloudreq

import "encoding/json"

// unmarshalStrictish decodes JSON like serde_json::from_slice (lenient about
// extra fields). It exists as a named seam so the cache decode path is explicit.
func unmarshalStrictish(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// marshalPretty serializes with two-space indentation, mirroring
// serde_json::to_vec_pretty used when writing the cache file.
func marshalPretty(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
