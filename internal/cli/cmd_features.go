package cli

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/sqlrush/codexgo/pkg/config"
	"github.com/sqlrush/codexgo/pkg/features"
)

// runFeaturesSubcommand handles `codex features <list|enable|disable>`, mirroring
// the FeaturesCli surface in main.rs.
func runFeaturesSubcommand(_ context.Context, parsed ParsedCommandLine, streams Streams) int {
	args := parsed.SubcommandArgs
	if len(args) == 0 {
		fmt.Fprintln(streams.Stderr, "error: a subcommand is required: list, enable, or disable")
		return 1
	}
	if args[0] == "-h" || args[0] == "--help" {
		printFeaturesHelp(streams.Stdout)
		return 0
	}

	switch args[0] {
	case "list":
		return runFeaturesList(parsed, streams)
	case "enable":
		return runFeaturesSet(parsed, streams, args[1:], true)
	case "disable":
		return runFeaturesSet(parsed, streams, args[1:], false)
	default:
		fmt.Fprintf(streams.Stderr, "error: unknown features subcommand %q\n", args[0])
		return 1
	}
}

// runFeaturesList prints the known features with stage and effective enabled
// state, honoring -c overrides. It mirrors FeaturesSubcommand::List.
func runFeaturesList(parsed ParsedCommandLine, streams Streams) int {
	overrides, err := parsed.Root.Overrides.Parse()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 1
	}
	result, err := config.Load(config.LoadOptions{
		Profile:      parsed.Root.Profile,
		CliOverrides: overrides,
		StrictConfig: parsed.Root.StrictConfig,
	})
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 1
	}
	resolved := resolveEffectiveFeatures(result.Config.Features)

	type row struct {
		name    string
		stage   string
		enabled bool
	}
	rows := make([]row, 0, len(features.FEATURES))
	nameWidth, stageWidth := 0, 0
	for _, def := range features.FEATURES {
		stage := stageString(def.Stage)
		rows = append(rows, row{name: def.Key, stage: stage, enabled: resolved.Enabled(def.ID)})
		nameWidth = max(nameWidth, len(def.Key))
		stageWidth = max(stageWidth, len(stage))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

	for _, r := range rows {
		fmt.Fprintf(streams.Stdout, "%-*s  %-*s  %t\n", nameWidth, r.name, stageWidth, r.stage, r.enabled)
	}
	return 0
}

// resolveEffectiveFeatures merges the configured [features] table over the
// defaults, mirroring how Config resolves config.features for the list view.
func resolveEffectiveFeatures(configured *features.FeaturesToml) features.Features {
	resolved := features.NewFeaturesWithDefaults()
	if configured != nil {
		resolved.ApplyMap(configured.Entries())
	}
	resolved.NormalizeDependencies()
	return resolved
}

// runFeaturesSet enables or disables a feature in config.toml, mirroring
// enable_feature_in_config / disable_feature_in_config.
func runFeaturesSet(parsed ParsedCommandLine, streams Streams, args []string, enabled bool) int {
	verb := "enable"
	if !enabled {
		verb = "disable"
	}
	if len(args) != 1 {
		fmt.Fprintf(streams.Stderr, "error: features %s requires exactly one FEATURE argument\n", verb)
		return 1
	}
	feature := args[0]
	if !features.IsKnownFeatureKey(feature) {
		fmt.Fprintf(streams.Stderr, "error: Unknown feature flag: %s\n", feature)
		return 1
	}

	codexHome, err := config.FindCodexHome()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 1
	}
	if err := setFeatureEnabledInConfig(codexHome, feature, enabled); err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 1
	}

	if enabled {
		fmt.Fprintf(streams.Stdout, "Enabled feature `%s` in config.toml.\n", feature)
		maybePrintUnderDevelopmentWarning(streams, codexHome, feature)
	} else {
		fmt.Fprintf(streams.Stdout, "Disabled feature `%s` in config.toml.\n", feature)
	}
	return 0
}

// maybePrintUnderDevelopmentWarning warns when enabling an under-development
// feature, mirroring maybe_print_under_development_feature_warning.
func maybePrintUnderDevelopmentWarning(streams Streams, codexHome, feature string) {
	for _, spec := range features.FEATURES {
		if spec.Key != feature {
			continue
		}
		if spec.Stage.Kind != features.StageUnderDevelopment {
			return
		}
		fmt.Fprintf(streams.Stderr,
			"Under-development features enabled: %s. Under-development features are incomplete and may behave unpredictably. To suppress this warning, set `suppress_unstable_features_warning = true` in %s.\n",
			feature, config.ConfigTomlPath(codexHome))
		return
	}
}

// stageString renders a feature stage to its lowercase human label, matching
// stage_str in main.rs.
func stageString(stage features.Stage) string {
	switch stage.Kind {
	case features.StageUnderDevelopment:
		return "under development"
	case features.StageExperimental:
		return "experimental"
	case features.StageStable:
		return "stable"
	case features.StageDeprecated:
		return "deprecated"
	case features.StageRemoved:
		return "removed"
	default:
		return "unknown"
	}
}

func printFeaturesHelp(w io.Writer) {
	fmt.Fprintln(w, "Inspect and toggle feature flags")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex features <COMMAND>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  list               List known features with stage and effective state")
	fmt.Fprintln(w, "  enable <FEATURE>   Enable a feature in config.toml")
	fmt.Fprintln(w, "  disable <FEATURE>  Disable a feature in config.toml")
}
