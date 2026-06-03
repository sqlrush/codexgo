package shellcmd

import (
	"net/url"
	"regexp"
	"strings"
)

// urlTrimRE strips common PowerShell punctuation surrounding an inline URL:
// leading quotes/parens/whitespace and trailing semicolons/closing parens. It
// mirrors the regex used by looks_like_url in windows_dangerous_commands.rs.
var urlTrimRE = regexp.MustCompile(`^[ "'(\s]*([^\s"');]+)[\s;)]*$`)

// LooksLikeURL reports whether a single command-line token denotes an http or
// https URL. It first locates an embedded "https://"/"http://" substring (so a
// token like Start-Process('https://x') is recognized), then trims surrounding
// shell punctuation, and finally requires the candidate to parse as a URL with
// an http or https scheme.
//
// Mirrors looks_like_url in windows_dangerous_commands.rs.
func LooksLikeURL(token string) bool {
	urlish := token
	if idx := strings.Index(token, "https://"); idx >= 0 {
		urlish = token[idx:]
	} else if idx := strings.Index(token, "http://"); idx >= 0 {
		urlish = token[idx:]
	}

	candidate := urlish
	if m := urlTrimRE.FindStringSubmatch(urlish); m != nil {
		candidate = m[1]
	}

	parsed, err := url.Parse(candidate)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	return scheme == "http" || scheme == "https"
}

// ArgsHaveURL reports whether any argument in the slice looks like an http or
// https URL. Mirrors args_have_url in windows_dangerous_commands.rs. The input
// slice is not mutated.
func ArgsHaveURL(args []string) bool {
	for _, arg := range args {
		if LooksLikeURL(arg) {
			return true
		}
	}
	return false
}
