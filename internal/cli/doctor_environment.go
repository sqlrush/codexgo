package cli

import (
	"fmt"
	"os"
	"runtime"
	"sort"

	"github.com/sqlrush/codexgo/internal/termdetect"
)

// proxyEnvVars are the proxy-related environment variables the network.env check
// reports, mirroring the Rust doctor's PROXY_ENV_VARS list.
var proxyEnvVars = []string{
	"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
	"http_proxy", "https_proxy", "all_proxy", "no_proxy",
}

// localeEnvVars are the locale-related environment variables the
// system.environment check surfaces, mirroring the Rust LOCALE_ENV_VARS list.
var localeEnvVars = []string{"LANG", "LC_ALL", "LC_CTYPE", "LANGUAGE"}

// systemEnvironmentCheck reports OS/runtime metadata and the effective locale,
// mirroring system.environment in doctor.rs. It is always informational (ok).
func systemEnvironmentCheck() doctorCheck {
	b := newCheck("system.environment", "system")
	b.detail(fmt.Sprintf("os: %s", runtime.GOOS))
	b.detail(fmt.Sprintf("arch: %s", runtime.GOARCH))
	b.detail(fmt.Sprintf("go: %s", runtime.Version()))

	language := osLanguage()
	if language != "" {
		b.detail(fmt.Sprintf("os language: %s", language))
	} else {
		b.detail("os language: unavailable")
	}
	for _, name := range localeEnvVars {
		if value, ok := os.LookupEnv(name); ok && value != "" {
			b.detail(fmt.Sprintf("%s: %s", name, value))
		}
	}

	if language != "" {
		b.ok(fmt.Sprintf("OS language %s", language))
	} else {
		b.ok("OS language unavailable")
	}
	return b.build()
}

// terminalEnvCheck reports detected terminal metadata, mirroring terminal.env in
// doctor.rs. A dumb terminal degrades to a warning because the interactive TUI
// cannot run there; all other cases are informational.
func terminalEnvCheck() doctorCheck {
	b := newCheck("terminal.env", "terminal")
	info := termdetect.Detect()
	if info.TermProgram != "" {
		b.detail(fmt.Sprintf("TERM_PROGRAM: %s", info.TermProgram))
	}
	if info.Term != "" {
		b.detail(fmt.Sprintf("TERM: %s", info.Term))
	}
	if value, ok := os.LookupEnv("COLORTERM"); ok && value != "" {
		b.detail(fmt.Sprintf("COLORTERM: %s", value))
	}
	b.detail(fmt.Sprintf("stdout is terminal: %t", isTerminal(os.Stdout)))
	b.detail(fmt.Sprintf("stderr is terminal: %t", isTerminal(os.Stderr)))
	b.detail(fmt.Sprintf("stdin is terminal: %t", isTerminal(os.Stdin)))
	b.detail(fmt.Sprintf("effective locale: %s", orNone(effectiveLocale())))

	if info.Name == termdetect.TerminalDumb {
		b.warn("TERM is set to \"dumb\"; the interactive TUI may not work").
			remedy("Run in a supported terminal or unset TERM")
		return b.build()
	}
	b.ok("terminal metadata was detected")
	return b.build()
}

// terminalTitleCheck reports the configured terminal-title behavior, mirroring
// terminal.title in doctor.rs. With no override it reports the default item set.
func terminalTitleCheck(dctx doctorContext) doctorCheck {
	b := newCheck("terminal.title", "title")
	source := "default"
	items := []string{"activity", "project-name"}
	if dctx.Loaded && dctx.Cfg.Tui != nil && dctx.Cfg.Tui.TerminalTitle != nil {
		configured := *dctx.Cfg.Tui.TerminalTitle
		if len(configured) == 0 {
			source = "disabled"
			items = nil
		} else {
			source = "configured"
			items = configured
		}
	}
	b.detail(fmt.Sprintf("terminal title source: %s", source))
	if len(items) == 0 {
		b.detail("terminal title items: none")
	} else {
		b.detail(fmt.Sprintf("terminal title items: %s", joinComma(items)))
	}
	b.detail(fmt.Sprintf("terminal title activity: %t", containsString(items, "activity")))
	b.ok(fmt.Sprintf("terminal title %s", source))
	return b.build()
}

// networkEnvCheck reports the presence of proxy and custom-CA environment
// variables, mirroring network.env in doctor.rs. An unreadable custom CA file
// degrades to a warning; otherwise the check is informational.
func networkEnvCheck() doctorCheck {
	b := newCheck("network.env", "network")

	var present []string
	for _, name := range proxyEnvVars {
		if envVarPresent(name) {
			present = append(present, name)
		}
	}
	if len(present) == 0 {
		b.detail("proxy env vars: none")
	} else {
		sort.Strings(present)
		b.detail(fmt.Sprintf("proxy env vars present: %s", joinComma(present)))
	}

	status := statusOK
	summary := "network-related environment looks readable"
	for _, name := range []string{"CODEX_CA_CERTIFICATE", "SSL_CERT_FILE"} {
		raw, ok := os.LookupEnv(name)
		if !ok || raw == "" {
			continue
		}
		info, err := os.Stat(raw)
		switch {
		case err == nil && info.Mode().IsRegular():
			if file, openErr := os.Open(raw); openErr != nil {
				status = statusWarning
				summary = "custom CA env var points at an unreadable file"
				b.detail(fmt.Sprintf("%s: %s (%v)", name, raw, openErr))
			} else {
				_ = file.Close()
				b.detail(fmt.Sprintf("%s: readable file %s", name, raw))
			}
		case err == nil:
			status = statusWarning
			summary = "custom CA env var does not point at a file"
			b.detail(fmt.Sprintf("%s: not a file %s", name, raw))
		default:
			status = statusWarning
			summary = "custom CA env var points at an unreadable path"
			b.detail(fmt.Sprintf("%s: %s (%v)", name, raw, err))
		}
	}

	if status == statusWarning {
		b.warn(summary)
	} else {
		b.ok(summary)
	}
	return b.build()
}

// effectiveLocale resolves the effective locale from LC_ALL, then LANG, then
// LC_CTYPE, mirroring the precedence the Rust doctor uses.
func effectiveLocale() string {
	for _, name := range []string{"LC_ALL", "LANG", "LC_CTYPE"} {
		if value, ok := os.LookupEnv(name); ok && value != "" {
			return value
		}
	}
	return ""
}

// osLanguage derives a best-effort OS language tag from the effective locale, for
// example "en_US.UTF-8" -> "en-US".
func osLanguage() string {
	locale := effectiveLocale()
	if locale == "" || locale == "C" || locale == "POSIX" {
		return ""
	}
	if idx := indexByteSafe(locale, '.'); idx >= 0 {
		locale = locale[:idx]
	}
	return replaceRune(locale, '_', '-')
}

// containsString reports whether items contains value.
func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

// indexByteSafe returns the index of b in s, or -1 when absent.
func indexByteSafe(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// replaceRune returns s with every old byte replaced by new.
func replaceRune(s string, old, new byte) string {
	out := []byte(s)
	for i := range out {
		if out[i] == old {
			out[i] = new
		}
	}
	return string(out)
}
