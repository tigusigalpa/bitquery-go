package bitquery

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Config holds client configuration. Construct via NewConfig with
// functional options; explicit URL overrides always win over region.
type Config struct {
	Region        Region
	BaseURL       string // explicit HTTPS endpoint override
	WebSocketURL  string // explicit WSS endpoint override
	TokenEndpoint string

	HTTPClient *http.Client
	Timeout    time.Duration

	TokenProvider TokenProvider
	Retry         *RetryPolicy
	RateLimiter   RateLimiter
	Logger        Logger
	UserAgent     string

	// Strict makes Execute fail on any GraphQL errors[] — partial data is
	// still preserved on the returned Response inside *Error.
	Strict bool

	// --- V2 subscription options ---
	SubProtocol               SubProtocol
	SubscriptionMaxReconnects int
	SubscriptionQueueCapacity int
	// OverflowPolicy: "drop_oldest" (default) or "fail".
	OverflowPolicy string
	// Dialer overrides the WebSocket dial implementation (tests/custom transport).
	Dialer Dialer
}

// Option mutates Config.
type Option func(*Config)

// NewConfig builds a Config with defaults.
func NewConfig(opts ...Option) (*Config, error) {
	c := &Config{
		Region:                    RegionEurope,
		TokenEndpoint:             OAuthTokenEndpoint,
		Timeout:                   30 * time.Second,
		Retry:                     DefaultRetryPolicy(),
		UserAgent:                 "bitquery-go/0.1 (+https://github.com/tigusigalpa/bitquery-go)",
		SubProtocol:               SubProtocolGraphQLTransportWS,
		SubscriptionMaxReconnects: 8,
		SubscriptionQueueCapacity: 1000,
		OverflowPolicy:            OverflowDropOldest,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	if c.TokenProvider == nil {
		return nil, &Error{Kind: KindConfig, Message: "a TokenProvider is required (WithTokenProvider)"}
	}
	if c.HTTPClient == nil && c.Timeout <= 0 {
		return nil, &Error{Kind: KindConfig, Message: "timeout must be greater than zero when no HTTP client is supplied"}
	}
	if _, err := HTTPURL(V2, c.Region, c.BaseURL); err != nil {
		return nil, err
	}
	if strings.TrimSpace(c.WebSocketURL) != "" {
		if _, err := WebSocketURL(c.Region, c.BaseURL, c.WebSocketURL); err != nil {
			return nil, err
		}
	}
	if err := validateHTTPURL(c.TokenEndpoint, "OAuth token endpoint"); err != nil {
		return nil, err
	}
	if provider, ok := c.TokenProvider.(*ClientCredentialsProvider); ok && !provider.endpointExplicit {
		provider.tokenURL = c.TokenEndpoint
	}
	if c.Retry != nil {
		if err := validateRetryPolicy(c.Retry); err != nil {
			return nil, err
		}
	}
	if c.SubscriptionMaxReconnects < 0 {
		return nil, &Error{Kind: KindConfig, Message: "subscription reconnect count cannot be negative"}
	}
	if c.SubscriptionQueueCapacity < 0 {
		return nil, &Error{Kind: KindConfig, Message: "subscription queue capacity cannot be negative"}
	}
	if c.OverflowPolicy != OverflowDropOldest && c.OverflowPolicy != OverflowFail {
		return nil, &Error{Kind: KindConfig, Message: fmt.Sprintf("unknown subscription overflow policy %q", c.OverflowPolicy)}
	}
	if c.SubProtocol != SubProtocolGraphQLWS && c.SubProtocol != SubProtocolGraphQLTransportWS {
		return nil, &Error{Kind: KindConfig, Message: fmt.Sprintf("unknown GraphQL WebSocket subprotocol %q", c.SubProtocol)}
	}
	return c, nil
}

// WithRegion selects the endpoint region (europe/asia/us).
func WithRegion(r Region) Option {
	return func(c *Config) { c.Region = r }
}

// WithBaseURL sets an explicit HTTPS endpoint override — wins over region.
func WithBaseURL(u string) Option {
	return func(c *Config) { c.BaseURL = u }
}

// WithWebSocketURL sets an explicit WSS endpoint override for subscriptions.
func WithWebSocketURL(u string) Option {
	return func(c *Config) { c.WebSocketURL = u }
}

// WithTokenEndpoint overrides the OAuth token endpoint.
func WithTokenEndpoint(u string) Option {
	return func(c *Config) { c.TokenEndpoint = u }
}

// WithHTTPClient supplies a caller-owned http.Client. The SDK will not
// mutate it; set your own timeout/transport there if needed.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Config) { c.HTTPClient = hc }
}

