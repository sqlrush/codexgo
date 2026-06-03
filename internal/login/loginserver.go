package login

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
)

// LoginServer is a running local OAuth callback server. Mirrors
// server::LoginServer. The caller obtains AuthURL (to open in a browser) and
// calls Wait to block until the flow completes, fails, or is cancelled.
type LoginServer struct {
	// AuthURL is the browser authorization URL to visit.
	AuthURL string
	// ActualPort is the port the callback server bound to (1455 or 1457).
	ActualPort int

	httpServer *http.Server
	done       chan error
	closeOnce  sync.Once
	cancel     context.CancelFunc
}

// Wait blocks until the login flow finishes, returning nil on success or an
// error describing the failure/cancellation. Mirrors LoginServer::block_until_done.
func (s *LoginServer) Wait() error {
	return <-s.done
}

// Cancel signals the server to stop and unblocks Wait with a cancellation error.
// Mirrors LoginServer::cancel.
func (s *LoginServer) Cancel() {
	s.finish(errors.New("Login was not completed"))
}

// finish records the terminal result once and shuts the server down.
func (s *LoginServer) finish(result error) {
	s.closeOnce.Do(func() {
		s.done <- result
		close(s.done)
		if s.cancel != nil {
			s.cancel()
		}
		if s.httpServer != nil {
			_ = s.httpServer.Close()
		}
	})
}

// RunLoginServer binds the local callback server, builds the browser auth URL,
// and starts serving callbacks. Mirrors server::run_login_server. Browser
// opening is left to the caller (Go has no portable webbrowser dependency here):
// callers open opts-driven AuthURL themselves.
func RunLoginServer(opts ServerOptions) (*LoginServer, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}
	state := ""
	if opts.ForceState != nil {
		state = *opts.ForceState
	} else {
		state, err = GenerateState()
		if err != nil {
			return nil, err
		}
	}

	listener, actualPort, err := bindCallbackListener(opts.Port)
	if err != nil {
		return nil, err
	}

	redirectURI := redirectURIForPort(actualPort)
	authURL := BuildAuthorizeURL(opts.Issuer, opts.ClientID, redirectURI, pkce, state, opts.ForcedChatgptWorkspaceID)

	httpClient, err := newHTTPClient()
	if err != nil {
		_ = listener.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	server := &LoginServer{
		AuthURL:    authURL,
		ActualPort: actualPort,
		done:       make(chan error, 1),
		cancel:     cancel,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		server.handleCallback(ctx, w, r, httpClient, opts, redirectURI, pkce, actualPort, state)
	})
	mux.HandleFunc("/success", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>Login successful. You may close this window.</body></html>"))
		server.finish(nil)
	})
	mux.HandleFunc("/cancel", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Login cancelled"))
		server.finish(errors.New("Login cancelled"))
	})

	server.httpServer = &http.Server{Handler: mux}
	go func() {
		if serveErr := server.httpServer.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			server.finish(serveErr)
		}
	}()

	return server, nil
}

// handleCallback processes the /auth/callback request. It mirrors the callback
// branch of server::process_request: it validates state, surfaces OAuth errors,
// exchanges the code, enforces workspace restriction, obtains the API key,
// persists tokens, and redirects to the success page.
func (s *LoginServer) handleCallback(ctx context.Context, w http.ResponseWriter, r *http.Request, httpClient *http.Client, opts ServerOptions, redirectURI string, pkce PkceCodes, actualPort int, state string) {
	params := r.URL.Query()
	if params.Get("state") != state {
		http.Error(w, "State mismatch", http.StatusBadRequest)
		return
	}
	if errorCode := params.Get("error"); errorCode != "" {
		var desc *string
		if d := params.Get("error_description"); d != "" {
			desc = &d
		}
		message := oauthCallbackErrorMessage(errorCode, desc)
		http.Error(w, message, http.StatusForbidden)
		s.finish(errors.New(message))
		return
	}
	code := params.Get("code")
	if code == "" {
		message := "Missing authorization code. Sign-in could not be completed."
		http.Error(w, message, http.StatusBadRequest)
		s.finish(errors.New(message))
		return
	}

	tokens, err := ExchangeCodeForTokens(ctx, httpClient, opts.Issuer, opts.ClientID, redirectURI, pkce, code)
	if err != nil {
		http.Error(w, fmt.Sprintf("Token exchange failed: %v", err), http.StatusInternalServerError)
		s.finish(err)
		return
	}

	if err := EnsureWorkspaceAllowed(opts.ForcedChatgptWorkspaceID, tokens.IDToken); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		s.finish(err)
		return
	}

	var apiKey *string
	if key, keyErr := ObtainAPIKey(ctx, httpClient, opts.Issuer, opts.ClientID, tokens.IDToken); keyErr == nil {
		apiKey = &key
	}

	if err := PersistTokens(ctx, httpClient, opts.CodexHome, apiKey, tokens.IDToken, tokens.AccessToken, tokens.RefreshToken, opts.CLIAuthCredentialsStoreMode); err != nil {
		http.Error(w, "Sign-in completed but credentials could not be saved locally.", http.StatusInternalServerError)
		s.finish(err)
		return
	}

	successURL := ComposeSuccessURL(actualPort, opts.Issuer, tokens.IDToken, tokens.AccessToken, opts.CodexStreamlinedLogin)
	http.Redirect(w, r, successURL, http.StatusFound)
}

// bindCallbackListener binds the preferred port, falling back to FallbackPort
// when the preferred port is the default and is unavailable. It mirrors the port
// selection policy of server::bind_server (without the cancel-and-retry of an
// existing server, which the orchestrating CLI handles).
func bindCallbackListener(preferred int) (net.Listener, int, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferred))
	if err == nil {
		return listener, listenerPort(listener), nil
	}
	if preferred == DefaultPort {
		fallback, fbErr := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", FallbackPort))
		if fbErr == nil {
			return fallback, listenerPort(fallback), nil
		}
	}
	return nil, 0, fmt.Errorf("port %d is already in use: %w", preferred, err)
}

// listenerPort returns the bound TCP port of a listener.
func listenerPort(l net.Listener) int {
	if addr, ok := l.Addr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}
