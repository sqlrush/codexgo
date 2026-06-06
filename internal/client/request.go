// Package client is a faithful Go port of the Rust `codex-client` crate.
//
// It provides the HTTP transport layer used by higher-level Codex API clients:
// request building, body preparation (including optional zstd compression),
// a retry policy with codex defaults, custom-CA handling driven by the
// CODEXGO_CA_CERTIFICATE / SSL_CERT_FILE environment variables, ChatGPT
// Cloudflare cookie handling, and a small streaming-response abstraction.
package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/klauspost/compress/zstd"
)

// RequestCompression selects the on-the-wire compression applied to a request
// body. It mirrors the Rust `RequestCompression` enum.
type RequestCompression int

const (
	// CompressionNone sends the body uncompressed (the default).
	CompressionNone RequestCompression = iota
	// CompressionZstd compresses JSON bodies with zstd at level 3 and sets the
	// Content-Encoding header to "zstd".
	CompressionZstd
)

// RequestBodyKind discriminates the variants of RequestBody.
type RequestBodyKind int

const (
	// RequestBodyNone indicates no body is present.
	RequestBodyNone RequestBodyKind = iota
	// RequestBodyJSON indicates a JSON value body.
	RequestBodyJSON
	// RequestBodyRaw indicates raw bytes body.
	RequestBodyRaw
)

// RequestBody is the body of a Request: either a JSON value or raw bytes.
// It mirrors the Rust `RequestBody` enum.
type RequestBody struct {
	Kind RequestBodyKind
	// JSON holds the decoded JSON value when Kind == RequestBodyJSON. It is the
	// raw serialized form used to avoid lossy round-trips through interface{}.
	JSON json.RawMessage
	// Raw holds the body bytes when Kind == RequestBodyRaw.
	Raw []byte
}

// JSONBody returns the JSON value when this body is a JSON body, or nil
// otherwise. It mirrors `RequestBody::json`.
func (b *RequestBody) JSONBody() json.RawMessage {
	if b == nil || b.Kind != RequestBodyJSON {
		return nil
	}
	return b.JSON
}

// NewJSONBody builds a JSON RequestBody from any serializable value.
func NewJSONBody(v any) (*RequestBody, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal json body: %w", err)
	}
	return &RequestBody{Kind: RequestBodyJSON, JSON: data}, nil
}

// NewRawBody builds a raw-bytes RequestBody. The provided slice is copied so the
// caller's input is never mutated.
func NewRawBody(raw []byte) *RequestBody {
	cp := make([]byte, len(raw))
	copy(cp, raw)
	return &RequestBody{Kind: RequestBodyRaw, Raw: cp}
}

// PreparedRequestBody is the result of converting a Request body into the exact
// bytes and headers that will be sent. It mirrors the Rust
// `PreparedRequestBody`.
type PreparedRequestBody struct {
	// Headers are the request headers after content-type / content-encoding
	// insertion. They are a fresh copy; the source request is never mutated.
	Headers http.Header
	// Body is the exact bytes to send, or nil when there is no body.
	Body []byte
}

// BodyBytes returns the body bytes, or an empty slice when there is no body.
func (p PreparedRequestBody) BodyBytes() []byte {
	if p.Body == nil {
		return []byte{}
	}
	return p.Body
}

// Request describes an outbound HTTP request before it is handed to a transport.
// It mirrors the Rust `Request` struct. Methods that "modify" a Request return a
// new copy and never mutate the receiver.
type Request struct {
	Method      string
	URL         string
	Headers     http.Header
	Body        *RequestBody
	Compression RequestCompression
	// Timeout is an optional per-request timeout. Zero means "no timeout".
	Timeout time.Duration
}

// NewRequest builds a Request with empty headers and no body.
func NewRequest(method, url string) Request {
	return Request{
		Method:      method,
		URL:         url,
		Headers:     http.Header{},
		Body:        nil,
		Compression: CompressionNone,
		Timeout:     0,
	}
}

// WithJSON returns a copy of the request with a JSON body. On marshal failure
// the body is left unset, matching the Rust `with_json` behavior which silently
// drops a failed serialization.
func (r Request) WithJSON(v any) Request {
	out := r.clone()
	if body, err := NewJSONBody(v); err == nil {
		out.Body = body
	} else {
		out.Body = nil
	}
	return out
}

