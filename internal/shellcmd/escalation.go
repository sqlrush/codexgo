package shellcmd

import "path/filepath"

// Escalator identifies a privilege-escalation wrapper program.
type Escalator int

const (
	// EscalatorNone means the command is not an escalation wrapper.
	EscalatorNone Escalator = iota
	// EscalatorSudo is the sudo wrapper.
	EscalatorSudo
	// EscalatorSu is the su wrapper.
	EscalatorSu
	// EscalatorDoas is the doas wrapper.
	EscalatorDoas
)

// String returns the wrapper's program name, or "" for EscalatorNone.
func (e Escalator) String() string {
	switch e {
	case EscalatorSudo:
		return "sudo"
	case EscalatorSu:
		return "su"
	case EscalatorDoas:
		return "doas"
	default:
		return ""
	}
}

// escalatorForName classifies a command name (after taking its basename) as a
// known escalation wrapper. The basename is taken so that absolute paths like
// "/usr/bin/sudo" are recognized, matching how codex resolves executable names.
func escalatorForName(name string) Escalator {
	switch filepath.Base(name) {
	case "sudo":
		return EscalatorSudo
	case "su":
		return EscalatorSu
	case "doas":
		return EscalatorDoas
	default:
		return EscalatorNone
	}
}

// DetectEscalation reports whether a tokenized command begins with a
// privilege-escalation wrapper (sudo, su, or doas) and returns the wrapper kind
// alongside the wrapped command (the tokens following the wrapper and its own
// options).
//
// codex itself only special-cases `sudo` inside is_dangerous_to_call_with_exec;
// this helper generalizes the same idea to su and doas, which is the detection
// the codexgo task asks for. The returned wrapped-command slice shares backing
// storage with the input but is never mutated.
func DetectEscalation(command []string) (kind Escalator, wrapped []string, ok bool) {
	if len(command) == 0 {
		return EscalatorNone, nil, false
	}
	kind = escalatorForName(command[0])
	if kind == EscalatorNone {
		return EscalatorNone, nil, false
	}
	return kind, command[1:], true
}

// CommandMightBeDangerous reports whether a tokenized command, or any plain
// command inside a `bash -lc "<script>"` invocation, might be dangerous to run
// with exec. It mirrors command_might_be_dangerous in is_dangerous_command.rs
// for the non-Windows path: `rm -f`/`rm -rf` is dangerous, and `sudo <cmd>`
// defers to the danger check on <cmd>.
//
// The input slice is not mutated.
func CommandMightBeDangerous(command []string) bool {
	if isDangerousToCallWithExec(command) {
		return true
	}
	if all, ok := ParseShellLcPlainCommands(command); ok {
		for _, cmd := range all {
			if isDangerousToCallWithExec(cmd) {
				return true
			}
		}
	}
	return false
}

// isDangerousToCallWithExec mirrors is_dangerous_to_call_with_exec in
// is_dangerous_command.rs: `rm` with a `-f` or `-rf` first argument is
// dangerous, and `sudo <cmd>` recurses into <cmd>.
func isDangerousToCallWithExec(command []string) bool {
	if len(command) == 0 {
		return false
	}
	switch command[0] {
	case "rm":
		if len(command) >= 2 {
			return command[1] == "-f" || command[1] == "-rf"
		}
		return false
	case "sudo":
		return isDangerousToCallWithExec(command[1:])
	default:
		return false
	}
}
