package bitquery

import (
	"errors"
	"math/rand"
	"net/http"
	"testing"
	"time"
)

func TestRetryDelayGrowth(t *testing.T) {
	p := &RetryPolicy{
		BaseDelay: 5 * time.Second,
		MaxDelay:  60 * time.Second,
		Jitter:    0,
	}
	if d := p.Delay(1, 0); d != 5*time.Second {
		t.Fatalf("attempt1 = %v", d)
	}
	if d := p.Delay(2, 0); d != 10*time.Second {
		t.Fatalf("attempt2 = %v", d)
	}
	if d := p.Delay(10, 0); d != 60*time.Second {
		t.Fatalf("cap = %v", d)
	}
}

func TestRetryDelayHonoursRetryAfter(t *testing.T) {
	p := &RetryPolicy{BaseDelay: time.Second, MaxDelay: 10 * time.Second}
	if d := p.Delay(1, 3*time.Second); d != 3*time.Second {
		t.Fatalf("retryAfter = %v", d)
	}
	if d := p.Delay(1, time.Hour); d != 10*time.Second {
		t.Fatalf("retryAfter cap = %v", d)
	}
}

func TestRetryDelayJitterBounded(t *testing.T) {
	p := &RetryPolicy{
		BaseDelay: 5 * time.Second,
		MaxDelay:  60 * time.Second,
		Jitter:    0.25,
		Rand:      rand.New(rand.NewSource(1)),
	}
	for i := 0; i < 100; i++ {
		d := p.Delay(1, 0)
		lo := 5*time.Second - time.Duration(0.25*float64(5*time.Second))
		hi := 5*time.Second + time.Duration(0.25*float64(5*time.Second))
		if d < lo || d > hi {
			t.Fatalf("jittered delay %v outside [%v,%v]", d, lo, hi)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d := parseRetryAfter("7", time.Now()); d != 7*time.Second {
		t.Fatalf("seconds = %v", d)
	}
	if d := parseRetryAfter("", time.Now()); d != 0 {
		t.Fatalf("empty = %v", d)
	}
	if d := parseRetryAfter("-3", time.Now()); d != 0 {
		t.Fatalf("negative = %v", d)
	}
	future := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	if d := parseRetryAfter(future, time.Now()); d <= 0 || d > 90*time.Second {
		t.Fatalf("http-date = %v", d)
	}
}

func TestParseRetryAfterUsesInjectedClock(t *testing.T) {
	now := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	header := now.Add(10 * time.Second).Format(http.TimeFormat)
	if got := parseRetryAfter(header, now); got != 10*time.Second {
		t.Fatalf("http-date delay = %v, want 10s", got)
	}
}

func TestRetryableStatus(t *testing.T) {
	p := DefaultRetryPolicy()
	for _, s := range []int{429, 500, 502, 503, 504} {
		if !p.retryableStatus(s) {
			t.Fatalf("%d must be retryable", s)
		}
	}
	for _, s := range []int{400, 401, 402, 403, 404} {
		if p.retryableStatus(s) {
			t.Fatalf("%d must NOT be retried (plan/auth errors are not transient)", s)
		}
	}
}

func TestIsRetryable(t *testing.T) {
	if IsRetryable(errors.New("plain")) {
		t.Fatal("plain error not retryable")
	}
	if !IsRetryable(&Error{Kind: KindRateLimited, Temporary: true}) {
		t.Fatal("429 must be retryable")
	}
	if IsRetryable(&Error{Kind: KindPlanEntitlement}) {
		t.Fatal("402 must not be retryable")
	}
}

func TestDefaultRetryPolicyAndNoRetry(t *testing.T) {
	p := DefaultRetryPolicy()
	if p.MaxAttempts != 4 || p.Sleep == nil || p.Rand == nil {
		t.Fatalf("unexpected default policy: %+v", p)
	}
	if d := p.Delay(1, 0); d < 3750*time.Millisecond || d > 6250*time.Millisecond {
		t.Fatalf("default jittered delay = %v, want within 25%% of 5s", d)
	}
	if noRetry := NoRetry(); noRetry.MaxAttempts != 1 {
		t.Fatalf("NoRetry MaxAttempts = %d", noRetry.MaxAttempts)
	}
}
