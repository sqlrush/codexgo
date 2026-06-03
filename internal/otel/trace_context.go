// Package otel is a faithful port of codex-rs/otel. It provides W3C trace
// context propagation, metric naming/validation/tagging, and OTLP/Statsig
// exporter configuration for Codex telemetry.
//
// Telemetry is OFF unless explicitly opted in via OTEL settings (the default
// exporter is None and the built-in Statsig exporter is disabled in debug
// builds).
package otel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/sqlrush/codexgo/internal/protocol"
)

const (
	traceparentEnvVar = "TRACEPARENT"
	tracestateEnvVar  = "TRACESTATE"
)

var (
	traceparentOnce sync.Once
	traceparentCtx  context.Context
	traceparentSet  bool

	tracestateMu      sync.RWMutex
	tracestateEntries = map[string]map[string]string{}
)

// ContextFromW3CTraceContext extracts a context from a W3C trace context.
// Returns nil and false when the traceparent is missing or invalid. Mirrors
// Rust `context_from_w3c_trace_context`.
func ContextFromW3CTraceContext(trace *protocol.W3cTraceContext) (context.Context, bool) {
	if trace == nil {
		return nil, false
	}
	return contextFromTraceHeaders(trace.Traceparent, trace.Tracestate)
}

// SpanW3CTraceContext injects the given context's span into a W3C trace
// context, merging configured tracestate entries. Returns nil when the span
// context is invalid. Mirrors Rust `span_w3c_trace_context`.
func SpanW3CTraceContext(ctx context.Context) *protocol.W3cTraceContext {
	if ctx == nil {
		return nil
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil
	}

	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)

	var tp *string
	if v, ok := carrier["traceparent"]; ok {
		tp = &v
	}
	rawTracestate, hasTracestate := carrier["tracestate"]

	tracestateMu.RLock()
	configured := cloneTracestate(tracestateEntries)
	tracestateMu.RUnlock()

	var ts *string
	if merged := mergeTracestateEntries(optString(rawTracestate, hasTracestate), configured); merged != "" {
		ts = &merged
	}

	return &protocol.W3cTraceContext{Traceparent: tp, Tracestate: ts}
}

// CurrentSpanTraceID returns the active span's trace id in hex, or nil when no
// valid span is active. Mirrors Rust `current_span_trace_id`.
func CurrentSpanTraceID(ctx context.Context) *string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil
	}
	id := sc.TraceID().String()
	return &id
}

// TraceparentContextFromEnv reads TRACEPARENT/TRACESTATE from the environment
// once and returns the resulting context (or nil). Mirrors Rust
// `traceparent_context_from_env`.
func TraceparentContextFromEnv() (context.Context, bool) {
	traceparentOnce.Do(func() {
		traceparentCtx, traceparentSet = loadTraceparentContext()
	})
	return traceparentCtx, traceparentSet
}

func loadTraceparentContext() (context.Context, bool) {
	traceparent, ok := os.LookupEnv(traceparentEnvVar)
	if !ok {
		return nil, false
	}
	var tracestate *string
	if v, ok := os.LookupEnv(tracestateEnvVar); ok {
		tracestate = &v
	}
	return contextFromTraceHeaders(&traceparent, tracestate)
}

// contextFromTraceHeaders extracts a context from raw traceparent/tracestate
// headers. Mirrors Rust `context_from_trace_headers`.
func contextFromTraceHeaders(traceparent, tracestate *string) (context.Context, bool) {
	if traceparent == nil {
		return nil, false
	}
	carrier := propagation.MapCarrier{"traceparent": *traceparent}
	if tracestate != nil {
		carrier["tracestate"] = *tracestate
	}
	ctx := propagation.TraceContext{}.Extract(context.Background(), carrier)
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil, false
	}
	return ctx, true
}

// SetTracestateEntries validates and installs the process-global configured
// tracestate entries. Mirrors Rust `set_tracestate_entries`.
func SetTracestateEntries(entries map[string]map[string]string) error {
	if err := ValidateTracestateEntries(entries); err != nil {
		return err
	}
	tracestateMu.Lock()
	tracestateEntries = cloneTracestate(entries)
	tracestateMu.Unlock()
	return nil
}

// ValidateTracestateEntries validates configured tracestate members before they
// are propagated. Mirrors Rust `validate_tracestate_entries`.
func ValidateTracestateEntries(entries map[string]map[string]string) error {
	keys := sortedKeys(entries)
	ts := trace.TraceState{}
	// Insert in reverse so the resulting order matches deterministic map order.
	for i := len(keys) - 1; i >= 0; i-- {
		key := keys[i]
		encKey, encVal, err := encodeTracestateMemberFields(key, entries[key])
		if err != nil {
			return err
		}
		ts, err = ts.Insert(encKey, encVal)
		if err != nil {
			return fmt.Errorf("invalid configured tracestate: %w", err)
		}
	}
	return nil
}

