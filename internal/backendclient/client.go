package backendclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sqlrush/codexgo/internal/login"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// PathStyle selects the URL prefix used for backend requests.
type PathStyle int

const (
	// PathStyleCodexAPI uses /api/codex/… paths.
	PathStyleCodexAPI PathStyle = iota
	// PathStyleChatGptAPI uses /wham/… paths.
	PathStyleChatGptAPI
)

// PathStyleFromBaseURL chooses the path style from the base URL, mirroring the
// Rust `PathStyle::from_base_url`.
func PathStyleFromBaseURL(baseURL string) PathStyle {
	if strings.Contains(baseURL, "/backend-api") {
		return PathStyleChatGptAPI
	}
	return PathStyleCodexAPI
}

// AddCreditsNudgeCreditType identifies the kind of credits nudge email to send.
type AddCreditsNudgeCreditType string

const (
	// AddCreditsNudgeCredits requests a credits-depleted nudge.
	AddCreditsNudgeCredits AddCreditsNudgeCreditType = "credits"
	// AddCreditsNudgeUsageLimit requests a usage-limit nudge.
	AddCreditsNudgeUsageLimit AddCreditsNudgeCreditType = "usage_limit"
)

// RequestError describes a failed backend HTTP request. It mirrors the Rust
// `RequestError` enum's UnexpectedStatus / Other variants.
type RequestError struct {
	// Method is the HTTP method (empty for transport-level errors).
	Method string
	// URL is the request URL (empty for transport-level errors).
	URL string
	// StatusCode is the HTTP status code; 0 when there was no response.
	StatusCode int
	// ContentType is the response content type.
	ContentType string
	// Body is the response body.
	Body string
	// Err is the underlying transport error, when StatusCode is 0.
	Err error
}

// Error implements error, mirroring the Rust `Display` impl.
func (e *RequestError) Error() string {
	if e.StatusCode == 0 {
		if e.Err != nil {
			return e.Err.Error()
		}
		return "request failed"
	}
	return fmt.Sprintf("%s %s failed: %d; content-type=%s; body=%s",
		e.Method, e.URL, e.StatusCode, e.ContentType, e.Body)
}

// Unwrap exposes the underlying transport error.
func (e *RequestError) Unwrap() error { return e.Err }

// Status returns the HTTP status code, or 0 when none.
func (e *RequestError) Status() int { return e.StatusCode }

// IsUnauthorized reports whether the failure was an HTTP 401.
func (e *RequestError) IsUnauthorized() bool { return e.StatusCode == http.StatusUnauthorized }

// AuthSource supplies the bearer token used to authenticate requests. It mirrors
// the Rust `SharedAuthProvider`, which only contributes the Authorization
// header; the account-id and FedRAMP headers are set separately on the client.
type AuthSource interface {
	// Token returns the bearer token and true when one is available.
	Token() (string, bool)
}

// Client is a small REST client for the Codex/ChatGPT backend-api. It mirrors
// the Rust `codex_backend_client::Client`.
type Client struct {
	baseURL   string
	http      *http.Client
	auth      AuthSource
	userAgent string
	accountID string
	fedramp   bool
	pathStyle PathStyle
}

// NewClient constructs a client with the given base URL. ChatGPT hostnames are
// normalized to include /backend-api. It mirrors the Rust `Client::new`.
func NewClient(baseURL string) *Client {
	normalized := NormalizeBaseURL(baseURL)
	return &Client{
		baseURL:   normalized,
		http:      &http.Client{},
		auth:      noAuth{},
		pathStyle: PathStyleFromBaseURL(normalized),
	}
}

// NormalizeBaseURL trims trailing slashes and appends /backend-api for ChatGPT
// hosts when missing. It mirrors the URL normalization in the Rust `Client::new`.
func NormalizeBaseURL(input string) string {
	baseURL := strings.TrimRight(input, "/")
	if (strings.HasPrefix(baseURL, "https://chatgpt.com") ||
		strings.HasPrefix(baseURL, "https://chat.openai.com")) &&
		!strings.Contains(baseURL, "/backend-api") {
		baseURL = baseURL + "/backend-api"
	}
	return baseURL
}

