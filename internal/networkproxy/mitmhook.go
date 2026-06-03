package networkproxy

import (
	"fmt"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

const (
	patternPrefix = "pattern:"
	literalPrefix = "literal:"
)

// MitmHookConfig configures a man-in-the-middle hook for HTTPS requests after
// CONNECT termination. The configuration shape mirrors codex exactly so config
// files are drop-in compatible.
type MitmHookConfig struct {
	Host    string
	Matcher MitmHookMatchConfig
	Actions MitmHookActionsConfig
}

// MitmHookMatchConfig describes when a hook applies.
type MitmHookMatchConfig struct {
	Methods      []string
	PathPrefixes []string
	Query        map[string][]string
	Headers      map[string][]string
	// Body is reserved for a future release and must be nil (its presence is a
	// validation error, matching codex).
	Body *MitmHookBodyConfig
}

// MitmHookActionsConfig describes the header mutations a matching hook performs.
type MitmHookActionsConfig struct {
	StripRequestHeaders  []string
	InjectRequestHeaders []InjectedHeaderConfig
}

// InjectedHeaderConfig describes a header injected from a secret source.
type InjectedHeaderConfig struct {
	Name         string
	SecretEnvVar *string
	SecretFile   *string
	Prefix       *string
}

// MitmHookBodyConfig is a reserved body-match config (currently unsupported).
type MitmHookBodyConfig struct {
	Raw any
}

// compiled hook representations.

type mitmHook struct {
	host    string
	matcher mitmHookMatcher
	actions mitmHookActions
}

type mitmHookMatcher struct {
	methods      []string
	pathPrefixes []pathMatcher
	query        []queryConstraint
	headers      []headerConstraint
}

type queryConstraint struct {
	name          string
	allowedValues []valueMatcher
}

type headerConstraint struct {
	name          string
	allowedValues []valueMatcher
}

type mitmHookActions struct {
	stripRequestHeaders  []string
	injectRequestHeaders []resolvedInjectedHeader
}

type secretSourceKind int

const (
	secretSourceEnvVar secretSourceKind = iota
	secretSourceFile
)

type resolvedInjectedHeader struct {
	name       string
	value      string
	sourceKind secretSourceKind
	source     string
}

type pathMatcherKind int

const (
	pathMatcherPrefix pathMatcherKind = iota
	pathMatcherGlob
)

type pathMatcher struct {
	kind    pathMatcherKind
	literal string
	glob    string
}

func (m pathMatcher) matches(candidate string) bool {
	switch m.kind {
	case pathMatcherPrefix:
		return strings.HasPrefix(candidate, m.literal)
	case pathMatcherGlob:
		return globMatch(m.glob, candidate, true)
	default:
		return false
	}
}

type valueMatcherKind int

const (
	valueMatcherExact valueMatcherKind = iota
	valueMatcherGlob
)

type valueMatcher struct {
	kind  valueMatcherKind
	exact string
	glob  string
}

func (m valueMatcher) matches(candidate string) bool {
	switch m.kind {
	case valueMatcherExact:
		return m.exact == candidate
	case valueMatcherGlob:
		return globMatch(m.glob, candidate, false)
	default:
		return false
	}
}

type mitmHooksByHost map[string][]mitmHook

// hookEvaluationKind is the outcome of evaluating hooks for a host.
type hookEvaluationKind int

const (
	hookNoHooksForHost hookEvaluationKind = iota
	hookMatched
	hookedHostNoMatch
)

type hookEvaluation struct {
	kind    hookEvaluationKind
	actions mitmHookActions
}

func validateMitmHookConfig(cfg NetworkProxyConfig) error {
	hooks := cfg.Network.MitmHooks
	if len(hooks) == 0 {
		return nil
	}
	if !cfg.Network.Mitm {
		return fmt.Errorf("network.mitm_hooks requires network.mitm = true")
	}
	for i, hook := range hooks {
		host, err := normalizeHookHost(hook.Host)
		if err != nil {
			return fmt.Errorf("invalid network.mitm_hooks[%d].host: %w", i, err)
		}
		methods, err := normalizeMethods(hook.Matcher.Methods)
		if err != nil {
			return fmt.Errorf("invalid network.mitm_hooks[%d].match.methods: %w", i, err)
		}
		if len(methods) == 0 {
			return fmt.Errorf("network.mitm_hooks[%d].match.methods must not be empty", i)
		}
		paths, err := compilePathMatchers(hook.Matcher.PathPrefixes)
		if err != nil {
			return fmt.Errorf("invalid network.mitm_hooks[%d].match.path_prefixes: %w", i, err)
		}
		if len(paths) == 0 {
			return fmt.Errorf("network.mitm_hooks[%d].match.path_prefixes must not be empty", i)
		}
		if hook.Matcher.Body != nil {
			return fmt.Errorf("network.mitm_hooks[%d].match.body is reserved for a future release and is not yet supported", i)
		}
		if err := validateQueryConstraints(hook.Matcher.Query); err != nil {
			return fmt.Errorf("invalid network.mitm_hooks[%d].match.query: %w", i, err)
		}
		if err := validateHeaderConstraints(hook.Matcher.Headers); err != nil {
			return fmt.Errorf("invalid network.mitm_hooks[%d].match.headers: %w", i, err)
		}
		if err := validateStripRequestHeaders(hook.Actions.StripRequestHeaders); err != nil {
			return fmt.Errorf("invalid network.mitm_hooks[%d].actions.strip_request_headers: %w", i, err)
		}
		if err := validateInjectedHeaders(hook.Actions.InjectRequestHeaders); err != nil {
			return fmt.Errorf("invalid network.mitm_hooks[%d].actions.inject_request_headers: %w", i, err)
		}
		if host == "" {
			return fmt.Errorf("network.mitm_hooks[%d].host must not be empty", i)
		}
	}
	return nil
}

type envResolver func(name string) (string, bool)
type fileReader func(path abspath.AbsolutePathBuf) (string, error)

func compileMitmHooks(cfg NetworkProxyConfig) (mitmHooksByHost, error) {
	return compileMitmHooksWithResolvers(cfg,
		func(name string) (string, bool) { return os.LookupEnv(name) },
		func(path abspath.AbsolutePathBuf) (string, error) {
			data, err := os.ReadFile(path.Path())
			if err != nil {
				return "", fmt.Errorf("failed to read secret file %s: %w", path.Path(), err)
			}
			return strings.TrimSpace(string(data)), nil
		},
	)
}

func compileMitmHooksWithResolvers(cfg NetworkProxyConfig, resolveEnv envResolver, readFile fileReader) (mitmHooksByHost, error) {
	if err := validateMitmHookConfig(cfg); err != nil {
		return nil, err
	}
	result := make(mitmHooksByHost)
	for _, hook := range cfg.Network.MitmHooks {
		host, err := normalizeHookHost(hook.Host)
		if err != nil {
			return nil, err
		}
		methods, err := normalizeMethods(hook.Matcher.Methods)
		if err != nil {
			return nil, err
		}
		paths, err := compilePathMatchers(hook.Matcher.PathPrefixes)
		if err != nil {
			return nil, err
		}
		query, err := compileQueryConstraints(hook.Matcher.Query)
		if err != nil {
			return nil, err
		}
		headers, err := compileHeaderConstraints(hook.Matcher.Headers)
		if err != nil {
			return nil, err
		}
		strip, err := compileStripHeaders(hook.Actions.StripRequestHeaders)
		if err != nil {
			return nil, err
		}
		inject := make([]resolvedInjectedHeader, 0, len(hook.Actions.InjectRequestHeaders))
		for _, header := range hook.Actions.InjectRequestHeaders {
			resolved, err := compileInjectedHeader(header, resolveEnv, readFile)
			if err != nil {
				return nil, fmt.Errorf("failed to compile injected header %s: %w", header.Name, err)
			}
			inject = append(inject, resolved)
		}
		result[host] = append(result[host], mitmHook{
			host:    host,
			matcher: mitmHookMatcher{methods: methods, pathPrefixes: paths, query: query, headers: headers},
			actions: mitmHookActions{stripRequestHeaders: strip, injectRequestHeaders: inject},
		})
	}
	return result, nil
}

func compileInjectedHeader(header InjectedHeaderConfig, resolveEnv envResolver, readFile fileReader) (resolvedInjectedHeader, error) {
	name, err := parseHeaderName(header.Name)
	if err != nil {
		return resolvedInjectedHeader{}, err
	}
	var (
		secret     string
		sourceKind secretSourceKind
		source     string
	)
	switch {
	case header.SecretEnvVar != nil && header.SecretFile == nil:
		value, ok := resolveEnv(*header.SecretEnvVar)
		if !ok {
			return resolvedInjectedHeader{}, fmt.Errorf("missing required environment variable %s", *header.SecretEnvVar)
		}
		secret, sourceKind, source = value, secretSourceEnvVar, *header.SecretEnvVar
	case header.SecretEnvVar == nil && header.SecretFile != nil:
		path, err := parseSecretFile(*header.SecretFile)
		if err != nil {
			return resolvedInjectedHeader{}, err
		}
		value, err := readFile(path)
		if err != nil {
			return resolvedInjectedHeader{}, err
		}
		secret, sourceKind, source = value, secretSourceFile, path.Path()
	default:
		return resolvedInjectedHeader{}, fmt.Errorf("expected exactly one of secret_env_var or secret_file")
	}
	prefix := ""
	if header.Prefix != nil {
		prefix = *header.Prefix
	}
	value := prefix + secret
	if !isValidHeaderValue(value) {
		return resolvedInjectedHeader{}, fmt.Errorf("invalid value for injected header %s", header.Name)
	}
	return resolvedInjectedHeader{name: name, value: value, sourceKind: sourceKind, source: source}, nil
}

// evaluateMitmHooks evaluates hooks for a host against an HTTP request.
func evaluateMitmHooks(hooksByHost mitmHooksByHost, host string, req *http.Request) hookEvaluation {
	normalizedHost := NormalizeHost(host)
	hooks, ok := hooksByHost[normalizedHost]
	if !ok {
		return hookEvaluation{kind: hookNoHooksForHost}
	}
	for _, hook := range hooks {
		if hookMatches(hook, req) {
			return hookEvaluation{kind: hookMatched, actions: hook.actions}
		}
	}
	return hookEvaluation{kind: hookedHostNoMatch}
}

func hookMatches(hook mitmHook, req *http.Request) bool {
	method := strings.ToUpper(req.Method)
	methodOK := false
	for _, allowed := range hook.matcher.methods {
		if allowed == method {
			methodOK = true
			break
		}
	}
	if !methodOK {
		return false
	}
	if !pathMatches(hook.matcher.pathPrefixes, req.URL.Path) {
		return false
	}
	if !queryMatches(hook.matcher.query, req) {
		return false
	}
	return headersMatch(hook.matcher.headers, req)
}

func pathMatches(matchers []pathMatcher, path string) bool {
	for _, m := range matchers {
		if m.matches(path) {
			return true
		}
	}
	return false
}

func queryMatches(constraints []queryConstraint, req *http.Request) bool {
	if len(constraints) == 0 {
		return true
	}
	values, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		values = url.Values{}
	}
	for _, constraint := range constraints {
		actual, ok := values[constraint.name]
		if !ok {
			return false
		}
		matched := false
		for _, candidate := range actual {
			for _, allowed := range constraint.allowedValues {
				if allowed.matches(candidate) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func headersMatch(constraints []headerConstraint, req *http.Request) bool {
	for _, constraint := range constraints {
		actual := req.Header.Values(constraint.name)
		if len(actual) == 0 {
			return false
		}
		if len(constraint.allowedValues) == 0 {
			continue
		}
		matched := false
		for _, value := range actual {
			for _, allowed := range constraint.allowedValues {
				if allowed.matches(value) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

type matcherPatternKind int

const (
	matcherLiteral matcherPatternKind = iota
	matcherGlob
)

func parseMatcherPattern(pattern string) (matcherPatternKind, string, error) {
	if literal, ok := strings.CutPrefix(pattern, literalPrefix); ok {
		return matcherLiteral, literal, nil
	}
	glob, ok := strings.CutPrefix(pattern, patternPrefix)
	if !ok {
		return matcherLiteral, pattern, nil
	}
	if glob == "" {
		return matcherLiteral, "", fmt.Errorf("glob pattern must not be empty")
	}
	return matcherGlob, glob, nil
}

func compilePathMatchers(prefixes []string) ([]pathMatcher, error) {
	out := make([]pathMatcher, 0, len(prefixes))
	for _, prefix := range prefixes {
		kind, value, err := parseMatcherPattern(prefix)
		if err != nil {
			return nil, err
		}
		switch kind {
		case matcherLiteral:
			if value == "" {
				return nil, fmt.Errorf("path_prefixes must not contain empty entries")
			}
			out = append(out, pathMatcher{kind: pathMatcherPrefix, literal: value})
		case matcherGlob:
			if err := validateGlob(value); err != nil {
				return nil, err
			}
			out = append(out, pathMatcher{kind: pathMatcherGlob, glob: value})
		}
	}
	return out, nil
}

func compileValueMatchers(values []string) ([]valueMatcher, error) {
	out := make([]valueMatcher, 0, len(values))
	for _, value := range values {
		kind, parsed, err := parseMatcherPattern(value)
		if err != nil {
			return nil, err
		}
		switch kind {
		case matcherLiteral:
			out = append(out, valueMatcher{kind: valueMatcherExact, exact: parsed})
		case matcherGlob:
			if err := validateGlob(parsed); err != nil {
				return nil, err
			}
			out = append(out, valueMatcher{kind: valueMatcherGlob, glob: parsed})
		}
	}
	return out, nil
}

func compileQueryConstraints(query map[string][]string) ([]queryConstraint, error) {
	out := make([]queryConstraint, 0, len(query))
	for _, name := range sortedKeys(query) {
		normalized, err := normalizeQueryName(name)
		if err != nil {
			return nil, err
		}
		matchers, err := compileValueMatchers(query[name])
		if err != nil {
			return nil, err
		}
		out = append(out, queryConstraint{name: normalized, allowedValues: matchers})
	}
	return out, nil
}

func compileHeaderConstraints(headers map[string][]string) ([]headerConstraint, error) {
	out := make([]headerConstraint, 0, len(headers))
	for _, name := range sortedKeys(headers) {
		parsedName, err := parseHeaderName(name)
		if err != nil {
			return nil, err
		}
		matchers, err := compileValueMatchers(headers[name])
		if err != nil {
			return nil, err
		}
		out = append(out, headerConstraint{name: parsedName, allowedValues: matchers})
	}
	return out, nil
}

func compileStripHeaders(names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, name := range names {
		parsed, err := parseHeaderName(name)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

func validateQueryConstraints(query map[string][]string) error {
	for name, values := range query {
		normalized, err := normalizeQueryName(name)
		if err != nil {
			return err
		}
		if normalized == "" {
			return fmt.Errorf("query keys must not be empty")
		}
		if len(values) == 0 {
			return fmt.Errorf("query key %q must list at least one allowed value", name)
		}
		if _, err := compileValueMatchers(values); err != nil {
			return fmt.Errorf("invalid matcher for query key %q: %w", name, err)
		}
	}
	return nil
}

func validateHeaderConstraints(headers map[string][]string) error {
	for name, values := range headers {
		if _, err := parseHeaderName(name); err != nil {
			return err
		}
		if _, err := compileValueMatchers(values); err != nil {
			return fmt.Errorf("invalid matcher for header %q: %w", name, err)
		}
	}
	return nil
}

func validateStripRequestHeaders(names []string) error {
	for _, name := range names {
		if _, err := parseHeaderName(name); err != nil {
			return err
		}
	}
	return nil
}

func validateInjectedHeaders(headers []InjectedHeaderConfig) error {
	for _, header := range headers {
		if _, err := parseHeaderName(header.Name); err != nil {
			return err
		}
		switch {
		case header.SecretEnvVar != nil && header.SecretFile == nil:
			if strings.TrimSpace(*header.SecretEnvVar) == "" {
				return fmt.Errorf("secret_env_var must not be empty")
			}
		case header.SecretEnvVar == nil && header.SecretFile != nil:
			if _, err := parseSecretFile(*header.SecretFile); err != nil {
				return err
			}
		default:
			return fmt.Errorf("expected exactly one of secret_env_var or secret_file")
		}
	}
	return nil
}

func normalizeHookHost(host string) (string, error) {
	normalized := NormalizeHost(host)
	if normalized == "" {
		return "", fmt.Errorf("host must not be empty")
	}
	if strings.Contains(normalized, "*") {
		return "", fmt.Errorf("MITM hook hosts must be exact hosts and cannot contain wildcards")
	}
	return normalized, nil
}

func normalizeMethods(methods []string) ([]string, error) {
	out := make([]string, 0, len(methods))
	for _, method := range methods {
		normalized := strings.ToUpper(strings.TrimSpace(method))
		if normalized == "" {
			return nil, fmt.Errorf("methods must not contain empty entries")
		}
		out = append(out, normalized)
	}
	return out, nil
}

func normalizeQueryName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("query keys must not be empty")
	}
	return name, nil
}

// parseHeaderName validates a header name per RFC 7230 token rules and returns
// the canonicalized form used by net/http.Header.
func parseHeaderName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("invalid header name %q: empty", name)
	}
	for i := 0; i < len(name); i++ {
		if !isTokenChar(name[i]) {
			return "", fmt.Errorf("invalid header name %q", name)
		}
	}
	return textproto.CanonicalMIMEHeaderKey(name), nil
}

func isTokenChar(c byte) bool {
	// RFC 7230 token characters.
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}

func isValidHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		// Disallow control characters except HTAB; matches http header value rules.
		if c == '\t' {
			continue
		}
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

func parseSecretFile(path string) (abspath.AbsolutePathBuf, error) {
	if strings.TrimSpace(path) == "" {
		return abspath.AbsolutePathBuf{}, fmt.Errorf("secret_file must not be empty")
	}
	if !filepath.IsAbs(path) {
		return abspath.AbsolutePathBuf{}, fmt.Errorf("secret_file must be an absolute path: %q", path)
	}
	abs, err := abspath.FromAbsolutePath(path)
	if err != nil {
		return abspath.AbsolutePathBuf{}, fmt.Errorf("secret_file must be an absolute path: %q: %w", path, err)
	}
	return abs, nil
}

func validateGlob(pattern string) error {
	// Validate bracket balance for character classes; other syntax is permissive.
	depth := 0
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case '\\':
			i++
		}
	}
	if depth != 0 {
		return fmt.Errorf("invalid glob pattern %q: unbalanced character class", pattern)
	}
	return nil
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
