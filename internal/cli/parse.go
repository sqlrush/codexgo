package cli

import (
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/pkg/features"
)

// RootOptions holds the parsed top-level flags shared by every subcommand. They
// mirror the flattened option groups on MultitoolCli in main.rs:
// CliConfigOverrides (-c), the -p/--profile profile name, FeatureToggles
// (--enable/--disable), InteractiveRemoteOptions (--remote /
// --remote-auth-token-env), and --strict-config.
type RootOptions struct {
	// Overrides accumulates -c key=value plus the --enable/--disable toggles
	// folded into features.<name>=true/false (matching FeatureToggles::to_overrides).
	Overrides ConfigOverrides
	// Profile is the -p/--profile profile-v2 name (empty when unset).
	Profile string
	// Remote is the --remote endpoint (empty when unset).
	Remote string
	// RemoteAuthTokenEnv is the --remote-auth-token-env env var name (empty when unset).
	RemoteAuthTokenEnv string
	// StrictConfig is the --strict-config flag.
	StrictConfig bool
	// ShowHelp/ShowVersion request the top-level help/version output.
	ShowHelp    bool
	ShowVersion bool
}

// ParsedCommandLine is the result of splitting argv (excluding the program name)
// into the root options, the selected subcommand name (empty when none), and the
// remaining args to forward to that subcommand.
type ParsedCommandLine struct {
	Root           RootOptions
	Subcommand     string
	SubcommandArgs []string
}

// ParseCommandLine parses the top-level flags up to the first non-flag token,
// which it treats as the subcommand name; everything after it is forwarded
// verbatim to the subcommand. Global flags (-c, --enable, --disable, --remote,
// --remote-auth-token-env, --strict-config, -p/--profile) may appear before the
// subcommand. `--` terminates root flag parsing and forces the next token to be
// treated as the subcommand/positional stream.
//
// It mirrors clap's top-level parse where global flags are collected and the
// subcommand (or default no-subcommand path) is dispatched. Unknown leading
// flags are forwarded to the default path rather than rejected, so prompts and
// TUI-only flags still reach the no-subcommand notice.
func ParseCommandLine(args []string) (ParsedCommandLine, error) {
	root := RootOptions{}
	var enable, disable []string

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--":
			// Everything after `--` is forwarded; the next token (if any) is the
			// subcommand/positional stream.
			i++
			return finalize(root, enable, disable, args[i:])
		case arg == "-h" || arg == "--help":
			root.ShowHelp = true
			i++
		case arg == "-V" || arg == "--version":
			root.ShowVersion = true
			i++
		case arg == "--strict-config":
			root.StrictConfig = true
			i++
		case arg == "-c" || arg == "--config":
			v, ni, err := takeValue(args, i, arg)
			if err != nil {
				return ParsedCommandLine{}, err
			}
			root.Overrides = root.Overrides.Append(v)
			i = ni
		case strings.HasPrefix(arg, "-c="):
			root.Overrides = root.Overrides.Append(strings.TrimPrefix(arg, "-c="))
			i++
		case strings.HasPrefix(arg, "--config="):
			root.Overrides = root.Overrides.Append(strings.TrimPrefix(arg, "--config="))
			i++
		case arg == "-p" || arg == "--profile":
			v, ni, err := takeValue(args, i, arg)
			if err != nil {
				return ParsedCommandLine{}, err
			}
			root.Profile, i = v, ni
		case strings.HasPrefix(arg, "--profile="):
			root.Profile = strings.TrimPrefix(arg, "--profile=")
			i++
		case strings.HasPrefix(arg, "-p="):
			root.Profile = strings.TrimPrefix(arg, "-p=")
			i++
		case arg == "--enable":
			v, ni, err := takeValue(args, i, arg)
			if err != nil {
				return ParsedCommandLine{}, err
			}
			enable, i = append(enable, v), ni
		case strings.HasPrefix(arg, "--enable="):
			enable = append(enable, strings.TrimPrefix(arg, "--enable="))
			i++
		case arg == "--disable":
			v, ni, err := takeValue(args, i, arg)
			if err != nil {
				return ParsedCommandLine{}, err
			}
			disable, i = append(disable, v), ni
		case strings.HasPrefix(arg, "--disable="):
			disable = append(disable, strings.TrimPrefix(arg, "--disable="))
			i++
		case arg == "--remote":
			v, ni, err := takeValue(args, i, arg)
			if err != nil {
				return ParsedCommandLine{}, err
			}
			root.Remote, i = v, ni
		case strings.HasPrefix(arg, "--remote="):
			root.Remote = strings.TrimPrefix(arg, "--remote=")
			i++
		case arg == "--remote-auth-token-env":
			v, ni, err := takeValue(args, i, arg)
			if err != nil {
				return ParsedCommandLine{}, err
			}
			root.RemoteAuthTokenEnv, i = v, ni
		case strings.HasPrefix(arg, "--remote-auth-token-env="):
			root.RemoteAuthTokenEnv = strings.TrimPrefix(arg, "--remote-auth-token-env=")
			i++
		case strings.HasPrefix(arg, "-") && arg != "-":
			// Unknown leading flag: stop root parsing and forward the rest to the
			// default (no-subcommand) path, matching clap forwarding TUI-only flags.
			return finalize(root, enable, disable, args[i:])
		default:
			// First non-flag token is the subcommand (or a default-path positional).
			return finalizeWithSubcommand(root, enable, disable, args[i:])
		}
	}

	return finalize(root, enable, disable, nil)
}