// ValidateTracestateMember validates one configured tracestate member. Mirrors
// Rust `validate_tracestate_member`.
func ValidateTracestateMember(memberKey string, fields map[string]string) error {
	encKey, encVal, err := encodeTracestateMemberFields(memberKey, fields)
	if err != nil {
		return err
	}
	if _, err := (trace.TraceState{}).Insert(encKey, encVal); err != nil {
		return fmt.Errorf("invalid configured tracestate: %w", err)
	}
	return nil
}

func encodeTracestateMemberFields(memberKey string, fields map[string]string) (string, string, error) {
	encoded := make([]string, 0, len(fields))
	for _, fieldKey := range sortedStringKeys(fields) {
		value := fields[fieldKey]
		if !isConfiguredTracestateFieldKey(fieldKey) {
			return "", "", fmt.Errorf("invalid configured tracestate field key %s.%s", memberKey, fieldKey)
		}
		if !isConfiguredTracestateFieldValue(value) {
			return "", "", fmt.Errorf("invalid configured tracestate value for %s.%s", memberKey, fieldKey)
		}
		encoded = append(encoded, fieldKey+":"+value)
	}
	value := strings.Join(encoded, ";")
	if !isHeaderSafeTracestateMemberValue(value) {
		return "", "", fmt.Errorf("invalid configured tracestate value for %s", memberKey)
	}
	return memberKey, value, nil
}

func isConfiguredTracestateFieldKey(fieldKey string) bool {
	if fieldKey == "" {
		return false
	}
	for i := 0; i < len(fieldKey); i++ {
		b := fieldKey[i]
		if b < '!' || b > '~' {
			return false
		}
		if b == ':' || b == ';' || b == ',' || b == '=' {
			return false
		}
	}
	return true
}

func isConfiguredTracestateFieldValue(value string) bool {
	for i := 0; i < len(value); i++ {
		b := value[i]
		if !isTracestateMemberValueByte(b) || b == ';' {
			return false
		}
	}
	return true
}

func isHeaderSafeTracestateMemberValue(value string) bool {
	if value == "" {
		return true
	}
	for i := 0; i < len(value); i++ {
		if !isTracestateMemberValueByte(value[i]) {
			return false
		}
	}
	return value[len(value)-1] != ' '
}

func isTracestateMemberValueByte(b byte) bool {
	return b >= ' ' && b <= '~' && b != ',' && b != '='
}

// mergeTracestateEntries upserts configured member fields into an existing
// tracestate header. Mirrors Rust `merge_tracestate_entries`.
func mergeTracestateEntries(tracestate *string, configured map[string]map[string]string) string {
	ts := trace.TraceState{}
	if tracestate != nil {
		parsed, err := trace.ParseTraceState(*tracestate)
		if err == nil {
			ts = parsed
		}
	}

	// Insert configured entries in reverse map order so the result keeps
	// deterministic order (Insert places members at the front).
	keys := sortedKeys(configured)
	for i := len(keys) - 1; i >= 0; i-- {
		key := keys[i]
		value := mergeTracestateMemberFields(getTraceStateValue(ts, key), configured[key])
		next, err := ts.Insert(key, value)
		if err != nil {
			break
		}
		ts = next
	}

	return ts.String()
}

func getTraceStateValue(ts trace.TraceState, key string) *string {
	v := ts.Get(key)
	if v == "" {
		return nil
	}
	return &v
}

// mergeTracestateMemberFields upserts configured fields into one member's opaque
// value. Mirrors Rust `merge_tracestate_member_fields`.
func mergeTracestateMemberFields(existing *string, configuredFields map[string]string) string {
	var fields []string
	seen := map[string]struct{}{}

	if existing != nil {
		for _, field := range strings.Split(*existing, ";") {
			if field == "" {
				continue
			}
			if fieldKey, _, ok := strings.Cut(field, ":"); ok {
				if value, has := configuredFields[fieldKey]; has {
					if _, dup := seen[fieldKey]; !dup {
						seen[fieldKey] = struct{}{}
						fields = append(fields, fieldKey+":"+value)
					}
					continue
				}
				seen[fieldKey] = struct{}{}
			}
			fields = append(fields, field)
		}
	}

	for _, fieldKey := range sortedStringKeys(configuredFields) {
		if _, dup := seen[fieldKey]; dup {
			continue
		}
		fields = append(fields, fieldKey+":"+configuredFields[fieldKey])
	}
	return strings.Join(fields, ";")
}

func cloneTracestate(in map[string]map[string]string) map[string]map[string]string {
	out := make(map[string]map[string]string, len(in))
	for k, v := range in {
		inner := make(map[string]string, len(v))
		for ik, iv := range v {
			inner[ik] = iv
		}
		out[k] = inner
	}
	return out
}

func sortedKeys(m map[string]map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func optString(v string, present bool) *string {
	if !present {
		return nil
	}
	return &v
}

// ErrInvalidTracestate is returned for malformed configured tracestate input.
var ErrInvalidTracestate = errors.New("invalid configured tracestate")
