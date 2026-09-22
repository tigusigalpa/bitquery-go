package bitquery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/tigusigalpa/bitquery-go/internal/redact"
)

// Executor runs GraphQL operations for one explicit API version. It is
// wrapped by the v1 and v2 client packages — use those for the public
// API rather than constructing this directly.
type Executor struct {
	cfg        *Config
	version    ApiVersion
	httpClient *http.Client
}

// NewExecutor builds an executor for an explicit API version.
func NewExecutor(version ApiVersion, opts ...Option) (*Executor, error) {
	cfg, err := NewConfig(opts...)
	if err != nil {
		return nil, err
	}
	if version != V1 && version != V2 {
		return nil, &Error{Kind: KindConfig, Message: "Executor requires an explicit ApiVersion (V1 or V2)"}
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: cfg.Timeout, Transport: defaultTransport()}
	}
	return &Executor{cfg: cfg, version: version, httpClient: hc}, nil
}

// Endpoint returns the resolved HTTPS endpoint.
func (e *Executor) Endpoint() (string, error) {
	return HTTPURL(e.version, e.cfg.Region, e.cfg.BaseURL)
}

// Version returns the executor's API version.
func (e *Executor) Version() ApiVersion { return e.version }

// Config returns the executor config.
func (e *Executor) Config() *Config { return e.cfg }

// Execute runs a GraphQL operation. Tolerant mode returns responses that
// contain errors[] as-is (check resp.HasErrors / HasPartialData).
// Strict mode (Config.Strict or ExecuteStrict) fails with *Error of
// KindGraphQL — partial data stays available via Error.Response.
func (e *Executor) Execute(ctx context.Context, op Operation) (*Response, error) {
	if ctx == nil {
		return nil, &Error{Kind: KindConfig, Message: "context cannot be nil"}
	}
	resp, err := e.do(ctx, op)
	if err != nil {
		return resp, err
	}
	if e.cfg.Strict && resp.HasErrors() {
		return resp, graphqlError(resp)
	}
	return resp, nil
}

// ExecuteStrict fails on any GraphQL errors[] regardless of Strict config.
func (e *Executor) ExecuteStrict(ctx context.Context, op Operation) (*Response, error) {
	if ctx == nil {
		return nil, &Error{Kind: KindConfig, Message: "context cannot be nil"}
	}
	resp, err := e.do(ctx, op)
	if err != nil {
		return resp, err
	}
	if resp.HasErrors() {
		return resp, graphqlError(resp)
	}
	return resp, nil
}

func graphqlError(resp *Response) *Error {
	msgs := make([]string, 0, len(resp.Errors))
	for _, ge := range resp.Errors {
		msgs = append(msgs, ge.Message)
	}
	return &Error{
		Kind:          KindGraphQL,
		Message:       "GraphQL errors: " + redact.String(strings.Join(msgs, "; ")),
		StatusCode:    resp.StatusCode,
		GraphQLErrors: resp.Errors,
		Response:      resp,
		Context:       map[string]any{"error_count": len(resp.Errors)},
	}
}

func (e *Executor) do(ctx context.Context, op Operation) (*Response, error) {
	if strings.TrimSpace(op.Query) == "" {
		return nil, &Error{Kind: KindConfig, Message: "GraphQL document cannot be empty"}
	}

	endpoint, err := e.Endpoint()
	if err != nil {
		return nil, err
	}

	limiter := e.cfg.RateLimiter
	if limiter == nil {
		limiter = NopRateLimiter{}
	}
	if err := limiter.Wait(ctx); err != nil {
		return nil, &Error{Kind: KindTransport, Message: "rate limiter wait: " + err.Error(), cause: err}
	}

	policy := e.cfg.Retry
	if policy == nil {
		policy = NoRetry()
	}
	sleep := policy.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	canRetry := op.isRetryableRead() && policy.MaxAttempts > 1

	log := loggerOrNop(e.cfg.Logger)
	retried401 := false
	attempt := 0

	for {
		attempt++
		token, err := e.cfg.TokenProvider.Token(ctx)
		if err != nil {
			return nil, newError(KindAuthentication, 0, "retrieve access token: "+err.Error(), err)
		}

		status, header, body, terr := e.roundTrip(ctx, endpoint, op, token)
		if terr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if canRetry && attempt < policy.MaxAttempts {
				if serr := sleep(ctx, policy.Delay(attempt, 0)); serr != nil {
					return nil, serr
				}
				continue
			}
			return nil, &Error{Kind: KindTransport, Message: terr.Error(), Temporary: true, cause: terr}
		}

		if status >= 200 && status < 300 {
			resp, parseErr := parseGraphQLResponse(status, header, body)
			if parseErr != nil {
				return resp, parseErr
			}
			if resp.HasErrors() {
				log.Warn("Bitquery GraphQL response contains errors[]",
					"endpoint", redact.URL(endpoint),
					"version", e.version.String(),
					"status", status,
					"partial", resp.HasPartialData(),
				)
			}
			return resp, nil
		}

		if status == http.StatusUnauthorized && canRetry && !retried401 {
			retried401 = true
			if _, rerr := e.cfg.TokenProvider.Refresh(ctx); rerr != nil {
				return nil, rerr
			}
			continue
		}

		retryAfter := parseRetryAfter(header.Get("Retry-After"), time.Now())
		sharedCompute := strings.Contains(strings.ToLower(string(body)), "temporarily blocked")

		if canRetry && (policy.retryableStatus(status) || sharedCompute) && attempt < policy.MaxAttempts {
			if serr := sleep(ctx, policy.Delay(attempt, retryAfter)); serr != nil {
				return nil, serr
			}
			continue
		}

		return nil, classifyHTTPError(status, body, retryAfter, sharedCompute)
	}
}

