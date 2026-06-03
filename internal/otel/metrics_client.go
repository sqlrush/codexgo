package otel

import (
	"context"
	"sort"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	meterName           = "codex"
	durationUnit        = "ms"
	durationDescription = "Duration in milliseconds."
)

// MetricsConfig configures a [MetricsClient]. Mirrors Rust `MetricsConfig`.
type MetricsConfig struct {
	Environment    string
	ServiceName    string
	ServiceVersion string
	Exporter       OtelExporter
	ExportInterval *time.Duration
	RuntimeReader  bool
	defaultTags    map[string]string
}

// OtlpMetricsConfig builds an OTLP-exporter metrics config. Mirrors Rust
// `MetricsConfig::otlp`.
func OtlpMetricsConfig(environment, serviceName, serviceVersion string, exporter OtelExporter) MetricsConfig {
	return MetricsConfig{
		Environment:    environment,
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Exporter:       exporter,
		defaultTags:    map[string]string{},
	}
}

// WithExportInterval overrides the periodic export interval. Mirrors Rust
// `with_export_interval`.
func (c MetricsConfig) WithExportInterval(interval time.Duration) MetricsConfig {
	c.ExportInterval = &interval
	return c
}

// WithRuntimeReader enables on-demand runtime snapshots. Mirrors Rust
// `with_runtime_reader`.
func (c MetricsConfig) WithRuntimeReader() MetricsConfig {
	c.RuntimeReader = true
	return c
}

// WithTag adds a validated default tag. Mirrors Rust `with_tag`.
func (c MetricsConfig) WithTag(key, value string) (MetricsConfig, error) {
	if err := ValidateTagKey(key); err != nil {
		return c, err
	}
	if err := ValidateTagValue(value); err != nil {
		return c, err
	}
	tags := make(map[string]string, len(c.defaultTags)+1)
	for k, v := range c.defaultTags {
		tags[k] = v
	}
	tags[key] = value
	c.defaultTags = tags
	return c, nil
}

// MetricsClient records counters and histograms via the OpenTelemetry metric
// API. Mirrors Rust `MetricsClient`. Instrument export is governed by whichever
// global meter provider is installed (None/no-op unless an SDK provider is set).
type MetricsClient struct {
	meter       metric.Meter
	defaultTags map[string]string

	mu                 sync.Mutex
	counters           map[string]metric.Int64Counter
	histograms         map[string]metric.Float64Histogram
	durationHistograms map[string]metric.Float64Histogram
}

// NewMetricsClient builds a metrics client from configuration and validates the
// default tags. Mirrors Rust `MetricsClient::new`.
func NewMetricsClient(config MetricsConfig) (*MetricsClient, error) {
	if err := validateTags(config.defaultTags); err != nil {
		return nil, err
	}
	return &MetricsClient{
		meter:              otel.Meter(meterName),
		defaultTags:        config.defaultTags,
		counters:           map[string]metric.Int64Counter{},
		histograms:         map[string]metric.Float64Histogram{},
		durationHistograms: map[string]metric.Float64Histogram{},
	}, nil
}

// Counter increments a counter. Mirrors Rust `MetricsClient::counter`.
func (m *MetricsClient) Counter(name string, inc int64, tags []Tag) error {
	if err := ValidateMetricName(name); err != nil {
		return err
	}
	if inc < 0 {
		return &NegativeCounterIncrementError{Name: name, Inc: inc}
	}
	attrs, err := m.attributes(tags)
	if err != nil {
		return err
	}
	m.mu.Lock()
	counter, ok := m.counters[name]
	if !ok {
		counter, err = m.meter.Int64Counter(name)
		if err != nil {
			m.mu.Unlock()
			return err
		}
		m.counters[name] = counter
	}
	m.mu.Unlock()
	counter.Add(context.Background(), inc, metric.WithAttributes(attrs...))
	return nil
}

// Histogram records a histogram sample. Mirrors Rust `MetricsClient::histogram`.
func (m *MetricsClient) Histogram(name string, value int64, tags []Tag) error {
	if err := ValidateMetricName(name); err != nil {
		return err
	}
	attrs, err := m.attributes(tags)
	if err != nil {
		return err
	}
	m.mu.Lock()
	histogram, ok := m.histograms[name]
	if !ok {
		histogram, err = m.meter.Float64Histogram(name)
		if err != nil {
			m.mu.Unlock()
			return err
		}
		m.histograms[name] = histogram
	}
	m.mu.Unlock()
	histogram.Record(context.Background(), float64(value), metric.WithAttributes(attrs...))
	return nil
}

// RecordDuration records a duration in milliseconds. Mirrors Rust
// `MetricsClient::record_duration`.
func (m *MetricsClient) RecordDuration(name string, duration time.Duration, tags []Tag) error {
	ms := duration.Milliseconds()
	return m.durationHistogram(name, ms, tags)
}

func (m *MetricsClient) durationHistogram(name string, value int64, tags []Tag) error {
	if err := ValidateMetricName(name); err != nil {
		return err
	}
	attrs, err := m.attributes(tags)
	if err != nil {
		return err
	}
	m.mu.Lock()
	histogram, ok := m.durationHistograms[name]
	if !ok {
		histogram, err = m.meter.Float64Histogram(
			name,
			metric.WithUnit(durationUnit),
			metric.WithDescription(durationDescription),
		)
		if err != nil {
			m.mu.Unlock()
			return err
		}
		m.durationHistograms[name] = histogram
	}
	m.mu.Unlock()
	histogram.Record(context.Background(), float64(value), metric.WithAttributes(attrs...))
	return nil
}

// StartTimer starts a duration timer. Mirrors Rust `MetricsClient::start_timer`.
func (m *MetricsClient) StartTimer(name string, tags []Tag) (*Timer, error) {
	return newTimer(name, tags, m), nil
}

// attributes merges the default tags with the call-site tags, validating
// call-site tags. Mirrors Rust `MetricsClientInner::attributes`.
func (m *MetricsClient) attributes(tags []Tag) ([]attribute.KeyValue, error) {
	if len(tags) == 0 {
		return mapToAttributes(m.defaultTags), nil
	}
	merged := make(map[string]string, len(m.defaultTags)+len(tags))
	for k, v := range m.defaultTags {
		merged[k] = v
	}
	for _, tag := range tags {
		if err := ValidateTagKey(tag.Key); err != nil {
			return nil, err
		}
		if err := ValidateTagValue(tag.Value); err != nil {
			return nil, err
		}
		merged[tag.Key] = tag.Value
	}
	return mapToAttributes(merged), nil
}

func mapToAttributes(m map[string]string) []attribute.KeyValue {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	attrs := make([]attribute.KeyValue, 0, len(keys))
	for _, k := range keys {
		attrs = append(attrs, attribute.String(k, m[k]))
	}
	return attrs
}