type noAuth struct{}

func (noAuth) Token() (string, bool) { return "", false }

// codexAuthSource adapts *login.CodexAuth to AuthSource.
type codexAuthSource struct {
	auth *login.CodexAuth
}

func (c codexAuthSource) Token() (string, bool) {
	token, err := c.auth.GetToken()
	if err != nil || token == "" {
		return "", false
	}
	return token, true
}

// FromAuth builds an authenticated client from a CodexAuth, mirroring the Rust
// `Client::from_auth` (user agent + auth provider only). The account-id and
// FedRAMP routing headers are set separately by callers via the builder methods,
// matching the reference behavior. The user agent is supplied by the caller
// because the Go port does not maintain a global user-agent helper.
func FromAuth(baseURL string, auth *login.CodexAuth, userAgent string) *Client {
	c := NewClient(baseURL).WithUserAgent(userAgent)
	if auth != nil {
		c.auth = codexAuthSource{auth: auth}
	}
	return c
}

// WithAuthSource sets the auth source, mirroring `with_auth_provider`.
func (c *Client) WithAuthSource(auth AuthSource) *Client {
	clone := *c
	clone.auth = auth
	return &clone
}

// WithUserAgent sets the User-Agent header value, mirroring `with_user_agent`.
func (c *Client) WithUserAgent(ua string) *Client {
	clone := *c
	clone.userAgent = ua
	return &clone
}

// WithChatGptAccountID sets the ChatGPT-Account-Id header value, mirroring
// `with_chatgpt_account_id`.
func (c *Client) WithChatGptAccountID(id string) *Client {
	clone := *c
	clone.accountID = id
	return &clone
}

// WithFedrampRoutingHeader enables the FedRAMP routing header, mirroring
// `with_fedramp_routing_header`.
func (c *Client) WithFedrampRoutingHeader() *Client {
	clone := *c
	clone.fedramp = true
	return &clone
}

// WithPathStyle overrides the path style, mirroring `with_path_style`.
func (c *Client) WithPathStyle(style PathStyle) *Client {
	clone := *c
	clone.pathStyle = style
	return &clone
}

// WithHTTPClient overrides the underlying http.Client (used by tests).
func (c *Client) WithHTTPClient(h *http.Client) *Client {
	clone := *c
	clone.http = h
	return &clone
}

// BaseURL returns the normalized base URL.
func (c *Client) BaseURL() string { return c.baseURL }

func (c *Client) headers() http.Header {
	h := http.Header{}
	if c.userAgent != "" {
		h.Set("User-Agent", c.userAgent)
	} else {
		h.Set("User-Agent", "codex-cli")
	}
	if token, ok := c.auth.Token(); ok {
		h.Set("Authorization", "Bearer "+token)
	}
	if c.accountID != "" {
		h.Set("ChatGPT-Account-Id", c.accountID)
	}
	if c.fedramp {
		h.Set("X-OpenAI-Fedramp", "true")
	}
	return h
}

func (c *Client) pathFor(codexPath, whamPath string) string {
	switch c.pathStyle {
	case PathStyleChatGptAPI:
		return c.baseURL + whamPath
	default:
		return c.baseURL + codexPath
	}
}

// execRequest sends the request and returns (body, contentType). It bails with a
// formatted error on non-2xx, mirroring the Rust `exec_request`.
func (c *Client) execRequest(ctx context.Context, method, urlStr string, header http.Header, body []byte) (string, string, error) {
	respBody, ct, status, err := c.do(ctx, method, urlStr, header, body)
	if err != nil {
		return "", "", err
	}
	if status < 200 || status >= 300 {
		return "", "", fmt.Errorf("%s %s failed: %d; content-type=%s; body=%s", method, urlStr, status, ct, respBody)
	}
	return respBody, ct, nil
}

