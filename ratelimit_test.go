package ottomarket

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeClock lets the limiter run without real waiting.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func newTestLimiter(t *testing.T, limits ...RateLimit) (*MemoryRateLimiter, *fakeClock, *[]time.Duration) {
	t.Helper()
	l, err := NewMemoryRateLimiter(limits...)
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{now: time.Unix(0, 0)}
	var waits []time.Duration
	l.now = clock.Now
	l.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	return l, clock, &waits
}

func TestMemoryRateLimiterSlidingWindows(t *testing.T) {
	l, _, _ := newTestLimiter(t, RateLimit{2, time.Second}, RateLimit{3, 10 * time.Second})
	want := []time.Duration{0, 0, 1 * time.Second, 10 * time.Second, 10 * time.Second, 11 * time.Second}
	for i, w := range want {
		got := l.reserve().Sub(time.Unix(0, 0))
		if got != w {
			t.Errorf("request %d at %v, want %v", i+1, got, w)
		}
	}
}

func TestMemoryRateLimiterDefaults(t *testing.T) {
	l, _, _ := newTestLimiter(t)
	start := time.Unix(0, 0)
	var times []time.Time
	for range 1001 {
		times = append(times, l.reserve())
	}
	// 20 per second: request 21 waits one second.
	if d := times[20].Sub(start); d != time.Second {
		t.Errorf("request 21 at %v", d)
	}
	// 1000 per minute: request 1001 waits for the first to leave the window.
	if d := times[1000].Sub(start); d != time.Minute {
		t.Errorf("request 1001 at %v", d)
	}
	// No sliding window of one minute holds more than 1000 requests, and
	// none of one second more than 20.
	for i := range times {
		if i >= 1000 && times[i].Sub(times[i-1000]) < time.Minute {
			t.Fatalf("more than 1000 requests per minute at %d", i)
		}
		if i >= 20 && times[i].Sub(times[i-20]) < time.Second {
			t.Fatalf("more than 20 requests per second at %d", i)
		}
	}
}

func TestMemoryRateLimiterWaitsAndCancels(t *testing.T) {
	l, _, waits := newTestLimiter(t, RateLimit{1, time.Second})
	ctx := context.Background()
	l.Wait(ctx, nil)
	l.Wait(ctx, nil)
	if len(*waits) != 1 || (*waits)[0] != time.Second {
		t.Fatalf("waits = %v", *waits)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := l.Wait(cancelled, nil); err == nil {
		t.Fatal("expected context error")
	}
}

func TestNewMemoryRateLimiterInvalid(t *testing.T) {
	if _, err := NewMemoryRateLimiter(RateLimit{0, time.Second}); err == nil {
		t.Fatal("expected error")
	}
}
