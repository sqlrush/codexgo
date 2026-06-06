package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/login"
	"github.com/sqlrush/codexgo/internal/modelproviderinfo"
)

const loginSuccessMessage = "Successfully logged in"

// loginArgs holds the parsed `codex login` flags. It mirrors the LoginCommand
// surface in main.rs (the subset this port wires).
type loginArgs struct {
	status          bool
	withAPIKey      bool
	withAccessToken bool
	useDeviceCode   bool
	deprecatedKey   bool
	help            bool
}

// runLoginSubcommand handles `codex login [status] [--with-api-key] [--with-access-token] [--device-auth]`.
func runLoginSubcommand(ctx context.Context, parsed ParsedCommandLine, streams Streams) int {
	args, err := parseLoginArgs(parsed.SubcommandArgs)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 1
	}
	if args.help {
		printLoginHelp(streams.Stdout)
		return 0
	}

	cfg, err := loadConfig(parsed.Root)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "Error %v\n", err)
		return 1
	}

	if args.status {
		return runLoginStatus(ctx, cfg, streams)
	}

	switch {
	case args.withAPIKey && args.withAccessToken:
		fmt.Fprintln(streams.Stderr, "Choose one login credential source: --with-api-key or --with-access-token.")
		return 1
	case args.useDeviceCode:
		return runLoginWithDeviceCode(ctx, cfg, streams)
	case args.deprecatedKey:
		fmt.Fprintln(streams.Stderr, "The --api-key flag is no longer supported. Pipe the key instead, e.g. `printenv OPENAI_API_KEY | codex login --with-api-key`.")
		return 1
	case args.withAPIKey:
		return runLoginWithAPIKey(cfg, streams)
	case args.withAccessToken:
		return runLoginWithAccessToken(ctx, cfg, streams)
	default:
		return runLoginWithChatGPT(streams, cfg)
	}
}

// runLoginStatus reports the current login state, mirroring run_login_status.
func runLoginStatus(ctx context.Context, cfg loadedConfig, streams Streams) int {
	stored, err := login.LoadAuthDotJson(cfg.CodexHome, cfg.StoreMode)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "Error checking login status: %v\n", err)
		return 1
	}
	if stored == nil {
		fmt.Fprintln(streams.Stderr, "Not logged in")
		return 1
	}

	auth, err := login.FromAuthDotJson(ctx, http.DefaultClient, cfg.CodexHome, stored, cfg.StoreMode, cfg.ChatgptBaseURL)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "Error checking login status: %v\n", err)
		return 1
	}

	switch auth.AuthMode() {
	case appserverproto.AuthModeApiKey:
		token, err := auth.GetToken()
		if err != nil {
			fmt.Fprintf(streams.Stderr, "Unexpected error retrieving API key: %v\n", err)
			return 1
		}
		fmt.Fprintf(streams.Stderr, "Logged in using an API key - %s\n", safeFormatKey(token))
		return 0
	case appserverproto.AuthModeAgentIdentity:
		fmt.Fprintln(streams.Stderr, "Logged in using access token")
		return 0
	default:
		fmt.Fprintln(streams.Stderr, "Logged in using ChatGPT")
		return 0
	}
}

// runLoginWithAPIKey reads the API key from stdin and stores it.
func runLoginWithAPIKey(cfg loadedConfig, streams Streams) int {
	apiKey, ok := readStdinSecret(streams,
		"--with-api-key expects the API key on stdin. Try piping it, e.g. `printenv OPENAI_API_KEY | codex login --with-api-key`.",
		"Reading API key from stdin...",
		"No API key provided via stdin.")
	if !ok {
		return 1
	}
	if err := login.LoginWithAPIKey(cfg.CodexHome, apiKey, cfg.StoreMode); err != nil {
		fmt.Fprintf(streams.Stderr, "Error logging in: %v\n", err)
		return 1
	}
	fmt.Fprintln(streams.Stderr, loginSuccessMessage)
	return 0
}

// runLoginWithAccessToken reads an access token from stdin and stores it.
func runLoginWithAccessToken(ctx context.Context, cfg loadedConfig, streams Streams) int {
	token, ok := readStdinSecret(streams,
		"--with-access-token expects the access token on stdin. Try piping it, e.g. `printenv CODEX_ACCESS_TOKEN | codex login --with-access-token`.",
		"Reading access token from stdin...",
		"No access token provided via stdin.")
	if !ok {
		return 1
	}
	_ = ctx
	if err := login.LoginWithAccessTokenRecord(cfg.CodexHome, token, cfg.StoreMode); err != nil {
		fmt.Fprintf(streams.Stderr, "Error logging in with access token: %v\n", err)
		return 1
	}
	fmt.Fprintln(streams.Stderr, loginSuccessMessage)
	return 0
}