// finalizeWithSubcommand treats rest[0] as the subcommand name when it matches a
// known subcommand; otherwise the whole rest stream is forwarded to the default
// (no-subcommand) path as positionals.
func finalizeWithSubcommand(root RootOptions, enable, disable, rest []string) (ParsedCommandLine, error) {
	if len(rest) == 0 {
		return finalize(root, enable, disable, nil)
	}
	name, canonical := canonicalSubcommand(rest[0])
	if !canonical {
		// Not a subcommand: forward everything to the default path.
		return finalize(root, enable, disable, rest)
	}
	pcl, err := finalize(root, enable, disable, rest[1:])
	if err != nil {
		return ParsedCommandLine{}, err
	}
	pcl.Subcommand = name
	return pcl, nil
}

// finalize folds the feature toggles into config overrides and returns the parsed
// command line with the given forwarded args.
func finalize(root RootOptions, enable, disable, rest []string) (ParsedCommandLine, error) {
	toggles, err := featureToggleOverrides(enable, disable)
	if err != nil {
		return ParsedCommandLine{}, err
	}
	root.Overrides = root.Overrides.Append(toggles...)
	return ParsedCommandLine{Root: root, SubcommandArgs: rest}, nil
}

// featureToggleOverrides converts --enable/--disable into features.<name>=bool
// override strings, validating each against the known feature registry. Mirrors
// FeatureToggles::to_overrides and validate_feature.
func featureToggleOverrides(enable, disable []string) ([]string, error) {
	out := make([]string, 0, len(enable)+len(disable))
	for _, feature := range enable {
		if !features.IsKnownFeatureKey(feature) {
			return nil, fmt.Errorf("Unknown feature flag: %s", feature)
		}
		out = append(out, "features."+feature+"=true")
	}
	for _, feature := range disable {
		if !features.IsKnownFeatureKey(feature) {
			return nil, fmt.Errorf("Unknown feature flag: %s", feature)
		}
		out = append(out, "features."+feature+"=false")
	}
	return out, nil
}

// takeValue returns the value for a flag that takes a separate argument.
func takeValue(args []string, i int, name string) (string, int, error) {
	if i+1 >= len(args) {
		return "", 0, fmt.Errorf("flag %s requires a value", name)
	}
	return args[i+1], i + 2, nil
}
