package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/sqlrush/codexgo/internal/cloud"
	"github.com/sqlrush/codexgo/internal/login"
)

// runCloudSubcommand handles `codex cloud` (alias `cloud-tasks`): browse tasks
// from Codex Cloud and apply changes locally. It mirrors the Rust
// codex_cloud_tasks::run_main dispatch surface, wiring the offline-capable mock
// backend (selected via CODEXGO_CLOUD_TASKS_MODE=mock) and the authenticated HTTP
// backend.
//
// The interactive task browser (the Rust default with no subcommand) is a TUI
// surface; this port wires the non-interactive `list` and `apply` actions. When
// the HTTP backend is selected but no ChatGPT/API-key authentication is present,
// it prints a clear "authentication required" notice and exits non-zero. The
// subcommand and its --help are always registered.
func runCloudSubcommand(ctx context.Context, parsed ParsedCommandLine, streams Streams) int {
	args, err := parseCloudArgs(parsed.SubcommandArgs)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 2
	}
	if args.help {
		printCloudHelp(streams.Stdout)
		return 0
	}

	backend, err := resolveCloudBackend(ctx, streams)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "codex cloud: %v\n", err)
		return 1
	}

	switch args.action {
	case cloudActionList:
		return runCloudList(ctx, backend, args, streams)
	case cloudActionApply:
		return runCloudApply(ctx, backend, args, streams)
	default:
		// No subcommand: the Rust default opens the interactive task browser,
		// which is a TUI surface not yet ported. Emit a clear notice rather than
		// silently doing nothing.
		fmt.Fprintln(streams.Stderr,
			"`codex cloud` with no action opens the interactive task browser (a TUI surface not yet ported);"+
				" use `codex cloud list` or `codex cloud apply <TASK_ID>`.")
		return 1
	}
}

// cloudAction selects the non-interactive cloud action.
type cloudAction int

const (
	cloudActionNone cloudAction = iota
	cloudActionList
	cloudActionApply
)

// cloudArgs holds the parsed `codex cloud` flags.
type cloudArgs struct {
	action cloudAction
	help   bool
	// env filters list output by environment id (--env).
	env string
	// limit bounds list output (--limit).
	limit *int64
	// taskID is the positional task id for apply.
	taskID string
	// preflight requests an apply dry-run (--preflight / --dry-run).
	preflight bool
}

// parseCloudArgs parses `codex cloud [list|apply] [OPTIONS] [TASK_ID]`.
func parseCloudArgs(args []string) (cloudArgs, error) {
	var out cloudArgs
	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		switch rest[0] {
		case "list":
			out.action = cloudActionList
			rest = rest[1:]
		case "apply":
			out.action = cloudActionApply
			rest = rest[1:]
		}
	}

	i := 0
	for i < len(rest) {
		arg := rest[i]
		switch {
		case arg == "-h" || arg == "--help":
			out.help = true
			i++
		case arg == "--env":
			v, ni, err := takeValue(rest, i, arg)
			if err != nil {
				return cloudArgs{}, err
			}
			out.env, i = v, ni
		case strings.HasPrefix(arg, "--env="):
			out.env = strings.TrimPrefix(arg, "--env=")
			i++
		case arg == "--limit":
			v, ni, err := takeValue(rest, i, arg)
			if err != nil {
				return cloudArgs{}, err
			}
			n, perr := strconv.ParseInt(v, 10, 64)
			if perr != nil {
				return cloudArgs{}, fmt.Errorf("invalid --limit value %q: %w", v, perr)
			}
			out.limit, i = &n, ni
		case arg == "--preflight" || arg == "--dry-run":
			out.preflight = true
			i++
		case strings.HasPrefix(arg, "-") && arg != "-":
			return cloudArgs{}, fmt.Errorf("unexpected flag: %s", arg)
		default:
			if out.taskID != "" {
				return cloudArgs{}, fmt.Errorf("unexpected extra argument: %s", arg)
			}
			out.taskID = arg
			i++
		}
	}
	return out, nil
}

// resolveCloudBackend selects the cloud backend: the offline mock backend when
// CODEXGO_CLOUD_TASKS_MODE=mock, otherwise the authenticated HTTP backend. When
// HTTP auth is missing it returns an authentication-required error.
func resolveCloudBackend(ctx context.Context, _ Streams) (cloud.CloudBackend, error) {
	if cloud.MockModeEnabled() {
		return cloud.MockClient{}, nil
	}

	codexHome := resolveCodexHome()
	auth, err := login.LoadAuth(ctx, nil, codexHome, true, resolveStoreMode(nil), nil)
	if err != nil {
		return nil, fmt.Errorf("loading authentication: %w", err)
	}
	if auth == nil {
		return nil, fmt.Errorf(
			"Codex Cloud requires ChatGPT or API-key authentication; run `codex login` " +
				"(or set CODEXGO_CLOUD_TASKS_MODE=mock to use the offline mock backend)")
	}
	return cloud.NewHTTPClientFromAuth(cloud.BaseURLFromEnv(), auth, "codex-cli-go"), nil
}

// runCloudList lists cloud tasks.
func runCloudList(ctx context.Context, backend cloud.CloudBackend, args cloudArgs, streams Streams) int {
	var envFilter *string
	if args.env != "" {
		envFilter = &args.env
	}
	page, err := backend.ListTasks(ctx, envFilter, args.limit, nil)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "codex cloud list: %v\n", err)
		return 1
	}
	if len(page.Tasks) == 0 {
		fmt.Fprintln(streams.Stdout, "No tasks found.")
		return 0
	}
	for _, task := range page.Tasks {
		fmt.Fprintf(streams.Stdout, "%s\t%s\t%s\n", task.ID, task.Status, task.Title)
	}
	return 0
}

// runCloudApply applies (or preflights) a cloud task's diff to the working tree.
func runCloudApply(ctx context.Context, backend cloud.CloudBackend, args cloudArgs, streams Streams) int {
	if args.taskID == "" {
		fmt.Fprintln(streams.Stderr, "error: apply requires a TASK_ID")
		return 2
	}
	id := cloud.TaskID(args.taskID)
	var (
		outcome cloud.ApplyOutcome
		err     error
	)
	if args.preflight {
		outcome, err = backend.ApplyTaskPreflight(ctx, id, nil)
	} else {
		outcome, err = backend.ApplyTask(ctx, id, nil)
	}
	if err != nil {
		fmt.Fprintf(streams.Stderr, "codex cloud apply: %v\n", err)
		return 1
	}
	fmt.Fprintf(streams.Stdout, "apply status: %s\n", outcome.Status)
	if outcome.Message != "" {
		fmt.Fprintln(streams.Stdout, outcome.Message)
	}
	if outcome.Status == cloud.ApplyStatusError {
		return 1
	}
	return 0
}

func printCloudHelp(w io.Writer) {
	fmt.Fprintln(w, "[EXPERIMENTAL] Browse tasks from Codex Cloud and apply changes locally")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex cloud [COMMAND] [OPTIONS] [TASK_ID]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  list           List cloud tasks")
	fmt.Fprintln(w, "  apply          Apply a cloud task's diff to the working tree")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "      --env <ID>     Filter tasks by environment id (list)")
	fmt.Fprintln(w, "      --limit <N>    Maximum number of tasks to list")
	fmt.Fprintln(w, "      --preflight    Dry-run the apply without writing changes")
	fmt.Fprintln(w, "  -h, --help         Print help")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Authentication: run `codex login`, or set CODEXGO_CLOUD_TASKS_MODE=mock for the offline mock backend.")
}
