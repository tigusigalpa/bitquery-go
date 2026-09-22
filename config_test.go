package bitquery

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestNewConfigAppliesOptions(t *testing.T) {
	httpClient := &http.Client{Timeout: time.Second}
	retry := NoRetry()
	rateLimiter := NewRateLimiter(60)
	logger := NopLogger{}
	dialer := func(context.Context, string, []string) (WSConn, error) { return nil, nil }

	config, err := NewConfig(
		WithRegion(RegionAsia),
		WithBaseURL("https://api.example.test/graphql"),
		WithWebSocketURL("wss://stream.example.test/graphql"),
		WithTokenEndpoint("https://oauth.example.test/token"),
		WithHTTPClient(httpClient),
		WithTimeout(12*time.Second),
		WithTokenProvider(NewStaticTokenProvider("token")),
		WithRetryPolicy(retry),
		WithRateLimiter(rateLimiter),
		WithLogger(logger),
		WithUserAgent("integration-test/1.0"),
		WithStrict(),
		WithSubProtocol(SubProtocolGraphQLWS),
		WithSubscriptionReconnect(3),
		WithSubscriptionQueue(7, OverflowFail),
		WithDialer(dialer),
	)
	if err != nil {
		t.Fatal(err)
	}

	if config.Region != RegionAsia || config.BaseURL != "https://api.example.test/graphql" || config.WebSocketURL != "wss://stream.example.test/graphql" {
		t.Fatalf("endpoint configuration = %+v", config)
	}
	if config.TokenEndpoint != "https://oauth.example.test/token" || config.HTTPClient != httpClient || config.Timeout != 12*time.Second {
		t.Fatalf("transport configuration = %+v", config)
	}
	if config.TokenProvider == nil || config.Retry != retry || config.RateLimiter != rateLimiter || config.Logger != logger {
		t.Fatalf("dependencies were not retained: %+v", config)
	}
	if config.UserAgent != "integration-test/1.0" || !config.Strict || config.SubProtocol != SubProtocolGraphQLWS {
		t.Fatalf("request configuration = %+v", config)
	}
	if config.SubscriptionMaxReconnects != 3 || config.SubscriptionQueueCapacity != 7 || config.OverflowPolicy != OverflowFail || config.Dialer == nil {
		t.Fatalf("subscription configuration = %+v", config)
	}
}

func TestNewConfigRejectsInvalidValues(t *testing.T) {
	provider := WithTokenProvider(NewStaticTokenProvider("token"))
	tests := []struct {
		name string
		opts []Option
	}{
		{name: "missing token provider"},
		{name: "zero timeout", opts: []Option{provider, WithTimeout(0)}},
		{name: "invalid base URL", opts: []Option{provider, WithBaseURL("ftp://example.test/graphql")}},
		{name: "invalid WebSocket URL", opts: []Option{provider, WithWebSocketURL("https://example.test/graphql")}},
		{name: "invalid OAuth URL", opts: []Option{provider, WithTokenEndpoint("ftp://example.test/token")}},
		{name: "no retry attempts", opts: []Option{provider, WithRetryPolicy(&RetryPolicy{MaxAttempts: 0})}},
		{name: "negative retry delay", opts: []Option{provider, WithRetryPolicy(&RetryPolicy{MaxAttempts: 1, BaseDelay: -time.Second})}},
		{name: "invalid retry jitter", opts: []Option{provider, WithRetryPolicy(&RetryPolicy{MaxAttempts: 1, Jitter: 2})}},
		{name: "negative reconnects", opts: []Option{provider, WithSubscriptionReconnect(-1)}},
		{name: "negative queue", opts: []Option{provider, WithSubscriptionQueue(-1, OverflowDropOldest)}},
		{name: "unknown overflow policy", opts: []Option{provider, WithSubscriptionQueue(1, "discard")}},
		{name: "unknown subprotocol", opts: []Option{provider, WithSubProtocol("not-graphql")}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewConfig(test.opts...)
			if err == nil {
				t.Fatal("expected configuration error")
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestSubProtocolFrameTypes(t *testing.T) {
	tests := []struct {
		protocol              SubProtocol
		subscribe, data, ping string
	}{
		{SubProtocolGraphQLWS, "start", "data", "ka"},
		{SubProtocolGraphQLTransportWS, "subscribe", "next", "pong"},
	}
	for _, test := range tests {
		if got := test.protocol.SubscribeType(); got != test.subscribe {
			t.Errorf("%s SubscribeType = %q, want %q", test.protocol, got, test.subscribe)
		}
		if got := test.protocol.DataType(); got != test.data {
			t.Errorf("%s DataType = %q, want %q", test.protocol, got, test.data)
		}
		if got := test.protocol.KeepaliveType(); got != test.ping {
			t.Errorf("%s KeepaliveType = %q, want %q", test.protocol, got, test.ping)
		}
	}
}
