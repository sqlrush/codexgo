package filewatcher

// recursiveMode is the effective OS watch mode for a path. It mirrors
// notify::RecursiveMode but, because fsnotify's public API only supports
// non-recursive watches, recursiveModeRecursive is honored for event-matching
// semantics and ref-counting; the OS watch itself is always added
// non-recursively. See package docs for this deviation.
type recursiveMode int

const (
	// modeNone means the path should not be watched.
	modeNone recursiveMode = iota
	// modeNonRecursive watches only the path's direct children.
	modeNonRecursive
	// modeRecursive watches the path's entire subtree (semantically).
	modeRecursive
)

// pathWatchCounts reference-counts recursive and non-recursive registrations for
// a single OS watch path. It mirrors codex's PathWatchCounts.
type pathWatchCounts struct {
	nonRecursive int
	recursive    int
}

// increment adds amount registrations of the given recursiveness.
func (c pathWatchCounts) increment(recursive bool, amount int) pathWatchCounts {
	if recursive {
		c.recursive += amount
	} else {
		c.nonRecursive += amount
	}
	return c
}

// decrement removes amount registrations of the given recursiveness, saturating
// at zero so spurious unregisters never underflow.
func (c pathWatchCounts) decrement(recursive bool, amount int) pathWatchCounts {
	if recursive {
		c.recursive = saturatingSub(c.recursive, amount)
	} else {
		c.nonRecursive = saturatingSub(c.nonRecursive, amount)
	}
	return c
}

// effectiveMode returns the OS watch mode implied by the current counts.
// Recursive registrations dominate non-recursive ones, matching codex.
func (c pathWatchCounts) effectiveMode() recursiveMode {
	switch {
	case c.recursive > 0:
		return modeRecursive
	case c.nonRecursive > 0:
		return modeNonRecursive
	default:
		return modeNone
	}
}

// isEmpty reports whether no registrations remain.
func (c pathWatchCounts) isEmpty() bool {
	return c.nonRecursive == 0 && c.recursive == 0
}

// saturatingSub returns a-b clamped at zero.
func saturatingSub(a, b int) int {
	if b >= a {
		return 0
	}
	return a - b
}
