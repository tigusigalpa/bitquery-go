package bitquery

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStaticTokenProvider(t *testing.T) {
	p := NewStaticTokenProvider("TOK")
	if tok, _ := p.Token(context.Background()); tok != "TOK" {
		t.Fatalf("token = %q", tok)
	}
	if tok, _ := p.Refresh(context.Background()); tok != "TOK" {
		t.Fatalf("refresh = %q", tok)
	}
}

func TestStaticTokenProviderRejectsEmptyToken(t *testing.T) {
	_, err := NewStaticTokenProvider(" \t").Token(context.Background())
	if err == nil {
		t.Fatal("expected empty-token error")
	}
	var be *Error
	if !errors.As(err, &be) || be.Kind != KindConfig {
		t.Fatalf("kind = %v, want config", err)
	}
}

func TestClientCredentialsFlow(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", ct)
		}
		body, _ := io.ReadAll(r.Body)
		form := string(body)
		for _, want := range []string{
			"grant_type=client_credentials",
			"client_id=TEST_ID",
			"client_secret=TEST_SECRET",
			"scope=api",
		} {
			if !strings.Contains(form, want) {
				t.Errorf("form missing %q — got %q", want, form)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"MINTED","expires_in":3600}`))
	}))
	defer srv.Close()

	p := newClientCredentialsProvider("TEST_ID", "TEST_SECRET", srv.URL)

	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "MINTED" {
		t.Fatalf("token = %q", tok)
	}

	// Second call uses the cache — no new OAuth request.
	if _, err := p.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("oauth calls = %d, want 1 (cached)", calls.Load())
	}

	// Refresh forces a new request.
	tok2, err := p.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok2 != "MINTED" || calls.Load() != 2 {
		t.Fatalf("refresh: tok=%q calls=%d", tok2, calls.Load())
	}
}

func TestClientCredentialsRejectsBadResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client","detail":"client_secret=TEST_SECRET"}`))
	}))
	defer srv.Close()

	p := newClientCredentialsProvider("TEST_ID", "TEST_SECRET", srv.URL)
	_, err := p.Token(context.Background())
	if err == nil {
		t.Fatal("expected auth error")
	}
	var be *Error
	if !errors.As(err, &be) || be.Kind != KindAuthentication {
		t.Fatalf("kind = %v", err)
	}
	// The OAuth error body must never echo the client_secret back.
	if strings.Contains(err.Error(), "TEST_SECRET") {
		t.Fatalf("secret leaked in error: %s", err.Error())
	}
}

func TestClientCredentialsRejectsNon2xxEvenWithTokenPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"access_token":"MUST_NOT_USE","expires_in":3600}`))
	}))
	defer srv.Close()

	p := newClientCredentialsProvider("TEST_ID", "TEST_SECRET", srv.URL)
	_, err := p.Token(context.Background())
	if err == nil {
		t.Fatal("expected error for non-2xx OAuth response")
	}
	if strings.Contains(err.Error(), "MUST_NOT_USE") {
		t.Fatalf("token leaked into error: %v", err)
	}
}

func TestClientCredentialsCachesShortLivedToken(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"access_token":"SHORT","expires_in":10}`))
	}))
	defer srv.Close()

	p := newClientCredentialsProvider("TEST_ID", "TEST_SECRET", srv.URL)
	now := time.Now()
	p.now = func() time.Time { return now }
	if _, err := p.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("OAuth calls = %d, want cached token", calls.Load())
	}
	if !p.expiresAt.Equal(now.Add(9 * time.Second)) {
		t.Fatalf("expiresAt = %v, want 90%% of short lifetime", p.expiresAt)
	}
}

func TestConfigTokenEndpointAppliesToDefaultOAuthProvider(t *testing.T) {
	provider := NewClientCredentialsProvider("TEST_ID", "TEST_SECRET")
	_, err := NewConfig(
		WithTokenProvider(provider),
		WithTokenEndpoint("https://oauth.example.test/token"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if provider.tokenURL != "https://oauth.example.test/token" {
		t.Fatalf("token endpoint = %q", provider.tokenURL)
	}

	explicit := NewClientCredentialsProvider("TEST_ID", "TEST_SECRET", WithOAuthTokenEndpoint("https://explicit.example.test/token"))
	_, err = NewConfig(
		WithTokenProvider(explicit),
		WithTokenEndpoint("https://config.example.test/token"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if explicit.tokenURL != "https://explicit.example.test/token" {
		t.Fatalf("explicit token endpoint was overwritten: %q", explicit.tokenURL)
	}
}