// execRequestDetailed mirrors the Rust `exec_request_detailed`, returning a
// structured RequestError on failure.
func (c *Client) execRequestDetailed(ctx context.Context, method, urlStr string, header http.Header, body []byte) (string, string, *RequestError) {
	respBody, ct, status, err := c.do(ctx, method, urlStr, header, body)
	if err != nil {
		return "", "", &RequestError{Err: err}
	}
	if status < 200 || status >= 300 {
		return "", "", &RequestError{
			Method:      method,
			URL:         urlStr,
			StatusCode:  status,
			ContentType: ct,
			Body:        respBody,
		}
	}
	return respBody, ct, nil
}

func (c *Client) do(ctx context.Context, method, urlStr string, header http.Header, body []byte) (string, string, int, error) {
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, reader)
	if err != nil {
		return "", "", 0, fmt.Errorf("build request: %w", err)
	}
	for name, values := range header {
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		// Match Rust `text().await.unwrap_or_default()` by treating read
		// failures as an empty body.
		buf.Reset()
	}
	ct := resp.Header.Get("Content-Type")
	return buf.String(), ct, resp.StatusCode, nil
}

func decodeJSON[T any](urlStr, ct, body string) (T, error) {
	var v T
	dec := json.NewDecoder(strings.NewReader(body))
	if err := dec.Decode(&v); err != nil {
		return v, fmt.Errorf("Decode error for %s: %v; content-type=%s; body=%s", urlStr, err, ct, body)
	}
	return v, nil
}

// GetRateLimits fetches usage and returns the preferred ("codex") snapshot,
// mirroring `get_rate_limits`.
func (c *Client) GetRateLimits(ctx context.Context) (protocol.RateLimitSnapshot, error) {
	snapshots, err := c.GetRateLimitsMany(ctx)
	if err != nil {
		return protocol.RateLimitSnapshot{}, err
	}
	for _, snapshot := range snapshots {
		if snapshot.LimitID != nil && *snapshot.LimitID == "codex" {
			return snapshot, nil
		}
	}
	return snapshots[0], nil
}

// GetRateLimitsMany fetches all usage snapshots, mirroring `get_rate_limits_many`.
func (c *Client) GetRateLimitsMany(ctx context.Context) ([]protocol.RateLimitSnapshot, error) {
	urlStr := c.pathFor("/api/codex/usage", "/wham/usage")
	body, ct, err := c.execRequest(ctx, http.MethodGet, urlStr, c.headers(), nil)
	if err != nil {
		return nil, err
	}
	payload, err := decodeJSON[RateLimitStatusPayload](urlStr, ct, body)
	if err != nil {
		return nil, err
	}
	return rateLimitSnapshotsFromPayload(payload), nil
}

// SendAddCreditsNudgeEmail posts a credits-nudge email request, mirroring
// `send_add_credits_nudge_email`.
func (c *Client) SendAddCreditsNudgeEmail(ctx context.Context, creditType AddCreditsNudgeCreditType) *RequestError {
	urlStr := c.pathFor(
		"/api/codex/accounts/send_add_credits_nudge_email",
		"/wham/accounts/send_add_credits_nudge_email",
	)
	body, _ := json.Marshal(map[string]string{"credit_type": string(creditType)})
	header := c.headers()
	header.Set("Content-Type", "application/json")
	_, _, reqErr := c.execRequestDetailed(ctx, http.MethodPost, urlStr, header, body)
	return reqErr
}

// ListTasks fetches a page of tasks, mirroring `list_tasks`.
func (c *Client) ListTasks(ctx context.Context, limit *int32, taskFilter, environmentID, cursor *string) (PaginatedListTaskListItem, error) {
	urlStr := c.pathFor("/api/codex/tasks/list", "/wham/tasks/list")
	query := url.Values{}
	if limit != nil {
		query.Set("limit", strconv.FormatInt(int64(*limit), 10))
	}
	if taskFilter != nil {
		query.Set("task_filter", *taskFilter)
	}
	if cursor != nil {
		query.Set("cursor", *cursor)
	}
	if environmentID != nil {
		query.Set("environment_id", *environmentID)
	}
	if encoded := query.Encode(); encoded != "" {
		urlStr = urlStr + "?" + encoded
	}
	body, ct, err := c.execRequest(ctx, http.MethodGet, urlStr, c.headers(), nil)
	if err != nil {
		return PaginatedListTaskListItem{}, err
	}
	return decodeJSON[PaginatedListTaskListItem](urlStr, ct, body)
}

