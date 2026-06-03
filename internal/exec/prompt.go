package exec

import (
	"fmt"
	"io"
	"strings"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// stdinBehavior selects how stdin participates in prompt resolution, mirroring
// the Rust StdinPromptBehavior.
type stdinBehavior int

const (
	// behaviorRequiredIfPiped reads stdin as the prompt only when it is piped
	// (not a terminal); a terminal stdin is an error.
	behaviorRequiredIfPiped stdinBehavior = iota
	// behaviorForced always reads stdin (the explicit "-" prompt).
	behaviorForced
	// behaviorOptionalAppend reads piped stdin as an appended <stdin> block.
	behaviorOptionalAppend
)

// promptEnv abstracts stdin so prompt resolution is unit-testable.
type promptEnv struct {
	// stdin is the input reader.
	stdin io.Reader
	// stdinIsTerminal reports whether stdin is an interactive terminal.
	stdinIsTerminal bool
	// errOut receives the informational "Reading prompt from stdin..." notices.
	errOut io.Writer
}

// resolveRootPrompt resolves the prompt for a default/run session: a non-"-"
// positional is used as-is, with any piped stdin appended as a <stdin> block; a
// missing or "-" positional falls back to reading the prompt from stdin.
// It returns the prompt text or an error describing why no prompt is available.
func resolveRootPrompt(env promptEnv, prompt *string) (string, error) {
	if prompt != nil && *prompt != "-" {
		stdinText, err := readStdin(env, behaviorOptionalAppend)
		if err != nil {
			return "", err
		}
		if stdinText != nil {
			return promptWithStdinContext(*prompt, *stdinText), nil
		}
		return *prompt, nil
	}
	return resolvePrompt(env, prompt)
}

// resolvePrompt resolves the prompt for the resume/review paths: a non-"-"
// positional is used verbatim; otherwise the prompt is read from stdin (forced
// for "-", required-if-piped otherwise).
func resolvePrompt(env promptEnv, prompt *string) (string, error) {
	if prompt != nil && *prompt != "-" {
		return *prompt, nil
	}
	behavior := behaviorRequiredIfPiped
	if prompt != nil && *prompt == "-" {
		behavior = behaviorForced
	}
	text, err := readStdin(env, behavior)
	if err != nil {
		return "", err
	}
	if text == nil {
		return "", fmt.Errorf("no prompt provided via stdin")
	}
	return *text, nil
}

// readStdin reads the prompt from stdin per behavior. It returns nil (no error)
// when behavior is optional-append and no usable input is available, and an
// error when a required prompt is missing or stdin cannot be read.
func readStdin(env promptEnv, behavior stdinBehavior) (*string, error) {
	switch behavior {
	case behaviorRequiredIfPiped:
		if env.stdinIsTerminal {
			return nil, fmt.Errorf("no prompt provided. Either specify one as an argument or pipe the prompt into stdin")
		}
		fmt.Fprintln(env.errOut, "Reading prompt from stdin...")
	case behaviorForced:
		// Always read.
	case behaviorOptionalAppend:
		if env.stdinIsTerminal {
			return nil, nil
		}
		fmt.Fprintln(env.errOut, "Reading additional input from stdin...")
	}

	raw, err := io.ReadAll(env.stdin)
	if err != nil {
		return nil, fmt.Errorf("failed to read prompt from stdin: %w", err)
	}
	buffer := string(raw)
	if strings.TrimSpace(buffer) == "" {
		if behavior == behaviorOptionalAppend {
			return nil, nil
		}
		return nil, fmt.Errorf("no prompt provided via stdin")
	}
	return &buffer, nil
}

// promptWithStdinContext appends piped stdin to the positional prompt as a
// <stdin> block, matching prompt_with_stdin_context.
func promptWithStdinContext(prompt, stdinText string) string {
	var b strings.Builder
	b.WriteString(prompt)
	b.WriteString("\n\n<stdin>\n")
	b.WriteString(stdinText)
	if !strings.HasSuffix(stdinText, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("</stdin>")
	return b.String()
}

// buildInput assembles the turn input from local image paths and the prompt
// text, mirroring the Rust ordering (images first, then the text item).
func buildInput(images []string, text string) []appserverproto.UserInput {
	input := make([]appserverproto.UserInput, 0, len(images)+1)
	for _, path := range images {
		input = append(input, appserverproto.UserInput{
			Kind: appserverproto.UserInputKindLocalImage,
			Path: path,
		})
	}
	input = append(input, appserverproto.UserInput{
		Kind: appserverproto.UserInputKindText,
		Text: text,
	})
	return input
}
