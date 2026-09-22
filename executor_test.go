package bitquery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// noSleep is an instant retry sleeper for deterministic tests.
func noSleep(context.Context, time.Duration) error { return nil }

func fastPolicy() *RetryPolicy {
	p := DefaultRetryPolicy()
	p.Sleep = noSleep
	p.BaseDelay = time.Millisecond
	p.MaxDelay = 10 * time.Millisecond
	p.Jitter = 0
	return p
}

func newTestExecutor(t *testing.T, version ApiVersion, url string, extra ...Option) *Executor {
	t.Helper()
	opts := append([]Option{
		WithTokenProvider(NewStaticTokenProvider("TEST_TOKEN")),
		WithBaseURL(url),
		WithRetryPolicy(fastPolicy()),
	}, extra...)
	ex, err := NewExecutor(version, opts...)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	return ex
}

func TestExecuteSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer TEST_TOKEN" {
			t.Errorf("Authorization = %q", got)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["query"] == nil {
			t.Error("missing query in request body")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"ethereum":{"blocks":42}}}`))
	}))
	defer srv.Close()

	ex := newTestExecutor(t, V2, srv.URL)
	resp, err := ex.Execute(context.Background(), Operation{Query: "query { ethereum { blocks } }"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != 200 || resp.HasErrors() {
		t.Fatalf("unexpected resp: %+v", resp)
	}
	var data struct {
		Ethereum struct {
			Blocks int `json:"blocks"`
		} `json:"ethereum"`
	}
	if err := resp.DecodeData(&data); err != nil {
		t.Fatal(err)
	}
	if data.Ethereum.Blocks != 42 {
		t.Fatalf("blocks = %d", data.Ethereum.Blocks)
	}
}

func TestExecute200WithGraphQLErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":null,"errors":[{"message":"bad field","path":["foo"],"locations":[{"line":1,"column":2}]}]}`))
	}))
	defer srv.Close()

	ex := newTestExecutor(t, V1, srv.URL)

	// Tolerant mode: errors[] surfaced on Response, not a Go error.
	resp, err := ex.Execute(context.Background(), Operation{Query: "{ foo }"})
	if err != nil {
		t.Fatalf("tolerant Execute returned err: %v", err)
	}
	if !resp.HasErrors() {
		t.Fatal("expected HasErrors")
	}
	if resp.Errors[0].Message != "bad field" {
		t.Fatalf("message = %q", resp.Errors[0].Message)
	}

	// Strict mode: errors[] becomes a typed KindGraphQL error.
	resp, err = ex.ExecuteStrict(context.Background(), Operation{Query: "{ foo }"})
	if err == nil {
		t.Fatal("strict Execute expected error")
	}
	var be *Error
	if !errors.As(err, &be) || be.Kind != KindGraphQL {
		t.Fatalf("expected KindGraphQL, got %v", err)
	}
	if !errors.Is(err, ErrGraphQL) {
		t.Fatal("errors.Is(ErrGraphQL) failed")
	}
	if resp == nil {
		t.Fatal("response must be preserved on strict error")
	}
}

