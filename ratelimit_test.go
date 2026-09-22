package bitquery

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRateLimiterAllowsBurst(t *testing.T) {
	l := NewRateLimiter(3)
	for i := 0; i < 3; i++ {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatalf("wait %d: %v", i, err)
		}
	}
}

func TestRateLimiterWaitsWithFakeClock(t *testing.T) {
	l := NewRateLimiter(2) // 30s per token

	now := time.Now()
	var mu sync.Mutex
	slept := time.Duration(0)
	l.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now.Add(slept) }
	l.sleep = func(_ context.Context, d time.Duration) error {
		mu.Lock()
		slept += d
		mu.Unlock()
		return nil
	}

	// Drain the initial burst of 2.
	_ = l.Wait(context.Background())
	_ = l.Wait(context.Background())

	// Third wait must sleep ~30s of fake time.
	if err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := slept
	mu.Unlock()
	if got < 29*time.Second || got > 31*time.Second {
		t.Fatalf("slept %v, want ~30s", got)
	}
}

func TestRateLimiterContextCancel(t *testing.T) {
	l := NewRateLimiter(1)
	_ = l.Wait(context.Background()) // drain burst

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.Wait(ctx); err == nil {
		t.Fatal("expected ctx error")
	}
}

func TestRateLimiterNonPositiveRPMIsNoop(t *testing.T) {
	for _, rpm := range []int{0, -1} {
		if err := NewRateLimiter(rpm).Wait(context.Background()); err != nil {
			t.Fatalf("rpm %d: %v", rpm, err)
		}
	}
}