// GetTaskDetails fetches and parses task details, mirroring `get_task_details`.
func (c *Client) GetTaskDetails(ctx context.Context, taskID string) (CodeTaskDetailsResponse, error) {
	parsed, _, _, err := c.GetTaskDetailsWithBody(ctx, taskID)
	return parsed, err
}

// GetTaskDetailsWithBody fetches task details and returns the parsed value plus
// the raw body and content type, mirroring `get_task_details_with_body`.
func (c *Client) GetTaskDetailsWithBody(ctx context.Context, taskID string) (CodeTaskDetailsResponse, string, string, error) {
	urlStr := c.pathFor("/api/codex/tasks/"+taskID, "/wham/tasks/"+taskID)
	body, ct, err := c.execRequest(ctx, http.MethodGet, urlStr, c.headers(), nil)
	if err != nil {
		return CodeTaskDetailsResponse{}, "", "", err
	}
	parsed, err := decodeJSON[CodeTaskDetailsResponse](urlStr, ct, body)
	if err != nil {
		return CodeTaskDetailsResponse{}, "", "", err
	}
	return parsed, body, ct, nil
}

// ListSiblingTurns fetches sibling turns for a turn, mirroring `list_sibling_turns`.
func (c *Client) ListSiblingTurns(ctx context.Context, taskID, turnID string) (TurnAttemptsSiblingTurnsResponse, error) {
	urlStr := c.pathFor(
		"/api/codex/tasks/"+taskID+"/turns/"+turnID+"/sibling_turns",
		"/wham/tasks/"+taskID+"/turns/"+turnID+"/sibling_turns",
	)
	body, ct, err := c.execRequest(ctx, http.MethodGet, urlStr, c.headers(), nil)
	if err != nil {
		return TurnAttemptsSiblingTurnsResponse{}, err
	}
	return decodeJSON[TurnAttemptsSiblingTurnsResponse](urlStr, ct, body)
}

// GetConfigRequirementsFile fetches the managed requirements file, mirroring
// `get_config_requirements_file`.
func (c *Client) GetConfigRequirementsFile(ctx context.Context) (ConfigFileResponse, *RequestError) {
	urlStr := c.pathFor("/api/codex/config/requirements", "/wham/config/requirements")
	body, ct, reqErr := c.execRequestDetailed(ctx, http.MethodGet, urlStr, c.headers(), nil)
	if reqErr != nil {
		return ConfigFileResponse{}, reqErr
	}
	parsed, err := decodeJSON[ConfigFileResponse](urlStr, ct, body)
	if err != nil {
		return ConfigFileResponse{}, &RequestError{Err: err}
	}
	return parsed, nil
}

// CreateTask posts a new task and returns the created task id, mirroring
// `create_task`. The id is taken from `task.id`, falling back to top-level `id`.
func (c *Client) CreateTask(ctx context.Context, requestBody json.RawMessage) (string, error) {
	urlStr := c.pathFor("/api/codex/tasks", "/wham/tasks")
	header := c.headers()
	header.Set("Content-Type", "application/json")
	body, ct, err := c.execRequest(ctx, http.MethodPost, urlStr, header, requestBody)
	if err != nil {
		return "", err
	}
	var v map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return "", fmt.Errorf("Decode error for %s: %v; content-type=%s; body=%s", urlStr, err, ct, body)
	}
	if taskRaw, ok := v["task"]; ok {
		var task map[string]json.RawMessage
		if json.Unmarshal(taskRaw, &task) == nil {
			if id, ok := stringField(task, "id"); ok {
				return id, nil
			}
		}
	}
	if id, ok := stringField(v, "id"); ok {
		return id, nil
	}
	return "", fmt.Errorf("POST %s succeeded but no task id found; content-type=%s; body=%s", urlStr, ct, body)
}

func stringField(m map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := m[key]
	if !ok {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}
