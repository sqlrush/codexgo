package appserverproto

import (
	"encoding/json"
	"fmt"
)

// CodexErrorInfoV2Kind discriminates the v2 API codex error variants. This is
// the camelCase API-v2 mirror of the internal protocol CodexErrorInfo (which is
// snake_case); the two are distinct wire types.
//
// Rust: v2::shared::CodexErrorInfo, serde rename_all = "camelCase",
// externally-tagged. Unit variants serialize as bare strings; struct variants
// serialize as {"<variant>": {<fields>}}.
type CodexErrorInfoV2Kind string

const (
	CodexErrorInfoV2ContextWindowExceeded         CodexErrorInfoV2Kind = "contextWindowExceeded"
	CodexErrorInfoV2UsageLimitExceeded            CodexErrorInfoV2Kind = "usageLimitExceeded"
	CodexErrorInfoV2ServerOverloaded              CodexErrorInfoV2Kind = "serverOverloaded"
	CodexErrorInfoV2CyberPolicy                   CodexErrorInfoV2Kind = "cyberPolicy"
	CodexErrorInfoV2HTTPConnectionFailed          CodexErrorInfoV2Kind = "httpConnectionFailed"
	CodexErrorInfoV2ResponseStreamConnectionFail  CodexErrorInfoV2Kind = "responseStreamConnectionFailed"
	CodexErrorInfoV2InternalServerError           CodexErrorInfoV2Kind = "internalServerError"
	CodexErrorInfoV2Unauthorized                  CodexErrorInfoV2Kind = "unauthorized"
	CodexErrorInfoV2BadRequest                    CodexErrorInfoV2Kind = "badRequest"
	CodexErrorInfoV2ThreadRollbackFailed          CodexErrorInfoV2Kind = "threadRollbackFailed"
	CodexErrorInfoV2SandboxError                  CodexErrorInfoV2Kind = "sandboxError"
	CodexErrorInfoV2ResponseStreamDisconnected    CodexErrorInfoV2Kind = "responseStreamDisconnected"
	CodexErrorInfoV2ResponseTooManyFailedAttempts CodexErrorInfoV2Kind = "responseTooManyFailedAttempts"
	CodexErrorInfoV2ActiveTurnNotSteerable        CodexErrorInfoV2Kind = "activeTurnNotSteerable"
	CodexErrorInfoV2Other                         CodexErrorInfoV2Kind = "other"
)

// CodexErrorInfoV2 is the v2 API error-info union. The Kind selects the variant;
// HTTPStatusCode is populated for the four connection/stream variants, and
// TurnKind for the activeTurnNotSteerable variant.
//
// The struct variants carry an Option<u16> http_status_code (always emitted as
// the "httpStatusCode" key, even when null, matching serde's lack of
// skip_serializing_if), or a NonSteerableTurnKind under "turnKind".
type CodexErrorInfoV2 struct {
	Kind CodexErrorInfoV2Kind

	// HTTPStatusCode applies to the connection/stream struct variants. It is a
	// pointer so the null vs value distinction is preserved on the wire.
	HTTPStatusCode *uint16
	// TurnKind applies only to the activeTurnNotSteerable variant.
	TurnKind *NonSteerableTurnKind
}

// httpStatusBody is the payload for the connection/stream variants.
type httpStatusBody struct {
	HTTPStatusCode *uint16 `json:"httpStatusCode"`
}

// turnKindBody is the payload for the activeTurnNotSteerable variant.
type turnKindBody struct {
	TurnKind NonSteerableTurnKind `json:"turnKind"`
}

// isV2HTTPStatusVariant reports whether the kind carries an httpStatusCode body.
func isV2HTTPStatusVariant(kind CodexErrorInfoV2Kind) bool {
	switch kind {
	case CodexErrorInfoV2HTTPConnectionFailed,
		CodexErrorInfoV2ResponseStreamConnectionFail,
		CodexErrorInfoV2ResponseStreamDisconnected,
		CodexErrorInfoV2ResponseTooManyFailedAttempts:
		return true
	default:
		return false
	}
}

// MarshalJSON encodes the externally-tagged v2 error info.
func (c CodexErrorInfoV2) MarshalJSON() ([]byte, error) {
	switch {
	case isV2HTTPStatusVariant(c.Kind):
		return json.Marshal(map[string]httpStatusBody{
			string(c.Kind): {HTTPStatusCode: c.HTTPStatusCode},
		})
	case c.Kind == CodexErrorInfoV2ActiveTurnNotSteerable:
		tk := NonSteerableTurnKindReview
		if c.TurnKind != nil {
			tk = *c.TurnKind
		}
		return json.Marshal(map[string]turnKindBody{
			string(c.Kind): {TurnKind: tk},
		})
	case c.Kind == "":
		return nil, fmt.Errorf("appserverproto: CodexErrorInfoV2 has empty kind")
	default:
		// Unit variant: bare string.
		return json.Marshal(string(c.Kind))
	}
}

// UnmarshalJSON decodes both the bare-string and tagged-object forms.
func (c *CodexErrorInfoV2) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*c = CodexErrorInfoV2{Kind: CodexErrorInfoV2Kind(s)}
		return nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	if len(obj) != 1 {
		return fmt.Errorf("appserverproto: CodexErrorInfoV2 object must have exactly one key, got %d", len(obj))
	}
	for key, raw := range obj {
		kind := CodexErrorInfoV2Kind(key)
		switch {
		case isV2HTTPStatusVariant(kind):
			var body httpStatusBody
			if err := json.Unmarshal(raw, &body); err != nil {
				return err
			}
			*c = CodexErrorInfoV2{Kind: kind, HTTPStatusCode: body.HTTPStatusCode}
			return nil
		case kind == CodexErrorInfoV2ActiveTurnNotSteerable:
			var body turnKindBody
			if err := json.Unmarshal(raw, &body); err != nil {
				return err
			}
			tk := body.TurnKind
			*c = CodexErrorInfoV2{Kind: kind, TurnKind: &tk}
			return nil
		default:
			return fmt.Errorf("appserverproto: unexpected struct payload for CodexErrorInfoV2 variant %q", key)
		}
	}
	return fmt.Errorf("appserverproto: empty CodexErrorInfoV2 object")
}
