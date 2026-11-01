package ottomarket

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// RateLimiter throttles requests to the OTTO Market API.
//
// OTTO limits requests per partner, not per process. If several processes
// or clients work for the same partner, they must share one RateLimiter,
// e.g. an implementation backed by Redis or PostgreSQL.
type RateLimiter interface {
	// Wait blocks until req may be sent. It returns an error if ctx is
	// done first. It must be safe for concurrent use.
	Wait(ctx context.Context, req *http.Request) error
}

// RateLimit allows at most Requests requests within any period of length Per.
type RateLimit struct {
	Requests int
	Per      time.Duration
}

// DefaultRateLimits are the documented limits of the OTTO Market API for
// all endpoints except the token endpoint: at most 20 requests per second
// and 1,000 requests per minute per partner.
var DefaultRateLimits = []RateLimit{
	{Requests: 20, Per: time.Second},
	{Requests: 1000, Per: time.Minute},
}

// MemoryRateLimiter is an in-process RateLimiter. It enforces every limit
// exactly over a sliding window, so no window of length Per ever contains
// more than Requests requests. Requests are served in the order in which
// they call Wait.
type MemoryRateLimiter struct {
	mu      sync.Mutex
	windows []window
	last    time.Time
	now     func() time.Time
	sleep   func(ctx context.Context, d time.Duration) error
}

type window struct {
	limit RateLimit
	times []time.Time // ring buffer of the last limit.Requests send times
	next  int
	full  bool
}

// NewMemoryRateLimiter returns a RateLimiter that enforces all given limits.
// Without limits it uses DefaultRateLimits.
func NewMemoryRateLimiter(limits ...RateLimit) (*MemoryRateLimiter, error) {
	if len(limits) == 0 {
		limits = DefaultRateLimits
	}
	l := &MemoryRateLimiter{now: time.Now, sleep: sleepCtx}
	for _, lim := range limits {
		if lim.Requests <= 0 || lim.Per <= 0 {
			return nil, errors.New("ottomarket: rate limit needs Requests > 0 and Per > 0")
		}
		l.windows = append(l.windows, window{limit: lim, times: make([]time.Time, lim.Requests)})
	}
	return l, nil
}

// Wait implements RateLimiter.
func (l *MemoryRateLimiter) Wait(ctx context.Context, _ *http.Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	at := l.reserve()
	if d := at.Sub(l.now()); d > 0 {
		return l.sleep(ctx, d)
	}
	return nil
}

// reserve books the earliest send time that respects all limits.
func (l *MemoryRateLimiter) reserve() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	at := l.now()
	if at.Before(l.last) {
		at = l.last
	}
	for i := range l.windows {
		w := &l.windows[i]
		if w.full {
			// The oldest of the last Requests send times must have left
			// the window before the next request may be sent.
			if earliest := w.times[w.next].Add(w.limit.Per); at.Before(earliest) {
				at = earliest
			}
		}
	}
	for i := range l.windows {
		w := &l.windows[i]
		w.times[w.next] = at
		w.next = (w.next + 1) % len(w.times)
		if w.next == 0 {
			w.full = true
		}
	}
	l.last = at
	return at
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
