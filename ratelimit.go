package bitquery

import (
	"context"
	"sync"
	"time"
)

// RateLimiter paces outgoing requests. Wait is called once per HTTP
// request and must respect context cancellation.
type RateLimiter interface {
	Wait(ctx context.Context) error
}

// TokenBucketRateLimiter is an in-process limiter for N requests per
// minute. Clock and sleep are injectable for tests.
type TokenBucketRateLimiter struct {
	perMinute float64

	mu      sync.Mutex
	tokens  float64
	lastRef time.Time

	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

// NewRateLimiter returns a token-bucket limiter allowing rpm requests
// per minute. rpm <= 0 disables limiting; the returned limiter is safe
// to pass to WithRateLimiter and behaves as a no-op.
func NewRateLimiter(rpm int) *TokenBucketRateLimiter {
	return &TokenBucketRateLimiter{
		perMinute: float64(rpm),
		tokens:    float64(rpm),
		lastRef:   time.Now(),
		now:       time.Now,
		sleep:     sleepCtx,
	}
}

// Wait blocks until a token is available or ctx is cancelled.
func (l *TokenBucketRateLimiter) Wait(ctx context.Context) error {
	if l == nil || l.perMinute <= 0 {
		return nil
	}
	for {
		l.mu.Lock()
		l.refillLocked()
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}
		missing := time.Duration((1 - l.tokens) * float64(l.perToken()))
		l.mu.Unlock()

		if err := l.sleep(ctx, missing); err != nil {
			return err
		}
	}
}

func (l *TokenBucketRateLimiter) refillLocked() {
	now := l.now()
	elapsed := now.Sub(l.lastRef)
	if elapsed <= 0 {
		return
	}
	l.tokens += float64(elapsed) / float64(l.perToken())
	if l.tokens > l.perMinute {
		l.tokens = l.perMinute
	}
	l.lastRef = now
}

func (l *TokenBucketRateLimiter) perToken() time.Duration {
	return time.Duration(float64(time.Minute) / l.perMinute)
}

// NopRateLimiter never delays.
type NopRateLimiter struct{}

// Wait immediately succeeds without delaying the caller.
func (NopRateLimiter) Wait(context.Context) error { return nil }
