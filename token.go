package bitquery

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TokenProvider supplies OAuth access tokens for HTTP Bearer auth and
// the WSS `token` URL parameter. Implementations must never leak tokens
// into errors or logs.
type TokenProvider interface {
	Token(ctx context.Context) (string, error)
	// Refresh forces a fresh token (used after a 401).
	Refresh(ctx context.Context) (string, error)
}

// StaticTokenProvider returns a pre-minted access token.
type StaticTokenProvider struct{ token string }

func NewStaticTokenProvider(token string) *StaticTokenProvider {
	return &StaticTokenProvider{token: token}
}

func (p *StaticTokenProvider) Token(context.Context) (string, error) {
	if strings.TrimSpace(p.token) == "" {
		return "", &Error{Kind: KindConfig, Message: "static access token cannot be empty"}
	}
	return p.token, nil
}

func (p *StaticTokenProvider) Refresh(ctx context.Context) (string, error) {
	return p.Token(ctx)
}

// ClientCredentialsProvider mints OAuth tokens via client_credentials
// (grant_type=client_credentials, scope=api) against the Bitquery OAuth
// endpoint, caches them to expiry (minus a safety margin) and coalesces
// concurrent refreshes.
//
// https://docs.bitquery.io/docs/authorization/how-to-generate/
type ClientCredentialsProvider struct {
	clientID         string
	clientSecret     string
	tokenURL         string
	endpointExplicit bool
	scope            string
	httpClient       *http.Client
	now              func() time.Time // injectable for tests

	mu        sync.Mutex
	cached    string
	expiresAt time.Time
}

// ClientCredentialsOption configures the provider.
type ClientCredentialsOption func(*ClientCredentialsProvider)

// WithOAuthHTTPClient supplies a caller-owned HTTP client for token calls.
func WithOAuthHTTPClient(hc *http.Client) ClientCredentialsOption {
	return func(p *ClientCredentialsProvider) { p.httpClient = hc }
}

// WithOAuthScope overrides the requested scope (default "api").
func WithOAuthScope(scope string) ClientCredentialsOption {
	return func(p *ClientCredentialsProvider) { p.scope = scope }
}

// WithOAuthTokenEndpoint overrides the OAuth token endpoint. It is
// useful for regional proxies and local test servers; the endpoint must
// be an absolute HTTP(S) URL.
func WithOAuthTokenEndpoint(endpoint string) ClientCredentialsOption {
	return func(p *ClientCredentialsProvider) {
		p.tokenURL = endpoint
		p.endpointExplicit = true
	}
}

func newClientCredentialsProvider(clientID, clientSecret, tokenURL string, opts ...ClientCredentialsOption) *ClientCredentialsProvider {
	if tokenURL == "" {
		tokenURL = OAuthTokenEndpoint
	}
	p := &ClientCredentialsProvider{
		clientID:     clientID,
		clientSecret: clientSecret,
		tokenURL:     tokenURL,
		scope:        "api",
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		now:          time.Now,
	}
	for _, o := range opts {
		if o != nil {
			o(p)
		}
	}
	if p.httpClient == nil {
		p.httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return p
}

// NewClientCredentialsProvider builds a provider using the default OAuth endpoint.
func NewClientCredentialsProvider(clientID, clientSecret string, opts ...ClientCredentialsOption) *ClientCredentialsProvider {
	return newClientCredentialsProvider(clientID, clientSecret, OAuthTokenEndpoint, opts...)
}

const oauthExpiryMargin = 60 * time.Second

func (p *ClientCredentialsProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cached != "" && p.now().Before(p.expiresAt) {
		return p.cached, nil
	}
	return p.fetch(ctx)
}

func (p *ClientCredentialsProvider) Refresh(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fetch(ctx)
}

// fetch performs the token request. Caller must hold mu — this is what
// coalesces concurrent refreshes into a single OAuth call.
func (p *ClientCredentialsProvider) fetch(ctx context.Context) (string, error) {
	if strings.TrimSpace(p.clientID) == "" || strings.TrimSpace(p.clientSecret) == "" {
		return "", &Error{Kind: KindConfig, Message: "OAuth client ID and client secret cannot be empty"}
	}
	if strings.TrimSpace(p.scope) == "" {
		return "", &Error{Kind: KindConfig, Message: "OAuth scope cannot be empty"}
	}
	if err := validateHTTPURL(p.tokenURL, "OAuth token endpoint"); err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
		"scope":         {p.scope},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", &Error{Kind: KindTransport, Message: "build token request: " + err.Error(), cause: err}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", &Error{Kind: KindTransport, Message: "OAuth token request failed: " + err.Error(), cause: err}
	}
	defer resp.Body.Close()

	body, err := readLimited(resp.Body, 1<<20)
	if err != nil {
		return "", &Error{Kind: KindTransport, Message: "read token response: " + err.Error(), cause: err}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		// Do not include the body: OAuth servers often echo credential
		// details in their diagnostic payloads.
		return "", oauthHTTPError(resp.StatusCode)
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := jsonUnmarshal(body, &parsed); err != nil || parsed.AccessToken == "" || parsed.ExpiresIn <= 0 {
		// Response body intentionally omitted — may echo sensitive detail.
		return "", newError(KindTransport, resp.StatusCode, "OAuth token response is invalid", err)
	}

	p.cached = parsed.AccessToken
	lifetime := time.Duration(parsed.ExpiresIn) * time.Second
	margin := oauthExpiryMargin
	if tenth := lifetime / 10; tenth < margin {
		margin = tenth
	}
	p.expiresAt = p.now().Add(lifetime - margin)
	return p.cached, nil
}

func oauthHTTPError(status int) *Error {
	kind := KindTransport
	message := "OAuth token request rejected"
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		kind = KindAuthentication
	case http.StatusTooManyRequests:
		kind = KindRateLimited
		message = "OAuth token request rate limited"
	case http.StatusPaymentRequired:
		kind = KindPlanEntitlement
	}
	return &Error{
		Kind:       kind,
		StatusCode: status,
		Message:    message,
		Temporary:  status == http.StatusTooManyRequests || status >= 500,
		Context:    map[string]any{"status": status},
	}
}
