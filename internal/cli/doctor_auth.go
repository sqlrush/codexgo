package cli

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/sqlrush/codexgo/internal/login"
)

// authEnvVars are the environment variables that can supply Codex credentials
// without a stored auth.json, matching the env keys the Rust doctor inspects.
var authEnvVars = []string{"OPENAI_API_KEY", "CODEXGO_API_KEY", "CODEXGO_ACCESS_TOKEN"}

// authCheck reports the current login state without exposing secrets, mirroring
// auth.credentials in doctor.rs. It records storage mode, the auth file path, and
// which credential kinds are present, then classifies the result as ok/warn/error.
func authCheck(ctx context.Context, dctx doctorContext) doctorCheck {
	b := newCheck("auth.credentials", "auth")
	if !dctx.Loaded {
		b.warn("skipped: configuration did not load")
		return b.build()
	}

	authPath := filepath.Join(dctx.CodexHome, "auth.json")
	b.detail(fmt.Sprintf("auth storage mode: %s", dctx.StoreMode))
	b.detail(fmt.Sprintf("auth file: %s", authPath))

	var envPresent []string
	for _, name := range authEnvVars {
		if envVarPresent(name) {
			envPresent = append(envPresent, name)
		}
	}
	if len(envPresent) > 0 {
		b.detail(fmt.Sprintf("auth env vars present: %s", joinComma(envPresent)))
	}

	stored, err := login.LoadAuthDotJson(dctx.CodexHome, dctx.StoreMode)
	if err != nil {
		b.fail("stored credentials could not be read").
			detail(fmt.Errorf("auth read error: %w", err).Error()).
			remedy("Fix auth storage access or run codex login again.")
		return b.build()
	}
	if stored == nil {
		if len(envPresent) > 0 {
			b.ok("auth is provided by environment")
			return b.build()
		}
		b.fail("no Codex credentials were found").
			remedy("Run codex login or provide an API key through a supported auth env var.")
		return b.build()
	}

	b.detail(fmt.Sprintf("stored API key: %t", stored.OpenAIAPIKey != nil))
	b.detail(fmt.Sprintf("stored ChatGPT tokens: %t", stored.Tokens != nil))
	b.detail(fmt.Sprintf("stored agent identity: %t", stored.AgentIdentity != nil))

	auth, err := login.FromAuthDotJson(ctx, http.DefaultClient, dctx.CodexHome, stored, dctx.StoreMode, dctx.ChatgptBaseURL)
	if err != nil {
		b.warn("stored credentials are present but could not be loaded").
			detail(fmt.Errorf("auth load error: %w", err).Error())
		return b.build()
	}
	b.detail(fmt.Sprintf("stored auth mode: %s", auth.AuthMode()))

	if len(envPresent) > 1 {
		b.warn("auth is configured, but multiple auth env vars are present")
		return b.build()
	}
	b.ok("auth is configured")
	return b.build()
}

// joinComma joins items with ", ", returning "" for an empty slice.
func joinComma(items []string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += ", "
		}
		out += item
	}
	return out
}
