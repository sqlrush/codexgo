package sandbox

import (
	"fmt"
	"os/exec"
	"strings"
)

// removeEnv returns a copy of env (a "KEY=VALUE" slice) with every entry for key
// removed. The input is not mutated.
func removeEnv(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// setEnv returns a copy of env with key set to value, replacing any existing
// entry for key. The input is not mutated.
func setEnv(env []string, key, value string) []string {
	out := removeEnv(env, key)
	return append(out, key+"="+value)
}

// cloneEnv returns a shallow copy of env so backends never mutate the caller's
// map (immutability), returning a fresh map even for nil input.
func cloneEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env)+1)
	for k, v := range env {
		out[k] = v
	}
	return out
}

// lookPathSandbox resolves program to an absolute executable path. Absolute and
// path-bearing inputs are returned as-is (after a presence check is left to the
// kernel at exec time); a bare name is resolved against PATH.
func lookPathSandbox(program string) (string, error) {
	if program == "" {
		return "", fmt.Errorf("empty program")
	}
	if strings.ContainsRune(program, '/') || strings.ContainsRune(program, '\\') {
		return program, nil
	}
	resolved, err := exec.LookPath(program)
	if err != nil {
		return "", fmt.Errorf("resolve %q on PATH: %w", program, err)
	}
	return resolved, nil
}
