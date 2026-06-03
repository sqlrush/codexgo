package abspath

// This file ports `absolutize.rs` (adapted from path-absolutize 3.1.1).
//
// The Rust implementation relies on `std::path::Component`, which already knows
// the host's path grammar. We reimplement the same grammar here so the behavior
// is platform-correct: on Unix a path uses '/'; on Windows a path may carry a
// drive or UNC prefix and either separator.

// component kinds, mirroring the subset of std::path::Component we care about.
type compKind int

const (
	compPrefix  compKind = iota // Windows drive ("C:") or UNC root prefix
	compRootDir                 // the root separator
	compNormal                  // an ordinary path segment
)

// component is a single parsed path element.
type component struct {
	kind compKind
	text string
}

// absolutizeFrom mirrors Rust's `absolutize::absolutize_from`: join path onto
// base (with platform-aware prefix rules) and then lexically normalize.
func absolutizeFrom(path, base string) string {
	return normalizePath(pathWithBase(path, base))
}

// normalizePath mirrors Rust's `normalize_path`: it walks the components,
// dropping "." entries, popping on "..", and keeping prefix/root/normal
// segments. An empty result becomes ".".
func normalizePath(path string) string {
	comps := parseComponents(path)

	var out []component
	for _, c := range comps {
		switch c.kind {
		case compNormal:
			if c.text == "." {
				continue
			}
			if c.text == ".." {
				out = popComponent(out)
				continue
			}
			out = append(out, c)
		case compPrefix, compRootDir:
			out = append(out, c)
		}
	}

	if len(out) == 0 {
		return "."
	}
	return renderComponents(out)
}

// popComponent removes the last non-prefix, non-root component, mirroring
// PathBuf::pop (which refuses to pop the prefix or root). It returns a new slice
// and never mutates the caller's backing array beyond truncation of its own
// copy's length.
func popComponent(comps []component) []component {
	if len(comps) == 0 {
		return comps
	}
	last := comps[len(comps)-1]
	if last.kind == compNormal {
		return comps[:len(comps)-1]
	}
	// Refuse to pop past a root or prefix, matching `PathBuf::pop` behavior used
	// by `parent_dir_above_root_stays_at_root`.
	return comps
}

// pathWithBase mirrors Rust's `path_with_base`, dispatching on platform.
func pathWithBase(path, base string) string {
	if isWindows() {
		return pathWithBaseWindows(path, base)
	}
	return pathWithBaseUnix(path, base)
}

// pathWithBaseUnix mirrors the non-Windows `path_with_base`.
func pathWithBaseUnix(path, base string) string {
	if isAbsolute(path) {
		return path
	}
	return joinUnix(base, path)
}

// pathWithBaseWindows mirrors the Windows `path_with_base`.
func pathWithBaseWindows(path, base string) string {
	if isAbsolute(path) || hasRoot(path) {
		return joinWindows(base, path)
	}

	comps := parseComponents(path)
	if len(comps) == 0 || comps[0].kind != compPrefix {
		return joinWindows(base, path)
	}

	prefix := comps[0]
	rest := comps[1:]

	var out []component
	out = append(out, prefix)

	if len(rest) == 0 {
		out = append(out, component{kind: compRootDir})
		return renderComponents(out)
	}

	baseComps := parseComponents(base)
	skip := 0
	if len(baseComps) > 0 && baseComps[0].kind == compPrefix {
		skip = 1
	}
	out = append(out, baseComps[skip:]...)
	out = append(out, rest...)
	return renderComponents(out)
}
