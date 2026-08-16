package rollout

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// MarshalJSON flattens the SessionMeta fields and appends the optional `git`
// field, mirroring the Rust `#[serde(flatten)]` on `meta`.
func (l SessionMetaLine) MarshalJSON() ([]byte, error) {
	metaJSON, err := json.Marshal(l.Meta)
	if err != nil {
		return nil, fmt.Errorf("marshal session meta: %w", err)
	}
	if l.Git == nil {
		return metaJSON, nil
	}

	gitJSON, err := json.Marshal(l.Git)
	if err != nil {
		return nil, fmt.Errorf("marshal git info: %w", err)
	}

	// metaJSON is a JSON object "{...}". Insert "git":... before the closing
	// brace. An empty object "{}" has no preceding comma.
	trimmed := bytes.TrimSpace(metaJSON)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return nil, fmt.Errorf("session meta did not marshal to a JSON object")
	}

	var buf bytes.Buffer
	buf.Write(trimmed[:len(trimmed)-1])
	if len(bytes.TrimSpace(trimmed[1:len(trimmed)-1])) > 0 {
		buf.WriteByte(',')
	}
	buf.WriteString(`"git":`)
	buf.Write(gitJSON)
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON decodes the flattened SessionMeta fields plus the optional
// `git` field.
func (l *SessionMetaLine) UnmarshalJSON(data []byte) error {
	var meta SessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return fmt.Errorf("decode session meta: %w", err)
	}
	var gitWrapper struct {
		Git *protocol.GitInfo `json:"git"`
	}
	if err := json.Unmarshal(data, &gitWrapper); err != nil {
		return fmt.Errorf("decode git info: %w", err)
	}
	l.Meta = meta
	l.Git = gitWrapper.Git
	return nil
}