// runLoginWithChatGPT starts the local browser login server and waits for the
// flow to complete, mirroring login_with_chatgpt.
func runLoginWithChatGPT(streams Streams, cfg loadedConfig) int {
	opts := login.NewServerOptions(cfg.CodexHome, login.ClientID, nil, cfg.StoreMode)
	server, err := login.RunLoginServer(opts)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "Error logging in: %v\n", err)
		return 1
	}
	fmt.Fprintf(streams.Stderr,
		"Starting local login server on http://localhost:%d.\nIf your browser did not open, navigate to this URL to authenticate:\n\n%s\n\nOn a remote or headless machine? Use `codex login --device-auth` instead.\n",
		server.ActualPort, server.AuthURL)
	if err := server.Wait(); err != nil {
		fmt.Fprintf(streams.Stderr, "Error logging in: %v\n", err)
		return 1
	}
	fmt.Fprintln(streams.Stderr, loginSuccessMessage)
	return 0
}

// runLoginWithDeviceCode runs the OAuth device-code login flow for headless or
// remote machines. Mirrors cli/src/login.rs::run_login_with_device_code +
// login/src/device_code_auth.rs::run_device_code_login: it requests a device
// code, prints the sign-in prompt to stdout, polls for completion, and persists
// credentials. The prompt uses stdout (Rust println!), status messages stderr
// (Rust eprintln!).
func runLoginWithDeviceCode(ctx context.Context, cfg loadedConfig, streams Streams) int {
	httpClient, err := login.NewHTTPClient()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "Error logging in with device code: %v\n", err)
		return 1
	}
	opts := login.NewServerOptions(cfg.CodexHome, login.ClientID, nil, cfg.StoreMode)
	if err := login.RunDeviceCodeLogin(ctx, httpClient, opts, streams.Stdout, modelproviderinfo.CodexPkgVersion); err != nil {
		fmt.Fprintf(streams.Stderr, "Error logging in with device code: %v\n", err)
		return 1
	}
	fmt.Fprintln(streams.Stderr, loginSuccessMessage)
	return 0
}

// readStdinSecret reads a trimmed secret from stdin, mirroring read_stdin_secret.
// It returns ok=false (after printing the appropriate message) when stdin is a
// terminal, unreadable, or empty.
func readStdinSecret(streams Streams, terminalMsg, readingMsg, emptyMsg string) (string, bool) {
	if streams.StdinIsTerminal {
		fmt.Fprintln(streams.Stderr, terminalMsg)
		return "", false
	}
	fmt.Fprintln(streams.Stderr, readingMsg)
	data, err := io.ReadAll(streams.Stdin)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "Failed to read stdin: %v\n", err)
		return "", false
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		fmt.Fprintln(streams.Stderr, emptyMsg)
		return "", false
	}
	return secret, true
}

// safeFormatKey redacts an API key, keeping a short prefix/suffix for long keys,
// mirroring safe_format_key in login.rs.
func safeFormatKey(key string) string {
	if len(key) <= 13 {
		return "***"
	}
	return key[:8] + "***" + key[len(key)-5:]
}

// parseLoginArgs parses the login flags and optional `status` subcommand.
func parseLoginArgs(args []string) (loginArgs, error) {
	var out loginArgs
	for _, arg := range args {
		switch arg {
		case "status":
			out.status = true
		case "--with-api-key":
			out.withAPIKey = true
		case "--with-access-token":
			out.withAccessToken = true
		case "--device-auth":
			out.useDeviceCode = true
		case "--api-key":
			out.deprecatedKey = true
		case "-h", "--help":
			out.help = true
		default:
			if strings.HasPrefix(arg, "--api-key=") {
				out.deprecatedKey = true
				continue
			}
			return loginArgs{}, fmt.Errorf("unexpected argument: %s", arg)
		}
	}
	return out, nil
}

func printLoginHelp(w io.Writer) {
	fmt.Fprintln(w, "Manage login")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex login [OPTIONS]")
	fmt.Fprintln(w, "       codex login status")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "      --with-api-key        Read the API key from stdin")
	fmt.Fprintln(w, "      --with-access-token   Read the access token from stdin")
	fmt.Fprintln(w, "      --device-auth         Use the OAuth device code flow")
	fmt.Fprintln(w, "  -h, --help                Print help")
}
