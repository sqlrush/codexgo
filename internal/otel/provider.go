package otel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	apitrace "go.opentelemetry.io/otel/trace"
)

const (
	envAttribute      = "env"
	hostNameAttribute = "host.name"
	serviceNameAttr   = "service.name"
	serviceVersionAtt = "service.version"
)

// resourceKind selects which resource attributes to attach.
type resourceKind int

const (
	resourceKindLogs resourceKind = iota
	resourceKindTraces
)

// OtelProvider holds the installed telemetry providers. Mirrors Rust
// `OtelProvider`. Logs and gRPC traces and metric OTLP export are not wired in
// this port because the corresponding exporter crates are outside the allowed
// dependency set; the trace OTLP HTTP path and the metrics client are.
type OtelProvider struct {
	TracerProvider *sdktrace.TracerProvider
	Tracer         apitrace.Tracer
	Metrics        *MetricsClient
}

// Shutdown flushes and stops the installed providers. Mirrors Rust
// `OtelProvider::shutdown`.
func (p *OtelProvider) Shutdown(ctx context.Context) {
	if p.TracerProvider != nil {
		_ = p.TracerProvider.ForceFlush(ctx)
		_ = p.TracerProvider.Shutdown(ctx)
	}
}

// FromSettings builds an [OtelProvider] from settings. Returns (nil, nil) when
// no exporter is enabled. Mirrors Rust `OtelProvider::from`. OTEL_* environment
// variables are honored by the underlying OTLP HTTP exporter.
func FromSettings(ctx context.Context, settings *OtelSettings) (*OtelProvider, error) {
	logEnabled := settings.Exporter.Kind != OtelExporterNone
	traceEnabled := settings.TraceExporter.Kind != OtelExporterNone
	metricExporter := ResolveExporter(settings.MetricsExporter)
	metricsEnabled := metricExporter.Kind != OtelExporterNone

	if !logEnabled && !traceEnabled && !metricsEnabled {
		// Tracestate propagation is process-global; clear it when no provider
		// is installed.
		if err := SetTracestateEntries(map[string]map[string]string{}); err != nil {
			return nil, err
		}
		return nil, nil
	}

	if traceEnabled {
		if err := ValidateSpanAttributes(settings.SpanAttributes); err != nil {
			return nil, err
		}
	}
	if err := ValidateTracestateEntries(settings.Tracestate); err != nil {
		return nil, err
	}

	var metrics *MetricsClient
	if metricsEnabled {
		config := OtlpMetricsConfig(
			settings.Environment,
			settings.ServiceName,
			settings.ServiceVersion,
			metricExporter,
		)
		if settings.RuntimeMetrics {
			config = config.WithRuntimeReader()
		}
		client, err := NewMetricsClient(config)
		if err != nil {
			return nil, err
		}
		metrics = client
	}

	var tracerProvider *sdktrace.TracerProvider
	var tracer apitrace.Tracer
	if traceEnabled {
		tp, err := buildTracerProvider(ctx, settings)
		if err != nil {
			return nil, err
		}
		tracerProvider = tp
		tracer = tp.Tracer(settings.ServiceName)
	}

	if err := SetTracestateEntries(settings.Tracestate); err != nil {
		return nil, err
	}
	if tracerProvider != nil {
		otel.SetTracerProvider(tracerProvider)
		otel.SetTextMapPropagator(propagation.TraceContext{})
	}
	if metrics != nil {
		InstallGlobal(metrics)
		if settings.MetricsExporter.Kind == OtelExporterStatsig {
			InstallGlobalStatsigSettings(StatsigMetricsSettings{Environment: settings.Environment})
		}
	}

	return &OtelProvider{
		TracerProvider: tracerProvider,
		Tracer:         tracer,
		Metrics:        metrics,
	}, nil
}

// Metrics returns the metrics client, or nil. Mirrors Rust
// `OtelProvider::metrics`.
func (p *OtelProvider) MetricsClient() *MetricsClient {
	return p.Metrics
}

func buildTracerProvider(ctx context.Context, settings *OtelSettings) (*sdktrace.TracerProvider, error) {
	res, err := makeResource(ctx, settings, resourceKindTraces)
	if err != nil {
		return nil, err
	}

	exporter := ResolveExporter(settings.TraceExporter)
	switch exporter.Kind {
	case OtelExporterNone:
		return sdktrace.NewTracerProvider(sdktrace.WithResource(res)), nil
	case OtelExporterStatsig:
		// resolve_exporter never yields Statsig for traces.
		return nil, errors.New("otel: statsig exporter is metrics-only")
	case OtelExporterOtlpHTTP:
		spanExporter, err := buildHTTPSpanExporter(ctx, exporter)
		if err != nil {
			return nil, err
		}
		return sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithBatcher(spanExporter),
		), nil
	case OtelExporterOtlpGRPC:
		return nil, errors.New("otel: OTLP gRPC trace exporter is not available in this build")
	default:
		return nil, fmt.Errorf("otel: unknown exporter kind %d", exporter.Kind)
	}
}

func buildHTTPSpanExporter(ctx context.Context, exporter OtelExporter) (sdktrace.SpanExporter, error) {
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(exporter.Endpoint)}
	if len(exporter.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(exporter.Headers))
	}
	if strings.HasPrefix(exporter.Endpoint, "http://") {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	exp, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("otel: build OTLP HTTP trace exporter: %w", err)
	}
	return exp, nil
}

func makeResource(ctx context.Context, settings *OtelSettings, kind resourceKind) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		attribute.String(serviceNameAttr, settings.ServiceName),
		attribute.String(serviceVersionAtt, settings.ServiceVersion),
		attribute.String(envAttribute, settings.Environment),
	}
	if kind == resourceKindLogs {
		if host := detectedHostName(); host != "" {
			attrs = append(attrs, attribute.String(hostNameAttribute, host))
		}
	}
	res, err := resource.New(ctx, resource.WithAttributes(attrs...))
	if err != nil {
		return nil, fmt.Errorf("otel: build resource: %w", err)
	}
	return res, nil
}

func detectedHostName() string {
	host, err := os.Hostname()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(host)
}
