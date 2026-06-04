package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// doctorArgs holds the parsed `codex doctor` flags.
type doctorArgs struct {
	json    bool
	verbose bool
	help    bool
}

// runDoctorSubcommand handles `codex doctor [--json] [--verbose]`. It runs the
// full bounded diagnostic set, renders the report, and exits 1 when any check
// failed (overall status fail), matching run_doctor in doctor.rs.
func runDoctorSubcommand(ctx context.Context, parsed ParsedCommandLine, streams Streams) int {
	args, err := parseDoctorArgs(parsed.SubcommandArgs)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 1
	}
	if args.help {
		printDoctorHelp(streams.Stdout)
		return 0
	}

	checks := buildDoctorChecks(ctx, parsed.Root)
	report := newDoctorReport(Version, checks, time.Now())

	if args.json {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(streams.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Fprintln(streams.Stdout, string(data))
	} else {
		renderDoctorHuman(streams.Stdout, report, args.verbose)
	}

	if report.OverallStatus == statusFail {
		return 1
	}
	return 0
}

// renderDoctorHuman writes the human-readable report. Detail lines are printed
// only in verbose mode (matching --summary vs detailed output semantics).
func renderDoctorHuman(w io.Writer, report doctorReport, verbose bool) {
	fmt.Fprintf(w, "Codex doctor — codex %s\n", report.CodexVersion)
	fmt.Fprintf(w, "Generated at %s\n\n", report.GeneratedAt)

	counts := map[checkStatus]int{}
	for _, c := range report.Checks {
		counts[c.Status]++
		fmt.Fprintf(w, "%s  %-15s %s\n", statusLabel(c.Status), c.Category, c.Summary)
		if verbose {
			for _, d := range c.Details {
				fmt.Fprintf(w, "        %s\n", d)
			}
		}
		if c.Remediation != nil {
			fmt.Fprintf(w, "        -> %s\n", *c.Remediation)
		}
	}

	fmt.Fprintf(w, "\nSummary: %d ok, %d warning, %d fail (overall: %s)\n",
		counts[statusOK], counts[statusWarning], counts[statusFail], report.OverallStatus)
}

// statusLabel renders a fixed-width status label for the human report.
func statusLabel(s checkStatus) string {
	switch s {
	case statusOK:
		return "[ OK ]"
	case statusWarning:
		return "[WARN]"
	case statusFail:
		return "[FAIL]"
	case statusSkipped:
		return "[SKIP]"
	default:
		return "[????]"
	}
}

// parseDoctorArgs parses the doctor flags.
func parseDoctorArgs(args []string) (doctorArgs, error) {
	var out doctorArgs
	for _, arg := range args {
		switch arg {
		case "--json":
			out.json = true
		case "--verbose", "--all":
			out.verbose = true
		case "-h", "--help":
			out.help = true
		default:
			if strings.HasPrefix(arg, "-") {
				return doctorArgs{}, fmt.Errorf("unexpected flag: %s", arg)
			}
			return doctorArgs{}, fmt.Errorf("unexpected argument: %s", arg)
		}
	}
	return out, nil
}

func printDoctorHelp(w io.Writer) {
	fmt.Fprintln(w, "Diagnose local Codex installation, config, auth, and runtime health")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex doctor [OPTIONS]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "      --json      Emit a machine-readable report")
	fmt.Fprintln(w, "      --verbose   Expand per-check detail lines")
	fmt.Fprintln(w, "  -h, --help      Print help")
}
