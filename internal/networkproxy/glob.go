package networkproxy

import (
	"fmt"
	"strings"
)

// globMatch reports whether candidate matches the glob pattern. It supports the
// subset of glob syntax used by the Rust `globset` crate that this package
// relies on: `*` (any run of characters), `?` (exactly one character), and
// `[...]` character classes (with `[!...]`/`[^...]` negation and `a-z` ranges).
// When literalSeparator is true, `*` and `?` do not match the `/` path
// separator (mirroring globset's literal_separator option used by path/value
// matchers). Domain matching uses literalSeparator=false.
//
// Matching is case-insensitive for domains, so callers lowercase both the
// pattern and candidate before calling.
func globMatch(pattern, candidate string, literalSeparator bool) bool {
	return globMatchBytes([]byte(pattern), []byte(candidate), literalSeparator)
}

func globMatchBytes(pattern, s []byte, literalSeparator bool) bool {
	// Iterative backtracking matcher. starIdx/matchIdx implement `*` backtracking.
	var (
		pIdx, sIdx       int
		starIdx          = -1
		starMatchIdx     int
		starGlobAllowSep bool
	)
	for sIdx < len(s) {
		if pIdx < len(pattern) {
			switch pattern[pIdx] {
			case '?':
				if !literalSeparator || s[sIdx] != '/' {
					pIdx++
					sIdx++
					continue
				}
			case '*':
				// Collapse consecutive stars.
				for pIdx < len(pattern) && pattern[pIdx] == '*' {
					pIdx++
				}
				starIdx = pIdx
				starMatchIdx = sIdx
				starGlobAllowSep = !literalSeparator
				continue
			case '[':
				if matched, next := matchClass(pattern[pIdx:], s[sIdx]); matched {
					if !literalSeparator || s[sIdx] != '/' {
						pIdx += next
						sIdx++
						continue
					}
				} else if next > 0 {
					// Valid class but no match; fall through to backtrack.
					goto backtrack
				}
				// Not a valid class: treat '[' literally.
				if pattern[pIdx] == s[sIdx] {
					pIdx++
					sIdx++
					continue
				}
			default:
				if pattern[pIdx] == s[sIdx] {
					pIdx++
					sIdx++
					continue
				}
			}
		}
	backtrack:
		if starIdx >= 0 {
			// `*` cannot consume a path separator when literalSeparator is set.
			if !starGlobAllowSep && s[starMatchIdx] == '/' {
				return false
			}
			pIdx = starIdx
			starMatchIdx++
			sIdx = starMatchIdx
			continue
		}
		return false
	}
	for pIdx < len(pattern) && pattern[pIdx] == '*' {
		pIdx++
	}
	return pIdx == len(pattern)
}

// matchClass attempts to match a `[...]` character class at the start of pattern
// against ch. It returns whether ch matched and the number of bytes the class
// consumed (0 if pattern does not start with a well-formed class).
func matchClass(pattern []byte, ch byte) (bool, int) {
	if len(pattern) == 0 || pattern[0] != '[' {
		return false, 0
	}
	i := 1
	negate := false
	if i < len(pattern) && (pattern[i] == '!' || pattern[i] == '^') {
		negate = true
		i++
	}
	matched := false
	first := true
	for i < len(pattern) {
		if pattern[i] == ']' && !first {
			consumed := i + 1
			if negate {
				return !matched, consumed
			}
			return matched, consumed
		}
		first = false
		// Range like a-z.
		if i+2 < len(pattern) && pattern[i+1] == '-' && pattern[i+2] != ']' {
			lo, hi := pattern[i], pattern[i+2]
			if ch >= lo && ch <= hi {
				matched = true
			}
			i += 3
			continue
		}
		if pattern[i] == ch {
			matched = true
		}
		i++
	}
	// Unterminated class: not a valid class.
	return false, 0
}

// domainGlobSet is a compiled set of domain glob patterns. Matching is
// case-insensitive.
type domainGlobSet struct {
	patterns []string
}

func (g domainGlobSet) match(host string) bool {
	host = strings.ToLower(host)
	for _, p := range g.patterns {
		if globMatch(p, host, false) {
			return true
		}
	}
	return false
}

type globalWildcardPolicy int

const (
	globalWildcardAllow globalWildcardPolicy = iota
	globalWildcardReject
)

// compileAllowlistGlobset compiles allowlist domain patterns. The global `*`
// wildcard is permitted here so an explicit "*" allowlist works.
func compileAllowlistGlobset(patterns []string) (domainGlobSet, error) {
	return compileGlobsetWithPolicy(patterns, globalWildcardAllow)
}

// compileDenylistGlobset compiles denylist domain patterns. The global `*`
// wildcard is rejected.
func compileDenylistGlobset(patterns []string) (domainGlobSet, error) {
	return compileGlobsetWithPolicy(patterns, globalWildcardReject)
}

func compileGlobsetWithPolicy(patterns []string, policy globalWildcardPolicy) (domainGlobSet, error) {
	var compiled []string
	seen := make(map[string]struct{})
	for _, pattern := range patterns {
		if policy == globalWildcardReject && isGlobalWildcardDomainPattern(pattern) {
			return domainGlobSet{}, fmt.Errorf(
				"unsupported global wildcard domain pattern %q; use exact hosts or scoped wildcards like *.example.com or **.example.com",
				"*")
		}
		normalized := normalizePattern(pattern)
		for _, candidate := range expandDomainPattern(normalized) {
			lc := strings.ToLower(candidate)
			if _, ok := seen[lc]; ok {
				continue
			}
			seen[lc] = struct{}{}
			compiled = append(compiled, lc)
		}
	}
	return domainGlobSet{patterns: compiled}, nil
}

// normalizePattern normalizes a domain pattern, preserving wildcard prefixes and
// canonicalizing the remainder via NormalizeHost.
func normalizePattern(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "*" {
		return "*"
	}
	prefix := ""
	remainder := pattern
	if rest, ok := strings.CutPrefix(pattern, "**."); ok {
		prefix, remainder = "**.", rest
	} else if rest, ok := strings.CutPrefix(pattern, "*."); ok {
		prefix, remainder = "*.", rest
	}
	remainder = NormalizeHost(remainder)
	if prefix == "" {
		return remainder
	}
	return prefix + remainder
}

// expandDomainPattern expands wildcard-prefixed patterns into concrete glob
// patterns, mirroring Rust's `expand_domain_pattern`:
//   - exact "example.com"     -> ["example.com"]
//   - "*.example.com"          -> ["?*.example.com"] (subdomains only)
//   - "**.example.com"         -> ["example.com", "?*.example.com"] (apex + subs)
func expandDomainPattern(pattern string) []string {
	switch dp := parseDomainPattern(pattern); dp.kind {
	case domainExact:
		return []string{dp.domain}
	case domainSubdomainsOnly:
		return []string{"?*." + dp.domain}
	case domainApexAndSubdomains:
		return []string{dp.domain, "?*." + dp.domain}
	default:
		return []string{dp.domain}
	}
}

func isGlobalWildcardDomainPattern(pattern string) bool {
	normalized := normalizePattern(pattern)
	for _, candidate := range expandDomainPattern(normalized) {
		if candidate == "*" {
			return true
		}
	}
	return false
}

func globsetMatchesHostOrUnscoped(set domainGlobSet, host string) bool {
	if set.match(host) {
		return true
	}
	if ip, ok := unscopedIPLiteral(host); ok {
		return set.match(ip)
	}
	return false
}
