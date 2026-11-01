package ottomarket

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"time"
)

// Option configures New. Besides the options in this file, every
// ClientOption of the generated client (WithBaseURL, WithHTTPClient,
// WithRequestEditorFn) can be passed.
type Option interface {
	apply(*settings)
}

type settings struct {
	clientOpts   []ClientOption
	limiter      RateLimiter
	limiterSet   bool
	maxRetries   int
	maxRetryWait time.Duration
}

func (o ClientOption) apply(s *settings) { s.clientOpts = append(s.clientOpts, o) }

type optionFunc func(*settings)

func (f optionFunc) apply(s *settings) { f(s) }

// WithRateLimiter sets the rate limiter. Default: a MemoryRateLimiter with
// DefaultRateLimits for each client created by New. Share one limiter
// between all clients of the same partner. nil disables rate limiting.
func WithRateLimiter(l RateLimiter) Option {
	return optionFunc(func(s *settings) { s.limiter, s.limiterSet = l, true })
}

// WithRetries sets how often a request is repeated after the API answered
// 429 Too Many Requests. Default: 3. 0 disables retries.
func WithRetries(n int) Option {
	return optionFunc(func(s *settings) { s.maxRetries = max(n, 0) })
}

// WithMaxRetryWait caps the wait before a retry. Default: 30 seconds.
func WithMaxRetryWait(d time.Duration) Option {
	return optionFunc(func(s *settings) { s.maxRetryWait = d })
}

// New returns an API client for the given environment that authenticates
// every request with a token from tokens.
//
// The Authorization header is always set from the TokenProvider right
// before a request is sent; a header set by a RequestEditorFn is
// overwritten. Requests are throttled by a RateLimiter and repeated when
// the API answers 429 Too Many Requests.
func New(env Environment, tokens TokenProvider, opts ...Option) (*ClientWithResponses, error) {
	if !env.valid() {
		return nil, fmt.Errorf("ottomarket: invalid environment %d", int(env))
	}
	if tokens == nil {
		return nil, errors.New("ottomarket: TokenProvider is nil")
	}
	s := settings{maxRetries: 3, maxRetryWait: 30 * time.Second}
	for _, o := range opts {
		o.apply(&s)
	}
	if !s.limiterSet {
		l, err := NewMemoryRateLimiter()
		if err != nil {
			return nil, err
		}
		s.limiter = l
	}

	wrap := func(c *Client) error {
		next := c.Client
		if next == nil {
			next = &http.Client{Timeout: 60 * time.Second}
		}
		c.Client = &transport{
			next:         next,
			tokens:       tokens,
			limiter:      s.limiter,
			maxRetries:   s.maxRetries,
			maxRetryWait: s.maxRetryWait,
			sleep:        sleepCtx,
		}
		return nil
	}
	// wrap runs last so that it wraps any HTTP client set by the options.
	return NewClientWithResponses(env.BaseURL(), append(s.clientOpts, wrap)...)
}

// transport throttles, authenticates and retries every request.
type transport struct {
	next         HttpRequestDoer
	tokens       TokenProvider
	limiter      RateLimiter
	maxRetries   int
	maxRetryWait time.Duration
	sleep        func(ctx context.Context, d time.Duration) error
}

func (t *transport) Do(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" && !isLoopback(req.URL.Hostname()) {
		return nil, fmt.Errorf("ottomarket: refusing to send token over %s to %s", req.URL.Scheme, req.URL.Host)
	}
	ctx := req.Context()
	for attempt := 0; ; attempt++ {
		r, err := t.prepare(req, attempt)
		if err != nil {
			return nil, err
		}
		if t.limiter != nil {
			if err := t.limiter.Wait(ctx, r); err != nil {
				return nil, fmt.Errorf("ottomarket: rate limiter: %w", err)
			}
		}
		resp, err := t.next.Do(r)
		if err != nil || resp.StatusCode != http.StatusTooManyRequests ||
			attempt >= t.maxRetries || !replayable(req) {
			return resp, err
		}
		wait := t.retryWait(resp, attempt)
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if err := t.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

// prepare clones req for one attempt and sets the Authorization header.
func (t *transport) prepare(req *http.Request, attempt int) (*http.Request, error) {
	tok, err := t.tokens.Token(req.Context())
	if err != nil {
		return nil, fmt.Errorf("ottomarket: get token: %w", err)
	}
	if tok == "" {
		return nil, errors.New("ottomarket: TokenProvider returned an empty token")
	}
	r := req.Clone(req.Context())
	if attempt > 0 && req.Body != nil && req.Body != http.NoBody {
		if r.Body, err = req.GetBody(); err != nil {
			return nil, fmt.Errorf("ottomarket: replay request body: %w", err)
		}
	}
	r.Header.Set("Authorization", "Bearer "+tok)
	return r, nil
}

// replayable reports whether req can be sent again. A 429 answer means the
// request was rejected without being processed, so repeating it is safe
// for every method as long as the body can be read again.
func replayable(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

// retryWait honours Retry-After and otherwise backs off exponentially
// (1s, 2s, 4s, ...) with jitter.
func (t *transport) retryWait(resp *http.Response, attempt int) time.Duration {
	var d time.Duration
	if v := resp.Header.Get("Retry-After"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s >= 0 {
			d = time.Duration(s) * time.Second
		} else if at, err := http.ParseTime(v); err == nil {
			d = max(time.Until(at), 0)
		}
		return min(d, t.maxRetryWait)
	}
	d = time.Second << min(attempt, 10)
	d += time.Duration(rand.Int64N(int64(d / 4)))
	return min(d, t.maxRetryWait)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
