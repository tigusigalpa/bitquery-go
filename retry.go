package bitquery

import (
	"context"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RetryPolicy controls retry behaviour. Defaults follow official
// guidance: ~5s initial delay, exponential doubling, ~60s cap, jitter.
// Sleep and Rand are injectable for deterministic tests.
//
// https://docs.bitquery.io/docs/plans/rate-limits/
type RetryPolicy struct {
	// MaxAttempts is the total number of tries including the first. 1 disables retries.
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	// Jitter is a 0.0–1.0 randomisation factor applied to each delay.
	Jitter float64
	// RetryableStatuses — HTTP statuses allowed to retry.
	RetryableStatuses []int
	// Sleep sleeps d or returns early on ctx cancellation. Defaults to a
	// context-aware real sleep.
	Sleep func(ctx context.Context, d time.Duration) error
	// Rand is the jitter source (time-seeded by default).
	Rand *rand.Rand

	randMu sync.Mutex
}

// DefaultRetryPolicy returns the documented default policy.
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:       4,
		BaseDelay:         5 * time.Second,
		MaxDelay:          60 * time.Second,
		Jitter:            0.25,
		RetryableStatuses: []int{http.StatusTooManyRequests, 500, 502, 503, 504},
		Sleep:             sleepCtx,
		Rand:              rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// NoRetry returns a policy that never retries.
func NoRetry() *RetryPolicy {
	p := DefaultRetryPolicy()
	p.MaxAttempts = 1
	return p
}

func (p *RetryPolicy) retryableStatus(status int) bool {
	for _, s := range p.RetryableStatuses {
		if s == status {
			return true
		}
	}
	return false
}

// Delay computes the backoff before retry attempt n (1-based count of
// attempts already made). A server Retry-After hint wins and is capped.
func (p *RetryPolicy) Delay(attempt int, retryAfter time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if retryAfter > 0 {
		if retryAfter > p.MaxDelay {
			return p.MaxDelay
		}
		return retryAfter
	}

	d := p.BaseDelay << (attempt - 1)
	if d > p.MaxDelay {
		d = p.MaxDelay
	}

	if p.Jitter > 0 && p.Rand != nil {
		spread := int64(float64(d) * p.Jitter)
		if spread > 0 {
			p.randMu.Lock()
			d += time.Duration(p.Rand.Int63n(2*spread+1) - spread)
			p.randMu.Unlock()
		}
	}
	if d < 0 {
		return 0
	}
	return d
}

// parseRetryAfter parses a Retry-After header (seconds or HTTP date).
func parseRetryAfter(h string, now time.Time) time.Duration {
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
