package appserverproto

import (
	"encoding/json"
	"fmt"
)

// MessageKind discriminates the four JSON-RPC envelope shapes.
type MessageKind int

const (
	// MessageKindUnknown is the zero value; never produced by a successful decode.
	MessageKindUnknown MessageKind = iota
	// MessageKindRequest is a JSONRPCRequest (has id + method).
	MessageKindRequest
	// MessageKindNotification is a JSONRPCNotification (has method, no id).
	MessageKindNotification
	// MessageKindResponse is a JSONRPCResponse (has id + result).
	MessageKindResponse
	// MessageKindError is a JSONRPCError (has id + error).
	MessageKindError
)

// JSONRPCMessage is any valid JSON-RPC object on the wire. Mirrors the Rust
// untagged enum JSONRPCMessage { Request, Notification, Response, Error }.
//
// Because the Rust enum is untagged, decoding is structural: we inspect which
// keys are present to choose the variant. Exactly one of the pointer fields is
// non-nil after a successful UnmarshalJSON, and Kind reports which.
type JSONRPCMessage struct {
	Kind         MessageKind
	Request      *JSONRPCRequest
	Notification *JSONRPCNotification
	Response     *JSONRPCResponse
	Error        *JSONRPCError
}

// NewRequestMessage wraps a request envelope.
func NewRequestMessage(r JSONRPCRequest) JSONRPCMessage {
	return JSONRPCMessage{Kind: MessageKindRequest, Request: &r}
}

// NewNotificationMessage wraps a notification envelope.
func NewNotificationMessage(n JSONRPCNotification) JSONRPCMessage {
	return JSONRPCMessage{Kind: MessageKindNotification, Notification: &n}
}

// NewResponseMessage wraps a response envelope.
func NewResponseMessage(r JSONRPCResponse) JSONRPCMessage {
	return JSONRPCMessage{Kind: MessageKindResponse, Response: &r}
}

// NewErrorMessage wraps an error envelope.
func NewErrorMessage(e JSONRPCError) JSONRPCMessage {
	return JSONRPCMessage{Kind: MessageKindError, Error: &e}
}

// MarshalJSON emits the wrapped envelope directly (untagged).
func (m JSONRPCMessage) MarshalJSON() ([]byte, error) {
	switch m.Kind {
	case MessageKindRequest:
		if m.Request == nil {
			return nil, fmt.Errorf("appserverproto: JSONRPCMessage request variant has nil payload")
		}
		return json.Marshal(m.Request)
	case MessageKindNotification:
		if m.Notification == nil {
			return nil, fmt.Errorf("appserverproto: JSONRPCMessage notification variant has nil payload")
		}
		return json.Marshal(m.Notification)
	case MessageKindResponse:
		if m.Response == nil {
			return nil, fmt.Errorf("appserverproto: JSONRPCMessage response variant has nil payload")
		}
		return json.Marshal(m.Response)
	case MessageKindError:
		if m.Error == nil {
			return nil, fmt.Errorf("appserverproto: JSONRPCMessage error variant has nil payload")
		}
		return json.Marshal(m.Error)
	default:
		return nil, fmt.Errorf("appserverproto: JSONRPCMessage has unknown kind %d", m.Kind)
	}
}

// messageProbe captures the key presence used to classify an untagged message.
type messageProbe struct {
	ID     json.RawMessage `json:"id"`
	Method json.RawMessage `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

// UnmarshalJSON classifies the envelope by inspecting which keys are present,
// then decodes into the matching concrete struct.
//
// Classification order mirrors the JSON shapes distinctly:
//   - method present + id present  => Request
//   - method present + id absent   => Notification
//   - error present                => Error
//   - result present               => Response
func (m *JSONRPCMessage) UnmarshalJSON(data []byte) error {
	var probe messageProbe
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("appserverproto: classify JSONRPCMessage: %w", err)
	}

	hasMethod := len(probe.Method) > 0
	hasID := len(probe.ID) > 0
	hasResult := len(probe.Result) > 0
	hasError := len(probe.Error) > 0

	switch {
	case hasMethod && hasID:
		var req JSONRPCRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return fmt.Errorf("appserverproto: decode JSONRPCRequest: %w", err)
		}
		*m = JSONRPCMessage{Kind: MessageKindRequest, Request: &req}
		return nil
	case hasMethod:
		var note JSONRPCNotification
		if err := json.Unmarshal(data, &note); err != nil {
			return fmt.Errorf("appserverproto: decode JSONRPCNotification: %w", err)
		}
		*m = JSONRPCMessage{Kind: MessageKindNotification, Notification: &note}
		return nil
	case hasError:
		var rpcErr JSONRPCError
		if err := json.Unmarshal(data, &rpcErr); err != nil {
			return fmt.Errorf("appserverproto: decode JSONRPCError: %w", err)
		}
		*m = JSONRPCMessage{Kind: MessageKindError, Error: &rpcErr}
		return nil
	case hasResult:
		var resp JSONRPCResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("appserverproto: decode JSONRPCResponse: %w", err)
		}
		*m = JSONRPCMessage{Kind: MessageKindResponse, Response: &resp}
		return nil
	default:
		return fmt.Errorf("appserverproto: JSONRPCMessage does not match any variant: %s", string(data))
	}
}