// WithTimeout sets the per-request timeout when no custom client is given.
func WithTimeout(d time.Duration) Option {
	return func(c *Config) { c.Timeout = d }
}

// WithTokenProvider supplies the OAuth token source (required).
func WithTokenProvider(tp TokenProvider) Option {
	return func(c *Config) { c.TokenProvider = tp }
}

// WithRetryPolicy overrides the retry policy (nil disables retries).
func WithRetryPolicy(p *RetryPolicy) Option {
	return func(c *Config) { c.Retry = p }
}

// WithRateLimiter installs client-side request pacing.
func WithRateLimiter(l RateLimiter) Option {
	return func(c *Config) { c.RateLimiter = l }
}

// WithLogger installs a structured logger (default no-op). Output is
// redacted before logging.
func WithLogger(l Logger) Option {
	return func(c *Config) { c.Logger = l }
}

// WithUserAgent sets the HTTP User-Agent.
func WithUserAgent(ua string) Option {
	return func(c *Config) { c.UserAgent = ua }
}

// WithStrict enables strict mode: any errors[] fails the call.
func WithStrict() Option {
	return func(c *Config) { c.Strict = true }
}

// WithSubProtocol selects graphql-ws or graphql-transport-ws.
func WithSubProtocol(p SubProtocol) Option {
	return func(c *Config) { c.SubProtocol = p }
}

// WithSubscriptionReconnect sets max reconnect attempts.
func WithSubscriptionReconnect(n int) Option {
	return func(c *Config) { c.SubscriptionMaxReconnects = n }
}

// WithSubscriptionQueue sets the bounded event-buffer capacity and
// overflow policy (OverflowDropOldest or OverflowFail).
func WithSubscriptionQueue(capacity int, policy string) Option {
	return func(c *Config) {
		c.SubscriptionQueueCapacity = capacity
		if policy != "" {
			c.OverflowPolicy = policy
		}
	}
}

// WithDialer overrides the WebSocket dialer (tests/custom transport).
func WithDialer(d Dialer) Option {
	return func(c *Config) { c.Dialer = d }
}

func validateRetryPolicy(p *RetryPolicy) error {
	if p.MaxAttempts < 1 {
		return &Error{Kind: KindConfig, Message: "retry policy MaxAttempts must be at least 1"}
	}
	if p.BaseDelay < 0 || p.MaxDelay < 0 || p.MaxDelay < p.BaseDelay {
		return &Error{Kind: KindConfig, Message: "retry policy delays must be non-negative and MaxDelay must be at least BaseDelay"}
	}
	if p.Jitter < 0 || p.Jitter > 1 {
		return &Error{Kind: KindConfig, Message: "retry policy Jitter must be between 0 and 1"}
	}
	return nil
}

// Overflow policies for the subscription bounded queue.
const (
	OverflowDropOldest = "drop_oldest"
	OverflowFail       = "fail"
)

// SubProtocol selects the GraphQL-over-WebSocket subprotocol.
type SubProtocol string

const (
	// SubProtocolGraphQLWS is the legacy Apollo protocol
	// (start/data/ka/stop frames).
	SubProtocolGraphQLWS SubProtocol = "graphql-ws"
	// SubProtocolGraphQLTransportWS is the graphql-ws library protocol
	// (subscribe/next/ping-pong/complete frames).
	SubProtocolGraphQLTransportWS SubProtocol = "graphql-transport-ws"
)

// SubscribeType is the frame type used to start a subscription.
func (p SubProtocol) SubscribeType() string {
	if p == SubProtocolGraphQLWS {
		return "start"
	}
	return "subscribe"
}

// DataType is the frame type carrying a result payload.
func (p SubProtocol) DataType() string {
	if p == SubProtocolGraphQLWS {
		return "data"
	}
	return "next"
}

// KeepaliveType is the server's keepalive frame type.
func (p SubProtocol) KeepaliveType() string {
	if p == SubProtocolGraphQLWS {
		return "ka"
	}
	return "pong"
}
