package otel

import (
	"errors"
	"sort"
)

const (
	// statsigOtlpHTTPEndpoint is the built-in Statsig metrics ingestion
	// endpoint. Mirrors Rust STATSIG_OTLP_HTTP_ENDPOINT.
	statsigOtlpHTTPEndpoint = "https://ab.chatgpt.com/otlp/v1/metrics"
	// statsigAPIKeyHeader is the header carrying the Statsig client key.
	statsigAPIKeyHeader = "statsig-api-key"
	// statsigAPIKey is the built-in Statsig client key.
	statsigAPIKey = "client-MkRuleRQBd6qakfnDYqJVR9JuXcY57Ljly3vi5JVUIO"
)

// debugBuild controls whether the built-in Statsig exporter resolves to None.
// In codex this is cfg!(debug_assertions); we default to false (release-like)
// so the drop-in default behavior matches codex release builds. Tests can set
// this to true to exercise the debug path.
var debugBuild = false

// OtelHTTPProtocol selects the OTLP HTTP payload encoding. Mirrors Rust
// `OtelHttpProtocol`.
type OtelHTTPProtocol int

const (
	// OtelHTTPProtocolBinary is HTTP with binary protobuf.
	OtelHTTPProtocolBinary OtelHTTPProtocol = iota
	// OtelHTTPProtocolJSON is HTTP with JSON payload.
	OtelHTTPProtocolJSON
)

// OtelTLSConfig holds optional TLS material for OTLP exporters. Mirrors Rust
// `OtelTlsConfig`. Paths must be absolute.
type OtelTLSConfig struct {
	CACertificate     *string
	ClientCertificate *string
	ClientPrivateKey  *string
}

// OtelExporterKind enumerates the exporter variants. Mirrors the discriminant
// of Rust `OtelExporter`.
type OtelExporterKind int

const (
	// OtelExporterNone disables export.
	OtelExporterNone OtelExporterKind = iota
	// OtelExporterStatsig is the built-in Statsig metrics exporter.
	OtelExporterStatsig
	// OtelExporterOtlpGRPC exports over OTLP gRPC.
	OtelExporterOtlpGRPC
	// OtelExporterOtlpHTTP exports over OTLP HTTP.
	OtelExporterOtlpHTTP
)

// OtelExporter is the exporter configuration. Mirrors Rust `OtelExporter`. The
// active variant is selected by Kind; only the fields for that variant are
// meaningful.
type OtelExporter struct {
	Kind     OtelExporterKind
	Endpoint string
	Headers  map[string]string
	Protocol OtelHTTPProtocol
	TLS      *OtelTLSConfig
}

// NoneExporter returns the disabled exporter.
func NoneExporter() OtelExporter { return OtelExporter{Kind: OtelExporterNone} }

// StatsigExporter returns the built-in Statsig exporter.
func StatsigExporter() OtelExporter { return OtelExporter{Kind: OtelExporterStatsig} }

// OtelSettings configures the OTEL provider. Mirrors Rust `OtelSettings`.
type OtelSettings struct {
	Environment     string
	ServiceName     string
	ServiceVersion  string
	CodexHome       string
	Exporter        OtelExporter
	TraceExporter   OtelExporter
	MetricsExporter OtelExporter
	RuntimeMetrics  bool
	SpanAttributes  map[string]string
	Tracestate      map[string]map[string]string
}

// StatsigMetricsSettings are the resolved Statsig metrics settings that another
// process can use to recreate the built-in metrics exporter. Mirrors Rust
// `StatsigMetricsSettings`.
type StatsigMetricsSettings struct {
	Environment string `json:"environment"`
}

// ResolveExporter resolves the built-in Statsig exporter into a concrete OTLP
// HTTP exporter (or None in debug builds). Mirrors Rust `resolve_exporter`.
func ResolveExporter(exporter OtelExporter) OtelExporter {
	if exporter.Kind != OtelExporterStatsig {
		return exporter
	}
	if debugBuild {
		return NoneExporter()
	}
	return OtelExporter{
		Kind:     OtelExporterOtlpHTTP,
		Endpoint: statsigOtlpHTTPEndpoint,
		Headers: map[string]string{
			statsigAPIKeyHeader: statsigAPIKey,
		},
		Protocol: OtelHTTPProtocolJSON,
		TLS:      nil,
	}
}

// ValidateSpanAttributes rejects empty attribute keys. Mirrors Rust
// `validate_span_attributes`.
func ValidateSpanAttributes(attributes map[string]string) error {
	keys := make([]string, 0, len(attributes))
	for k := range attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "" {
			return errors.New("configured span attribute key must not be empty")
		}
	}
	return nil
}
