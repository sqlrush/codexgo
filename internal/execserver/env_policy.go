package execserver

import (
	"os"
	"runtime"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// codexThreadIDEnvVar is the environment variable populated with the thread id.
//
// Rust: `shell_environment::CODEX_THREAD_ID_ENV_VAR`.
const codexThreadIDEnvVar = "CODEX_THREAD_ID"

// unixCoreEnvVars is the core inherit set on non-Windows platforms.
//
// Rust: `shell_environment::UNIX_CORE_ENV_VARS`.
var unixCoreEnvVars = []string{
	"PATH", "SHELL", "TMPDIR", "TEMP", "TMP", "HOME", "LANG", "LC_ALL", "LC_CTYPE", "LOGNAME", "USER",
}

// windowsCoreEnvVars is the core inherit set on Windows.
//
// Rust: `shell_environment::WINDOWS_CORE_ENV_VARS`.
var windowsCoreEnvVars = []string{
	"PATH", "PATHEXT", "SHELL", "COMSPEC", "SYSTEMROOT", "SYSTEMDRIVE",
	"USERNAME", "USERDOMAIN", "USERPROFILE", "HOMEDRIVE", "HOMEPATH",
	"PROGRAMFILES", "PROGRAMFILES(X86)", "PROGRAMW6432", "PROGRAMDATA",
	"LOCALAPPDATA", "APPDATA", "TEMP", "TMP", "TMPDIR", "POWERSHELL", "PWSH",
}

// envPattern is a case-insensitive glob pattern over environment variable names,
// matching the `wildmatch` crate semantics used by
// `EnvironmentVariablePattern::new_case_insensitive`: `*` matches any sequence
// (including empty), `?` matches exactly one character, and matching is
// case-insensitive (both pattern and candidate are lowercased first).
type envPattern struct {
	pattern string
}

// newCaseInsensitivePattern lowercases the pattern, matching
// `WildMatchPattern::new_case_insensitive`.
func newCaseInsensitivePattern(pattern string) envPattern {
	return envPattern{pattern: strings.ToLower(pattern)}
}

// matches reports whether candidate matches the pattern, case-insensitively.
func (p envPattern) matches(candidate string) bool {
	return wildMatch(p.pattern, strings.ToLower(candidate))
}

// wildMatch implements wildmatch glob matching for the lowercased inputs. `*`
// matches any run of characters; `?` matches exactly one character. It uses an
// iterative backtracking algorithm equivalent to the wildmatch crate.
func wildMatch(pattern, text string) bool {
	pr := []rune(pattern)
	tr := []rune(text)
	pi, ti := 0, 0
	starP, starT := -1, -1

	for ti < len(tr) {
		switch {
		case pi < len(pr) && (pr[pi] == '?' || pr[pi] == tr[ti]):
			pi++
			ti++
		case pi < len(pr) && pr[pi] == '*':
			starP = pi
			starT = ti
			pi++
		case starP != -1:
			pi = starP + 1
			starT++
			ti = starT
		default:
			return false
		}
	}

	for pi < len(pr) && pr[pi] == '*' {
		pi++
	}
	return pi == len(pr)
}

// childEnv builds the child environment for the given params.
//
// Rust: `child_env`. With no policy the explicit env is used verbatim. With a
// policy, the inherited environment is filtered by the policy and the explicit
// env is overlaid on top (overlay wins).
func childEnv(params ExecParams, inherited map[string]string) map[string]string {
	if params.EnvPolicy == nil {
		return copyEnv(params.Env)
	}
	env := populateEnv(inherited, *params.EnvPolicy, nil)
	for k, v := range params.Env {
		env[k] = v
	}
	return env
}

// copyEnv returns a defensive copy of an environment map.
func copyEnv(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// populateEnv applies the shell-environment policy to the supplied vars.
//
// Rust: `shell_environment::populate_env`. The steps are: select the starting
// set by inherit strategy; apply default excludes (*KEY*, *SECRET*, *TOKEN*)
// unless disabled; apply custom excludes; apply overrides; apply include_only;
// and populate the thread id when provided.
func populateEnv(vars map[string]string, policy ExecEnvPolicy, threadID *string) map[string]string {
	env := make(map[string]string)
	switch policy.Inherit {
	case protocol.ShellEnvironmentPolicyInheritAll:
		for k, v := range vars {
			env[k] = v
		}
	case protocol.ShellEnvironmentPolicyInheritNone:
		// Start empty.
	case protocol.ShellEnvironmentPolicyInheritCore:
		core := unixCoreEnvVars
		if runtime.GOOS == "windows" {
			core = windowsCoreEnvVars
		}
		for k, v := range vars {
			if anyEqualFold(core, k) {
				env[k] = v
			}
		}
	default:
		// Match the Rust serde default (All) for any unrecognized value.
		for k, v := range vars {
			env[k] = v
		}
	}

	if !policy.IgnoreDefaultExcludes {
		defaultExcludes := []envPattern{
			newCaseInsensitivePattern("*KEY*"),
			newCaseInsensitivePattern("*SECRET*"),
			newCaseInsensitivePattern("*TOKEN*"),
		}
		retainNotMatching(env, defaultExcludes)
	}

	if len(policy.Exclude) > 0 {
		retainNotMatching(env, compilePatterns(policy.Exclude))
	}

	for k, v := range policy.Set {
		env[k] = v
	}

	if len(policy.IncludeOnly) > 0 {
		includeOnly := compilePatterns(policy.IncludeOnly)
		for k := range env {
			if !matchesAny(k, includeOnly) {
				delete(env, k)
			}
		}
	}

	if threadID != nil {
		env[codexThreadIDEnvVar] = *threadID
	}

	return env
}

// compilePatterns converts case-insensitive glob strings into patterns.
func compilePatterns(patterns []string) []envPattern {
	out := make([]envPattern, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, newCaseInsensitivePattern(p))
	}
	return out
}

// retainNotMatching deletes every key in env that matches any pattern.
func retainNotMatching(env map[string]string, patterns []envPattern) {
	for k := range env {
		if matchesAny(k, patterns) {
			delete(env, k)
		}
	}
}

// matchesAny reports whether name matches any of the patterns.
func matchesAny(name string, patterns []envPattern) bool {
	for _, p := range patterns {
		if p.matches(name) {
			return true
		}
	}
	return false
}

// anyEqualFold reports whether candidate equals any value, ignoring ASCII case.
func anyEqualFold(values []string, candidate string) bool {
	for _, v := range values {
		if strings.EqualFold(v, candidate) {
			return true
		}
	}
	return false
}

// currentEnv snapshots the current process environment as a map. It mirrors the
// Rust use of `std::env::vars()` as the base for policy filtering.
func currentEnv() map[string]string {
	pairs := os.Environ()
	env := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		if i := strings.IndexByte(pair, '='); i >= 0 {
			env[pair[:i]] = pair[i+1:]
		}
	}
	return env
}
