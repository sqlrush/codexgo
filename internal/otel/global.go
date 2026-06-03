package otel

import "sync"

// Process-global metrics state. Mirrors the GLOBAL_METRICS and
// GLOBAL_STATSIG_METRICS_SETTINGS OnceLocks in codex-rs/otel/src/metrics/mod.rs.
var (
	globalMu              sync.Mutex
	globalMetrics         *MetricsClient
	globalMetricsSet      bool
	globalStatsigSettings *StatsigMetricsSettings
)

// InstallGlobal installs the global metrics client once. Subsequent calls are
// ignored. Mirrors Rust `install_global`.
func InstallGlobal(metrics *MetricsClient) {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalMetricsSet {
		return
	}
	globalMetrics = metrics
	globalMetricsSet = true
}

// Global returns the installed global metrics client, or nil. Mirrors Rust
// `global`.
func Global() *MetricsClient {
	globalMu.Lock()
	defer globalMu.Unlock()
	return globalMetrics
}

// InstallGlobalStatsigSettings installs the global Statsig settings once.
// Mirrors Rust `install_global_statsig_settings`.
func InstallGlobalStatsigSettings(settings StatsigMetricsSettings) {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalStatsigSettings != nil {
		return
	}
	s := settings
	globalStatsigSettings = &s
}

// GlobalStatsigMetricsSettings returns the installed Statsig settings, or nil.
// Mirrors Rust `global_statsig_settings` / `global_statsig_metrics_settings`.
func GlobalStatsigMetricsSettings() *StatsigMetricsSettings {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalStatsigSettings == nil {
		return nil
	}
	s := *globalStatsigSettings
	return &s
}

// StartGlobalTimer starts a timer using the global metrics client. Mirrors Rust
// `start_global_timer`. Returns ErrExporterDisabled when no client is installed.
func StartGlobalTimer(name string, tags []Tag) (*Timer, error) {
	metrics := Global()
	if metrics == nil {
		return nil, ErrExporterDisabled
	}
	return metrics.StartTimer(name, tags)
}
