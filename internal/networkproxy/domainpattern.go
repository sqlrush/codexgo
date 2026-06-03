package networkproxy

import (
	"net/url"
	"strings"
)

// domainPatternKind classifies how a pattern matches.
type domainPatternKind int

const (
	domainExact domainPatternKind = iota
	domainSubdomainsOnly
	domainApexAndSubdomains
)

// domainPattern is the structured form of a domain pattern used for constraint
// comparisons (which manage how user policy may widen/narrow a managed baseline).
type domainPattern struct {
	kind   domainPatternKind
	domain string
}

// parseDomainPattern decodes wildcard prefixes without validating glob syntax,
// mirroring Rust's `DomainPattern::parse`.
func parseDomainPattern(input string) domainPattern {
	input = strings.TrimSpace(input)
	if input == "" {
		return domainPattern{kind: domainExact, domain: ""}
	}
	if rest, ok := strings.CutPrefix(input, "**."); ok {
		return parseDomainKind(rest, domainApexAndSubdomains)
	}
	if rest, ok := strings.CutPrefix(input, "*."); ok {
		return parseDomainKind(rest, domainSubdomainsOnly)
	}
	return domainPattern{kind: domainExact, domain: input}
}

func parseDomainKind(domain string, kind domainPatternKind) domainPattern {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return domainPattern{kind: domainExact, domain: ""}
	}
	return domainPattern{kind: kind, domain: domain}
}

// parseDomainPatternForConstraints validates the domain parts via URL host
// parsing, mirroring Rust's `DomainPattern::parse_for_constraints`.
func parseDomainPatternForConstraints(input string) domainPattern {
	input = strings.TrimSpace(input)
	if input == "" {
		return domainPattern{kind: domainExact, domain: ""}
	}
	if rest, ok := strings.CutPrefix(input, "**."); ok {
		return domainPattern{kind: domainApexAndSubdomains, domain: parseDomainForConstraints(rest)}
	}
	if rest, ok := strings.CutPrefix(input, "*."); ok {
		return domainPattern{kind: domainSubdomainsOnly, domain: parseDomainForConstraints(rest)}
	}
	return domainPattern{kind: domainExact, domain: parseDomainForConstraints(input)}
}

func parseDomainForConstraints(domain string) string {
	domain = strings.TrimRight(strings.TrimSpace(domain), ".")
	if domain == "" {
		return ""
	}
	host := domain
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	if strings.ContainsAny(host, "*?%") {
		return domain
	}
	// Validate as a URL host. url.Parse with a scheme gives us host validation.
	u, err := url.Parse("//" + host)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// allows reports whether this (managed baseline) pattern permits the candidate
// (user) pattern, mirroring Rust's `DomainPattern::allows`.
func (p domainPattern) allows(candidate domainPattern) bool {
	switch p.kind {
	case domainExact:
		return candidate.kind == domainExact && domainEq(candidate.domain, p.domain)
	case domainSubdomainsOnly:
		switch candidate.kind {
		case domainExact:
			return isStrictSubdomain(candidate.domain, p.domain)
		case domainSubdomainsOnly:
			return isSubdomainOrEqual(candidate.domain, p.domain)
		case domainApexAndSubdomains:
			return isStrictSubdomain(candidate.domain, p.domain)
		}
	case domainApexAndSubdomains:
		return isSubdomainOrEqual(candidate.domain, p.domain)
	}
	return false
}

func normalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimRight(domain, "."))
}

func domainEq(left, right string) bool {
	return normalizeDomain(left) == normalizeDomain(right)
}

func isSubdomainOrEqual(child, parent string) bool {
	child = normalizeDomain(child)
	parent = normalizeDomain(parent)
	if child == parent {
		return true
	}
	return strings.HasSuffix(child, "."+parent)
}

func isStrictSubdomain(child, parent string) bool {
	child = normalizeDomain(child)
	parent = normalizeDomain(parent)
	return child != parent && strings.HasSuffix(child, "."+parent)
}
