package cli

import "strings"

// bashCompletionScript renders the bash completion script for `codex`, mirroring
// clap_complete v4.5.65's bash.rs generator (the `Bash::try_generate` template
// plus all_subcommands / subcommand_details / option_details_for_path). The
// command tree is supplied by completion_tree.go, which was extracted
// byte-for-byte from the reference binary's output.
//
// The bin name is hard-coded to "codex" because codexgo is the drop-in
// replacement: the generated script must register completions for `codex`.
func bashCompletionScript() string {
	const binName = "codex"
	const cmdFn = "codex" // bin_name.replace('-', "__")

	var b strings.Builder

	// Header: mirrors the literal write! template in Bash::try_generate.
	b.WriteString("_" + binName + "() {\n")
	b.WriteString("    local i cur prev opts cmd\n")
	b.WriteString("    COMPREPLY=()\n")
	b.WriteString("    if [[ \"${BASH_VERSINFO[0]}\" -ge 4 ]]; then\n")
	b.WriteString("        cur=\"$2\"\n")
	b.WriteString("    else\n")
	b.WriteString("        cur=\"${COMP_WORDS[COMP_CWORD]}\"\n")
	b.WriteString("    fi\n")
	b.WriteString("    prev=\"$3\"\n")
	b.WriteString("    cmd=\"\"\n")
	b.WriteString("    opts=\"\"\n")
	b.WriteString("\n")
	b.WriteString("    for i in \"${COMP_WORDS[@]:0:COMP_CWORD}\"\n")
	b.WriteString("    do\n")
	b.WriteString("        case \"${cmd},${i}\" in\n")
	b.WriteString("            \",$1\")\n")
	b.WriteString("                cmd=\"" + cmdFn + "\"\n")
	b.WriteString("                ;;")
	b.WriteString(bashSubcmds())
	b.WriteString("\n")
	b.WriteString("            *)\n")
	b.WriteString("                ;;\n")
	b.WriteString("        esac\n")
	b.WriteString("    done\n")
	b.WriteString("\n")
	b.WriteString("    case \"${cmd}\" in\n")

	// The root `codex` arm is rendered in the {cmd} slot (always level 1), and
	// the remaining nodes follow as subcmd_details. completionTree[0] is the root.
	root := completionTree[0]
	b.WriteString("        " + cmdFn + ")\n")
	b.WriteString("            opts=\"" + root.Opts + "\"\n")
	b.WriteString("            if [[ ${cur} == -* || ${COMP_CWORD} -eq 1 ]] ; then\n")
	b.WriteString("                COMPREPLY=( $(compgen -W \"${opts}\" -- \"${cur}\") )\n")
	b.WriteString("                return 0\n")
	b.WriteString("            fi\n")
	b.WriteString("            case \"${prev}\" in")
	b.WriteString(bashOptionDetails(root.Flags))
	b.WriteString("\n")
	b.WriteString("                *)\n")
	b.WriteString("                    COMPREPLY=()\n")
	b.WriteString("                    ;;\n")
	b.WriteString("            esac\n")
	b.WriteString("            COMPREPLY=( $(compgen -W \"${opts}\" -- \"${cur}\") )\n")
	b.WriteString("            return 0\n")
	b.WriteString("            ;;")

	for _, node := range completionTree[1:] {
		b.WriteString(bashSubcmdArm(node))
	}

	b.WriteString("\n")
	b.WriteString("    esac\n")
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("if [[ \"${BASH_VERSINFO[0]}\" -eq 4 && \"${BASH_VERSINFO[1]}\" -ge 4 || \"${BASH_VERSINFO[0]}\" -gt 4 ]]; then\n")
	b.WriteString("    complete -F _" + binName + " -o nosort -o bashdefault -o default " + binName + "\n")
	b.WriteString("else\n")
	b.WriteString("    complete -F _" + binName + " -o bashdefault -o default " + binName + "\n")
	b.WriteString("fi\n")

	return b.String()
}

// bashSubcmds renders the for-loop dispatch arms (all_subcommands in bash.rs).
// Each arm is joined with "\n            " (the cases.join separator); the
// leading separator is emitted before the first arm.
func bashSubcmds() string {
	var b strings.Builder
	for _, e := range bashSubcmdMap {
		b.WriteString("\n            ")
		b.WriteString(e.Parent + "," + e.Name + ")\n")
		b.WriteString("                cmd=\"" + e.Fn + "\"\n")
		b.WriteString("                ;;")
	}
	return b.String()
}

// bashSubcmdArm renders one subcmd_details arm (subcommand_details in bash.rs).
func bashSubcmdArm(node completionNode) string {
	var b strings.Builder
	b.WriteString("\n        ")
	b.WriteString(node.Fn + ")\n")
	b.WriteString("            opts=\"" + node.Opts + "\"\n")
	b.WriteString("            if [[ ${cur} == -* || ${COMP_CWORD} -eq ")
	b.WriteString(itoa(node.Level))
	b.WriteString(" ]] ; then\n")
	b.WriteString("                COMPREPLY=( $(compgen -W \"${opts}\" -- \"${cur}\") )\n")
	b.WriteString("                return 0\n")
	b.WriteString("            fi\n")
	b.WriteString("            case \"${prev}\" in")
	b.WriteString(bashOptionDetails(node.Flags))
	b.WriteString("\n")
	b.WriteString("                *)\n")
	b.WriteString("                    COMPREPLY=()\n")
	b.WriteString("                    ;;\n")
	b.WriteString("            esac\n")
	b.WriteString("            COMPREPLY=( $(compgen -W \"${opts}\" -- \"${cur}\") )\n")
	b.WriteString("            return 0\n")
	b.WriteString("            ;;")
	return b.String()
}

// bashOptionDetails renders the per-flag `case "${prev}"` arms
// (option_details_for_path in bash.rs). Arms are joined with "\n                ".
func bashOptionDetails(flags []bashFlagDetail) string {
	var b strings.Builder
	for _, f := range flags {
		b.WriteString("\n                ")
		b.WriteString(f.Flag + ")\n")
		switch f.Kind {
		case "vals":
			b.WriteString("                    COMPREPLY=($(compgen -W \"" + f.Vals + "\" -- \"${cur}\"))\n")
			b.WriteString("                    return 0\n")
			b.WriteString("                    ;;")
		case "dir":
			b.WriteString("                    COMPREPLY=()\n")
			b.WriteString("                    if [[ \"${BASH_VERSINFO[0]}\" -ge 4 ]]; then\n")
			b.WriteString("                        compopt -o plusdirs\n")
			b.WriteString("                    fi\n")
			b.WriteString("                    return 0\n")
			b.WriteString("                    ;;")
		default: // "file"
			b.WriteString("                    COMPREPLY=($(compgen -f \"${cur}\"))\n")
			b.WriteString("                    return 0\n")
			b.WriteString("                    ;;")
		}
	}
	return b.String()
}

// itoa renders a small non-negative int without importing strconv at call sites
// (the levels are always single/low digits).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
