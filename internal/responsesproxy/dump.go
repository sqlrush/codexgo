package responsesproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// authorizationHeaderName and redactedHeaderValue mirror codex's dump constants.
const (
	authorizationHeaderName = "authorization"
	redactedHeaderValue     = "[REDACTED]"
)

// headerDump is a single name/value pair in a dumped exchange. It mirrors codex's
// `HeaderDump`. Sensitive values (authorization, anything containing "cookie")
// are redacted before construction.
type headerDump struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// newHeaderDump builds a [headerDump], redacting the value for sensitive headers.
func newHeaderDump(name, value string) headerDump {
	v := value
	if shouldRedactHeader(name) {
		v = redactedHeaderValue
	}
	return headerDump{Name: name, Value: v}
}

// requestDump is the JSON shape written for a forwarded request. It mirrors
// codex's `RequestDump`.
type requestDump struct {
	Method  string          `json:"method"`
	URL     string          `json:"url"`
	Headers []headerDump    `json:"headers"`
	Body    json.RawMessage `json:"body"`
}

// responseDump is the JSON shape written for an upstream response. It mirrors
// codex's `ResponseDump`.
type responseDump struct {
	Status  uint16          `json:"status"`
	Headers []headerDump    `json:"headers"`
	Body    json.RawMessage `json:"body"`
}

// exchangeDumper writes redacted request/response dumps to a directory, one pair
// of JSON files per forwarded exchange. It mirrors codex's `ExchangeDumper` and
// is safe for concurrent use.
type exchangeDumper struct {
	dumpDir      string
	nextSequence atomic.Uint64
}

// newExchangeDumper creates the dump directory and returns a dumper. It mirrors
// codex's `ExchangeDumper::new`; the sequence counter starts at 1.
func newExchangeDumper(dumpDir string) (*exchangeDumper, error) {
	if err := os.MkdirAll(dumpDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating dump dir %s: %w", dumpDir, err)
	}
	d := &exchangeDumper{dumpDir: dumpDir}
	d.nextSequence.Store(1)
	return d, nil
}

// dumpRequest writes the request dump file and returns an [exchangeDump] holding
// the path of the matching response file. It mirrors codex's
// `ExchangeDumper::dump_request`.
func (d *exchangeDumper) dumpRequest(method, url string, headers []headerDump, body []byte) (*exchangeDump, error) {
	sequence := d.nextSequence.Add(1) - 1 // fetch_add returns the prior value
	timestampMs := time.Now().UnixMilli()
	if timestampMs < 0 {
		timestampMs = 0
	}
	prefix := fmt.Sprintf("%06d-%d", sequence, timestampMs)

	requestPath := filepath.Join(d.dumpDir, prefix+"-request.json")
	responsePath := filepath.Join(d.dumpDir, prefix+"-response.json")

	dump := requestDump{
		Method:  method,
		URL:     url,
		Headers: headers,
		Body:    dumpBody(body),
	}
	if err := writeJSONDump(requestPath, dump); err != nil {
		return nil, err
	}
	return &exchangeDump{responsePath: responsePath}, nil
}

// exchangeDump holds the response-file path for a pending exchange. It mirrors
// codex's `ExchangeDump`.
type exchangeDump struct {
	responsePath string
}

// teeResponseBody returns a [responseBodyDump] that streams the upstream body to
// the caller while accumulating it for the response dump. It mirrors codex's
// `ExchangeDump::tee_response_body`.
func (e *exchangeDump) teeResponseBody(status uint16, headers []headerDump, body io.Reader) *responseBodyDump {
	return &responseBodyDump{
		responseBody: body,
		responsePath: e.responsePath,
		status:       status,
		headers:      headers,
	}
}

// responseBodyDump is an [io.Reader] that tees the upstream response body into an
// in-memory buffer and writes the response dump file when the stream ends or the
// reader is closed. It mirrors codex's `ResponseBodyDump`.
type responseBodyDump struct {
	responseBody io.Reader
	responsePath string
	status       uint16
	headers      []headerDump
	body         bytes.Buffer
	mu           sync.Mutex
	dumpWritten  bool
}

// Read implements [io.Reader], accumulating bytes and writing the dump on EOF.
func (r *responseBodyDump) Read(p []byte) (int, error) {
	n, err := r.responseBody.Read(p)
	if n > 0 {
		r.body.Write(p[:n])
	}
	if err == io.EOF {
		r.writeDumpIfNeeded()
	}
	return n, err
}

// Close writes the dump (if not already written), matching codex's Drop impl,
// so the dump is produced even when the body is not fully read.
func (r *responseBodyDump) Close() error {
	r.writeDumpIfNeeded()
	if closer, ok := r.responseBody.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// writeDumpIfNeeded writes the response dump exactly once.
func (r *responseBodyDump) writeDumpIfNeeded() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dumpWritten {
		return
	}
	r.dumpWritten = true

	dump := responseDump{
		Status:  r.status,
		Headers: r.headers,
		Body:    dumpBody(r.body.Bytes()),
	}
	if err := writeJSONDump(r.responsePath, dump); err != nil {
		fmt.Fprintf(os.Stderr, "responses-api-proxy failed to write %s: %v\n", r.responsePath, err)
	}
}

// shouldRedactHeader reports whether a header value should be redacted: the
// authorization header or any header whose name contains "cookie"
// (case-insensitive). It mirrors codex's `should_redact_header`.
func shouldRedactHeader(name string) bool {
	lower := strings.ToLower(name)
	return lower == authorizationHeaderName || strings.Contains(lower, "cookie")
}

// dumpBody renders a body as JSON when it parses as valid JSON, otherwise as a
// JSON string (lossy UTF-8). It mirrors codex's `dump_body`.
//
// When the body is valid JSON, the raw bytes are stored verbatim (the surrounding
// pretty-printer re-indents them). This preserves object key insertion order,
// matching codex's serde_json build, which enables the preserve_order feature.
func dumpBody(body []byte) json.RawMessage {
	if len(bytes.TrimSpace(body)) > 0 && json.Valid(body) {
		raw := make([]byte, len(body))
		copy(raw, body)
		return raw
	}
	raw, err := marshalNoHTMLEscape(string(toValidUTF8(body)))
	if err != nil {
		return json.RawMessage(`""`)
	}
	return raw
}

// writeJSONDump writes dump as pretty JSON (2-space indent) with a trailing
// newline, matching codex's `write_json_dump` (serde_json::to_vec_pretty + '\n').
func writeJSONDump(path string, dump any) error {
	bytesOut, err := marshalIndentNoHTMLEscape(dump)
	if err != nil {
		return fmt.Errorf("serializing dump: %w", err)
	}
	bytesOut = append(bytesOut, '\n')
	if err := os.WriteFile(path, bytesOut, 0o644); err != nil {
		return fmt.Errorf("writing dump %s: %w", path, err)
	}
	return nil
}