// WithRawBody returns a copy of the request with a raw-bytes body.
func (r Request) WithRawBody(raw []byte) Request {
	out := r.clone()
	out.Body = NewRawBody(raw)
	return out
}

// WithCompression returns a copy of the request with the given compression.
func (r Request) WithCompression(c RequestCompression) Request {
	out := r.clone()
	out.Compression = c
	return out
}

// clone returns a deep-enough copy of the request so that mutating the copy's
// headers or body never affects the original.
func (r Request) clone() Request {
	out := r
	out.Headers = cloneHeader(r.Headers)
	return out
}

// PrepareBodyForSend converts the request body into the exact bytes that will be
// sent, returning a PreparedRequestBody. It mirrors the Rust
// `prepare_body_for_send`: it does not mutate the request, applies compression
// for JSON bodies, sets Content-Type when missing, and rejects compression for
// raw bodies or when Content-Encoding is already set.
func (r Request) PrepareBodyForSend() (PreparedRequestBody, error) {
	headers := cloneHeader(r.Headers)

	if r.Body == nil {
		return PreparedRequestBody{Headers: headers, Body: nil}, nil
	}

	switch r.Body.Kind {
	case RequestBodyRaw:
		if r.Compression != CompressionNone {
			return PreparedRequestBody{}, errors.New("request compression cannot be used with raw bodies")
		}
		raw := make([]byte, len(r.Body.Raw))
		copy(raw, r.Body.Raw)
		return PreparedRequestBody{Headers: headers, Body: raw}, nil

	case RequestBodyJSON:
		// Canonicalize the JSON bytes through json.Marshal so the encoding
		// matches serde_json::to_vec (no extraneous whitespace).
		jsonBytes, err := canonicalizeJSON(r.Body.JSON)
		if err != nil {
			return PreparedRequestBody{}, fmt.Errorf("serialize json body: %w", err)
		}

		out := jsonBytes
		if r.Compression != CompressionNone {
			if headers.Get("Content-Encoding") != "" {
				return PreparedRequestBody{}, errors.New("request compression was requested but content-encoding is already set")
			}
			switch r.Compression {
			case CompressionZstd:
				compressed, cerr := zstdEncodeAll(jsonBytes)
				if cerr != nil {
					return PreparedRequestBody{}, fmt.Errorf("zstd compress: %w", cerr)
				}
				out = compressed
				headers.Set("Content-Encoding", "zstd")
			default:
				return PreparedRequestBody{}, fmt.Errorf("unsupported compression: %d", r.Compression)
			}
		}

		if headers.Get("Content-Type") == "" {
			headers.Set("Content-Type", "application/json")
		}
		return PreparedRequestBody{Headers: headers, Body: out}, nil

	default:
		return PreparedRequestBody{Headers: headers, Body: nil}, nil
	}
}

// canonicalizeJSON re-marshals already-valid JSON so the bytes match what
// serde_json produces (compact, key order preserved as encoded by the caller).
func canonicalizeJSON(raw json.RawMessage) ([]byte, error) {
	// json.RawMessage from json.Marshal is already compact; compact again to be
	// safe in case a caller constructed the body by hand.
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// zstdEncodeAll compresses src with zstd at level 3, matching the Rust default
// (`zstd::stream::encode_all(.., 3)`). klauspost maps zstd level 3 to
// SpeedDefault via EncoderLevelFromZstd.
func zstdEncodeAll(src []byte) ([]byte, error) {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(3)))
	if err != nil {
		return nil, err
	}
	defer enc.Close()
	return enc.EncodeAll(src, nil), nil
}

// Response is a complete (non-streaming) HTTP response. It mirrors the Rust
// `Response` struct.
type Response struct {
	Status  int
	Headers http.Header
	Body    []byte
}

// cloneHeader returns a deep copy of an http.Header, never returning nil.
func cloneHeader(h http.Header) http.Header {
	out := http.Header{}
	for k, vs := range h {
		cp := make([]string, len(vs))
		copy(cp, vs)
		out[k] = cp
	}
	return out
}
