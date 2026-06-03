package analytics

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// MarshalJSON serializes a [GuardianReviewEventPayload] with the guardian
// review params flattened at the top level (Rust `#[serde(flatten)]`). The
// session_id, app_server_client and runtime fields appear first, followed by
// the flattened guardian review fields.
func (p GuardianReviewEventPayload) MarshalJSON() ([]byte, error) {
	type header struct {
		SessionID       string                       `json:"session_id"`
		AppServerClient CodexAppServerClientMetadata `json:"app_server_client"`
		Runtime         CodexRuntimeMetadata         `json:"runtime"`
	}
	headerBytes, err := json.Marshal(header{
		SessionID:       p.SessionID,
		AppServerClient: p.AppServerClient,
		Runtime:         p.Runtime,
	})
	if err != nil {
		return nil, fmt.Errorf("analytics: marshal guardian payload header: %w", err)
	}
	reviewBytes, err := json.Marshal(p.GuardianReview)
	if err != nil {
		return nil, fmt.Errorf("analytics: marshal guardian review: %w", err)
	}
	return mergeJSONObjects(headerBytes, reviewBytes)
}

// mergeJSONObjects concatenates the fields of two JSON objects into one,
// preserving the order of a then b. b's fields override a's on key collision is
// not expected here (the Rust structs have disjoint keys), but a wins to match
// serde flatten semantics where outer fields are written first.
func mergeJSONObjects(a, b []byte) ([]byte, error) {
	a = bytes.TrimSpace(a)
	b = bytes.TrimSpace(b)
	if !bytes.HasPrefix(a, []byte("{")) || !bytes.HasSuffix(a, []byte("}")) {
		return nil, fmt.Errorf("analytics: cannot merge non-object JSON: %s", a)
	}
	if !bytes.HasPrefix(b, []byte("{")) || !bytes.HasSuffix(b, []byte("}")) {
		return nil, fmt.Errorf("analytics: cannot merge non-object JSON: %s", b)
	}
	aInner := bytes.TrimSpace(a[1 : len(a)-1])
	bInner := bytes.TrimSpace(b[1 : len(b)-1])

	var buf bytes.Buffer
	buf.WriteByte('{')
	buf.Write(aInner)
	if len(aInner) > 0 && len(bInner) > 0 {
		buf.WriteByte(',')
	}
	buf.Write(bInner)
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