func TestExecutePartialData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"a":1},"errors":[{"message":"partial"}]}`))
	}))
	defer srv.Close()

	ex := newTestExecutor(t, V2, srv.URL)
	resp, err := ex.Execute(context.Background(), Operation{Query: "{ a }"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !resp.HasPartialData() {
		t.Fatal("expected partial data (data + errors)")
	}
}

func TestExecuteInvalidJSONResponseIsTypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not JSON"))
	}))
	defer srv.Close()

	ex := newTestExecutor(t, V2, srv.URL)
	resp, err := ex.Execute(context.Background(), Operation{Query: "{ a }"})
	if err == nil {
		t.Fatal("expected response parse error")
	}
	var be *Error
	if !errors.As(err, &be) || be.Kind != KindTransport {
		t.Fatalf("kind = %v, want transport", err)
	}
	if resp == nil || string(resp.RawBody) != "not JSON" || be.Response != resp {
		t.Fatalf("raw response was not retained: resp=%+v error=%+v", resp, be)
	}
}

func TestExecute429RetriesThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer srv.Close()

	ex := newTestExecutor(t, V2, srv.URL)
	resp, err := ex.Execute(context.Background(), Operation{Query: "{ ok }"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestExecute429ExhaustsRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	ex := newTestExecutor(t, V2, srv.URL)
	_, err := ex.Execute(context.Background(), Operation{Query: "{ x }"})
	if err == nil {
		t.Fatal("expected rate-limit error")
	}
	var be *Error
	if !errors.As(err, &be) {
		t.Fatal("expected *Error")
	}
	if be.Kind != KindRateLimited || !errors.Is(err, ErrRateLimited) {
		t.Fatalf("kind = %v", be.Kind)
	}
	if be.RetryAfter != 5*time.Second {
		t.Fatalf("RetryAfter = %v", be.RetryAfter)
	}
	if !be.Temporary || !IsRetryable(err) {
		t.Fatal("429 must be marked temporary/retryable")
	}
	if calls.Load() != int32(fastPolicy().MaxAttempts) {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestExecuteSharedComputeIsRetryable(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest) // 400 but documented transient
			_, _ = w.Write([]byte(`You are temporarily blocked due to exceeding shared compute`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"ok":1}}`))
	}))
	defer srv.Close()

	ex := newTestExecutor(t, V2, srv.URL)
	if _, err := ex.Execute(context.Background(), Operation{Query: "{ ok }"}); err != nil {
		t.Fatalf("shared-compute must be retried: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestExecuteMutationIsNeverRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ex := newTestExecutor(t, V2, srv.URL)
	_, err := ex.Execute(context.Background(), Operation{Query: "mutation { submitSomething }"})
	if err == nil {
		t.Fatal("expected server error")
	}
	if calls.Load() != 1 {
		t.Fatalf("mutation was replayed %d times; mutations must not be retried", calls.Load())
	}
}

func TestExecuteCommentPrefixedQueryRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer srv.Close()

	ex := newTestExecutor(t, V2, srv.URL)
	if _, err := ex.Execute(context.Background(), Operation{Query: "# safe read\nquery { ok }"}); err != nil {
		t.Fatalf("comment-prefixed query should retry: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestExecute401RefreshesTokenOnce(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		tok := r.Header.Get("Authorization")
		if n == 1 {
			if tok != "Bearer STALE" {
				t.Errorf("first call token = %q", tok)
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if tok != "Bearer FRESH" {
			t.Errorf("retry token = %q", tok)
		}
		_, _ = w.Write([]byte(`{"data":{"ok":1}}`))
	}))
	defer srv.Close()

	provider := &refreshStub{token: "STALE"}
	ex, err := NewExecutor(V2,
		WithTokenProvider(provider),
		WithBaseURL(srv.URL),
		WithRetryPolicy(fastPolicy()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ex.Execute(context.Background(), Operation{Query: "{ ok }"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if provider.refreshes != 1 {
		t.Fatalf("refreshes = %d", provider.refreshes)
	}
}

type refreshStub struct {
	token     string
	refreshes int
}

func (s *refreshStub) Token(context.Context) (string, error) { return s.token, nil }
func (s *refreshStub) Refresh(context.Context) (string, error) {
	s.refreshes++
	s.token = "FRESH"
	return s.token, nil
}

func TestExecuteErrorClassification(t *testing.T) {
	cases := []struct {
		status int
		kind   Kind
	}{
		{http.StatusForbidden, KindAuthorization},
		{http.StatusPaymentRequired, KindPlanEntitlement},
		{http.StatusInternalServerError, KindServer},
		{http.StatusBadRequest, KindTransport},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
		}))
		ex := newTestExecutor(t, V2, srv.URL)
		_, err := ex.Execute(context.Background(), Operation{Query: "{ x }"})
		srv.Close()
		if err == nil {
			t.Fatalf("status %d: expected error", tc.status)
		}
		var be *Error
		if !errors.As(err, &be) || be.Kind != tc.kind {
			t.Fatalf("status %d: kind = %v, want %v", tc.status, be.Kind, tc.kind)
		}
		if be.StatusCode != tc.status {
			t.Fatalf("status %d: StatusCode = %d", tc.status, be.StatusCode)
		}
	}
}

func TestExecuteEmptyQuery(t *testing.T) {
	ex := newTestExecutor(t, V2, "http://localhost:1")
	_, err := ex.Execute(context.Background(), Operation{Query: "   "})
	if err == nil {
		t.Fatal("expected config error")
	}
	var be *Error
	if !errors.As(err, &be) || be.Kind != KindConfig {
		t.Fatalf("kind = %v", err)
	}
}

func TestExecuteRequiresVersion(t *testing.T) {
	_, err := NewExecutor(ApiVersion(9), WithTokenProvider(NewStaticTokenProvider("x")))
	if err == nil {
		t.Fatal("expected version error")
	}
}

func TestPrecisionBigInt(t *testing.T) {
	big := "9007199254740993" // 2^53 + 1 — loses precision as float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"amount":` + big + `}}`))
	}))
	defer srv.Close()

	ex := newTestExecutor(t, V2, srv.URL)
	resp, err := ex.Execute(context.Background(), Operation{Query: "{ amount }"})
	if err != nil {
		t.Fatal(err)
	}

	var data map[string]any
	if err := resp.DecodeData(&data); err != nil {
		t.Fatal(err)
	}
	num, ok := data["amount"].(json.Number)
	if !ok {
		t.Fatalf("amount decoded as %T — precision lost", data["amount"])
	}
	if num.String() != big {
		t.Fatalf("amount = %s, want %s", num, big)
	}

	// RawBody keeps the exact source bytes as well.
	if !strings.Contains(string(resp.RawBody), big) {
		t.Fatal("raw body lost the big integer")
	}
}
