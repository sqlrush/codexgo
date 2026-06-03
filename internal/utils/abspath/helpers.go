package abspath

// rootPath returns the platform root used as the base for
// from_absolute_path_checked. The Rust source passes Path::new("/"); on Windows
// a bare "\\" is a drive-relative root, but the input is already guaranteed
// absolute there, so the base is never actually consulted for prefix data.
func rootPath() string {
	if isWindows() {
		return `\`
	}
	return "/"
}

// parentOf returns the parent of a normalized absolute path, mirroring
// Path::parent. It returns ok=false when path is a root (a prefix and/or a lone
// root separator with no normal components).
//
// The input is assumed already normalized (as produced by this package), so a
// simple component walk is sufficient and never mutates the input.
func parentOf(path string) (string, bool) {
	comps := parseComponents(path)
	if len(comps) == 0 {
		return "", false
	}
	// Find the last normal component; dropping it yields the parent.
	lastNormal := -1
	for i := len(comps) - 1; i >= 0; i-- {
		if comps[i].kind == compNormal {
			lastNormal = i
			break
		}
	}
	if lastNormal == -1 {
		// Only prefix and/or root remain: this is a root, which has no parent.
		return "", false
	}
	parent := comps[:lastNormal]
	if len(parent) == 0 {
		return "", false
	}
	return renderComponents(parent), true
}