func (e *Executor) roundTrip(ctx context.Context, endpoint string, op Operation, token string) (int, http.Header, []byte, error) {
	payload, err := json.Marshal(op)
	if err != nil {
		return 0, nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", e.cfg.UserAgent)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()

	body, err := readLimited(resp.Body, 64<<20)
	if err != nil {
		return resp.StatusCode, resp.Header, nil, err
	}
	return resp.StatusCode, resp.Header, body, nil
}

func defaultTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 20
	transport.IdleConnTimeout = 90 * time.Second
	transport.TLSHandshakeTimeout = 10 * time.Second
	return transport
}

func parseGraphQLResponse(status int, header http.Header, body []byte) (*Response, error) {
	resp := &Response{
		StatusCode: status,
		Header:     header,
		RawBody:    body,
	}

	var decoded struct {
		Data       json.RawMessage  `json:"data"`
		Errors     []map[string]any `json:"errors"`
		Extensions json.RawMessage  `json:"extensions"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return resp, &Error{
			Kind:       KindTransport,
			StatusCode: status,
			Message:    "parse GraphQL response: " + redact.String(err.Error()),
			Response:   resp,
			Context:    map[string]any{"status": status},
			cause:      err,
		}
	}

	resp.Data = decoded.Data
	resp.Extensions = decoded.Extensions
	for _, raw := range decoded.Errors {
		rawJSON, _ := json.Marshal(raw)
		ge := GraphQLError{Raw: rawJSON}
		if m, ok := raw["message"].(string); ok {
			ge.Message = m
		} else {
			ge.Message = "unknown GraphQL error"
		}
		if loc, ok := raw["locations"].([]any); ok {
			for _, l := range loc {
				if lm, ok := l.(map[string]any); ok {
					ge.Locations = append(ge.Locations, lm)
				}
			}
		}
		if p, ok := raw["path"].([]any); ok {
			ge.Path = p
		}
		if ext, ok := raw["extensions"].(map[string]any); ok {
			ge.Extensions = ext
		}
		resp.Errors = append(resp.Errors, ge)
	}
	return resp, nil
}

func classifyHTTPError(status int, body []byte, retryAfter time.Duration, sharedCompute bool) *Error {
	sanitizedBody := redact.String(truncate(string(body), 512))

	switch {
	case status == http.StatusUnauthorized:
		return &Error{Kind: KindAuthentication, StatusCode: status, Message: "authentication failed (401) — check or refresh the access token", Context: map[string]any{"status": status}}
	case status == http.StatusForbidden:
		return &Error{Kind: KindAuthorization, StatusCode: status, Message: "authorization failed (403)", Context: map[string]any{"status": status}}
	case status == http.StatusPaymentRequired:
		return &Error{Kind: KindPlanEntitlement, StatusCode: status, Message: "plan does not cover this request (402)", Context: map[string]any{"status": status}}
	case status == http.StatusTooManyRequests || sharedCompute:
		return &Error{Kind: KindRateLimited, StatusCode: status, Message: "rate limited: " + firstLine(sanitizedBody), RetryAfter: retryAfter, Temporary: true, Context: map[string]any{"status": status}}
	case status >= 500:
		return &Error{Kind: KindServer, StatusCode: status, Message: "server error: " + firstLine(sanitizedBody), Temporary: status == 500 || status == 502 || status == 503 || status == 504, Context: map[string]any{"status": status}}
	default:
		return &Error{Kind: KindTransport, StatusCode: status, Message: "unexpected HTTP status: " + firstLine(sanitizedBody), Context: map[string]any{"status": status}}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// IsRetryable reports whether err is a typed transient error a caller
// could safely retry beyond the built-in policy.
func IsRetryable(err error) bool {
	var be *Error
	if errors.As(err, &be) {
		return be.Temporary
	}
	return false
}

// isRetryableRead conservatively identifies an HTTP GraphQL query that
// can be retried by the SDK. Mutations and subscriptions are never
// replayed automatically: callers must decide whether their operation is
// safe to repeat.
func (op Operation) isRetryableRead() bool {
	doc := strings.TrimLeftFunc(op.Query, func(r rune) bool { return r == '\ufeff' || r == ',' || r == ' ' || r == '\t' || r == '\r' || r == '\n' })
	for strings.HasPrefix(doc, "#") {
		if newline := strings.IndexByte(doc, '\n'); newline >= 0 {
			doc = strings.TrimSpace(doc[newline+1:])
		} else {
			return false
		}
	}
	if strings.HasPrefix(doc, "{") {
		return true
	}
	if strings.HasPrefix(doc, "query") {
		return len(doc) == len("query") || isGraphQLDelimiter(doc[len("query")])
	}
	return false
}

func isGraphQLDelimiter(b byte) bool {
	switch b {
	case ' ', '\t', '\r', '\n', '(', '{', '@':
		return true
	default:
		return false
	}
}
