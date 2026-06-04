package tools

import (
	"testing"

	"github.com/sqlrush/codexgo/internal/features"
	"github.com/sqlrush/codexgo/internal/modelsmanager"
	"github.com/sqlrush/codexgo/internal/pty"
)

// shellFeatures mirrors the reference test fixture (tool_config_tests.rs
// `shell_features`): shell_tool on, shell_zsh_fork and unified_exec off.
func shellFeatures() *features.Features {
	f := features.NewFeaturesWithDefaults()
	f.Enable(features.FeatureShellTool)
	f.Disable(features.FeatureShellZshFork)
	f.Disable(features.FeatureUnifiedExec)
	return &f
}

func modelWithShellType(shellType modelsmanager.ConfigShellToolType) modelsmanager.ModelInfo {
	mi := modelsmanager.ModelInfoFromSlug("test-model")
	mi.ShellType = shellType
	return mi
}

// TestShellTypeForModelAndFeatures ports the reference
// `shell_type_is_derived_from_model_and_feature_gates` case plus the
// Default/Local/ShellCommand mappings.
func TestShellTypeForModelAndFeatures(t *testing.T) {
	t.Parallel()

	unifiedWhenPTY := modelsmanager.ConfigShellToolTypeShellCommand
	if pty.ConPTYSupported() {
		unifiedWhenPTY = modelsmanager.ConfigShellToolTypeUnifiedExec
	}

	tests := []struct {
		name      string
		modelType modelsmanager.ConfigShellToolType
		mutate    func(*features.Features)
		want      modelsmanager.ConfigShellToolType
	}{
		{
			name:      "unified-exec model falls back to shell_command when feature off",
			modelType: modelsmanager.ConfigShellToolTypeUnifiedExec,
			mutate:    func(*features.Features) {},
			want:      modelsmanager.ConfigShellToolTypeShellCommand,
		},
		{
			name:      "unified_exec feature selects the PTY pair when supported",
			modelType: modelsmanager.ConfigShellToolTypeUnifiedExec,
			mutate:    func(f *features.Features) { f.Enable(features.FeatureUnifiedExec) },
			want:      unifiedWhenPTY,
		},
		{
			name:      "unified_exec overrides a shell_command model too",
			modelType: modelsmanager.ConfigShellToolTypeShellCommand,
			mutate:    func(f *features.Features) { f.Enable(features.FeatureUnifiedExec) },
			want:      unifiedWhenPTY,
		},
		{
			name:      "zsh-fork wins over unified_exec",
			modelType: modelsmanager.ConfigShellToolTypeUnifiedExec,
			mutate: func(f *features.Features) {
				f.Enable(features.FeatureUnifiedExec)
				f.Enable(features.FeatureShellZshFork)
			},
			want: modelsmanager.ConfigShellToolTypeShellCommand,
		},
		{
			name:      "shell_tool off disables everything",
			modelType: modelsmanager.ConfigShellToolTypeUnifiedExec,
			mutate: func(f *features.Features) {
				f.Enable(features.FeatureUnifiedExec)
				f.Enable(features.FeatureShellZshFork)
				f.Disable(features.FeatureShellTool)
			},
			want: modelsmanager.ConfigShellToolTypeDisabled,
		},
		{
			name:      "default model type maps to shell_command",
			modelType: modelsmanager.ConfigShellToolTypeDefault,
			mutate:    func(*features.Features) {},
			want:      modelsmanager.ConfigShellToolTypeShellCommand,
		},
		{
			name:      "local model type maps to shell_command",
			modelType: modelsmanager.ConfigShellToolTypeLocal,
			mutate:    func(*features.Features) {},
			want:      modelsmanager.ConfigShellToolTypeShellCommand,
		},
		{
			name:      "disabled model type passes through",
			modelType: modelsmanager.ConfigShellToolTypeDisabled,
			mutate:    func(*features.Features) {},
			want:      modelsmanager.ConfigShellToolTypeDisabled,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := shellFeatures()
			tt.mutate(f)
			mi := modelWithShellType(tt.modelType)
			if got := ShellTypeForModelAndFeatures(&mi, f); got != tt.want {
				t.Errorf("ShellTypeForModelAndFeatures(%q) = %q, want %q", tt.modelType, got, tt.want)
			}
		})
	}
}

// TestShellCommandBackendForFeatures ports
// `shell_command_backend_requires_both_shell_tool_and_zsh_fork`.
func TestShellCommandBackendForFeatures(t *testing.T) {
	t.Parallel()

	f := shellFeatures()
	if got := ShellCommandBackendForFeatures(f); got != ShellCommandBackendClassic {
		t.Errorf("backend = %v, want classic", got)
	}

	f.Enable(features.FeatureShellZshFork)
	if got := ShellCommandBackendForFeatures(f); got != ShellCommandBackendZshFork {
		t.Errorf("backend = %v, want zsh-fork", got)
	}

	f.Disable(features.FeatureShellTool)
	if got := ShellCommandBackendForFeatures(f); got != ShellCommandBackendClassic {
		t.Errorf("backend = %v, want classic after shell_tool off", got)
	}
}
