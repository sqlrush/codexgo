package exec

import (
	"fmt"
	"strings"
)

// Subcommand identifies the exec action: a default new session, a resume, or a
// review. Mirrors the Rust Command enum (with the default being no subcommand).
type Subcommand int

const (
	// SubcommandRun is the default non-interactive session.
	SubcommandRun Subcommand = iota
	// SubcommandResume resumes a previous session.
	SubcommandResume
	// SubcommandReview runs a code review.
	SubcommandReview
)

// CLI is the parsed codex-exec command line. It captures the flags this port
// supports faithfully to codex exec's surface; richer config-override flags are
// out of scope for this stage and are accepted-and-ignored when prefixed config
// overrides are not present.
type CLI struct {
	// Subcommand selects the action.
	Subcommand Subcommand

	// JSON enables JSONL (one event per line) output instead of human text.
	JSON bool
	// OutputLastMessage is the file to write the final agent message to (-o).
	OutputLastMessage string
	// OutputSchema is the path to a JSON Schema file constraining the final
	// model response.
	OutputSchema string

	// Prompt is the positional prompt (nil when omitted, "-" forces stdin).
	Prompt *string
	// Images are local image paths attached to the prompt (--image/-i).
	Images []string
	// Cwd is the working directory for the run (-C/--cd), empty when unset. The
	// turn's commands and apply_patch resolve relative paths against it, matching
	// codex exec's `-C` flag.
	Cwd string

	// Resume fields (only meaningful for SubcommandResume).
	ResumeSessionID string
	ResumeLast      bool

	// Review fields (only meaningful for SubcommandReview).
	ReviewUncommitted bool
	ReviewBase        string
	ReviewCommit      string
	ReviewTitle       string
}

// ParseArgs parses codex-exec arguments (excluding the program name). It returns
// the parsed CLI or an error describing the misuse. Flags may appear before or
// after the (optional) subcommand and positional prompt, matching clap's global
// flags.
func ParseArgs(args []string) (CLI, error) {
	cli := CLI{}
	// Detect a leading subcommand.
	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		switch rest[0] {
		case "resume":
			cli.Subcommand = SubcommandResume
			rest = rest[1:]
		case "review":
			cli.Subcommand = SubcommandReview
			rest = rest[1:]
		}
	}

	var positionals []string
	i := 0
	for i < len(rest) {
		arg := rest[i]
		switch {
		case arg == "--":
			positionals = append(positionals, rest[i+1:]...)
			i = len(rest)
		case arg == "--json" || arg == "--experimental-json":
			cli.JSON = true
			i++
		case arg == "--last":
			cli.ResumeLast = true
			i++
		case arg == "--all":
			// Accepted for compatibility; cwd filtering is not applied here.
			i++
		case arg == "--uncommitted":
			cli.ReviewUncommitted = true
			i++
		case arg == "-o" || arg == "--output-last-message":
			v, ni, err := flagValue(rest, i, arg)
			if err != nil {
				return CLI{}, err
			}
			cli.OutputLastMessage, i = v, ni
		case strings.HasPrefix(arg, "--output-last-message="):
			cli.OutputLastMessage = strings.TrimPrefix(arg, "--output-last-message=")
			i++
		case arg == "--output-schema":
			v, ni, err := flagValue(rest, i, arg)
			if err != nil {
				return CLI{}, err
			}
			cli.OutputSchema, i = v, ni
		case strings.HasPrefix(arg, "--output-schema="):
			cli.OutputSchema = strings.TrimPrefix(arg, "--output-schema=")
			i++
		case arg == "-i" || arg == "--image":
			v, ni, err := flagValue(rest, i, arg)
			if err != nil {
				return CLI{}, err
			}
			cli.Images = append(cli.Images, splitCSV(v)...)
			i = ni
		case strings.HasPrefix(arg, "--image="):
			cli.Images = append(cli.Images, splitCSV(strings.TrimPrefix(arg, "--image="))...)
			i++
		case arg == "-C" || arg == "--cd":
			v, ni, err := flagValue(rest, i, arg)
			if err != nil {
				return CLI{}, err
			}
			cli.Cwd, i = v, ni
		case strings.HasPrefix(arg, "--cd="):
			cli.Cwd = strings.TrimPrefix(arg, "--cd=")
			i++
		case arg == "--base":
			v, ni, err := flagValue(rest, i, arg)
			if err != nil {
				return CLI{}, err
			}
			cli.ReviewBase, i = v, ni
		case arg == "--commit":
			v, ni, err := flagValue(rest, i, arg)
			if err != nil {
				return CLI{}, err
			}
			cli.ReviewCommit, i = v, ni
		case arg == "--title":
			v, ni, err := flagValue(rest, i, arg)
			if err != nil {
				return CLI{}, err
			}
			cli.ReviewTitle, i = v, ni
		case strings.HasPrefix(arg, "-") && arg != "-":
			// Unknown flag: tolerate global/config flags this stage does not model
			// rather than failing, but consume an attached "=value" form cleanly.
			i++
		default:
			positionals = append(positionals, arg)
			i++
		}
	}

	return cli.applyPositionals(positionals)
}

// applyPositionals assigns the parsed positionals to the right CLI fields based
// on the subcommand, replicating clap's positional handling (resume takes an
// optional session id before the prompt; --last reinterprets the positional).
func (c CLI) applyPositionals(pos []string) (CLI, error) {
	switch c.Subcommand {
	case SubcommandResume:
		// resume [SESSION_ID] [PROMPT]; with --last and no explicit prompt the
		// single positional is the prompt.
		switch len(pos) {
		case 0:
		case 1:
			if c.ResumeLast {
				p := pos[0]
				c.Prompt = &p
			} else {
				c.ResumeSessionID = pos[0]
			}
		default:
			c.ResumeSessionID = pos[0]
			p := pos[1]
			c.Prompt = &p
		}
	case SubcommandReview:
		if len(pos) > 0 {
			p := strings.Join(pos, " ")
			c.Prompt = &p
		}
	default:
		if len(pos) > 0 {
			p := strings.Join(pos, " ")
			c.Prompt = &p
		}
	}
	return c, nil
}

// flagValue returns the value following a value-taking flag at index i and the
// next index to resume parsing from.
func flagValue(args []string, i int, name string) (string, int, error) {
	if i+1 >= len(args) {
		return "", 0, fmt.Errorf("exec: flag %s requires a value", name)
	}
	return args[i+1], i + 2, nil
}

// splitCSV splits a comma-separated value list, trimming empties.
func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
