package ottomarket

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

func TestNewSetsAuthorization(t *testing.T) {
	var got []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer api.Close()

	tokens := TokenProviderFunc(func(context.Context) (string, error) { return "abc", nil })
	c, err := New(Production, tokens, WithBaseURL(api.URL))
	if err != nil {
		t.Fatal(err)
	}

	// A RequestEditorFn must not be able to replace the token.
	override := func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer forged")
		return nil
	}
	if _, err := c.PriceReductionsV1GetAllReductionsForPartnerWithResponse(context.Background(),
		&PriceReductionsV1GetAllReductionsForPartnerParams{}, override); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "Bearer abc" {
		t.Fatalf("Authorization headers = %q", got)
	}
}

func TestNewTokenError(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request sent without token")
	}))
	defer api.Close()

	want := errors.New("no token")
	tokens := TokenProviderFunc(func(context.Context) (string, error) { return "", want })
	c, _ := New(Production, tokens, WithBaseURL(api.URL))
	_, err := c.PriceReductionsV1GetAllReductionsForPartnerWithResponse(context.Background(),
		&PriceReductionsV1GetAllReductionsForPartnerParams{})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewRefusesPlainHTTP(t *testing.T) {
	tokens := TokenProviderFunc(func(context.Context) (string, error) { return "abc", nil })
	c, _ := New(Production, tokens, WithBaseURL("http://example.com"))
	_, err := c.PriceReductionsV1GetAllReductionsForPartnerWithResponse(context.Background(),
		&PriceReductionsV1GetAllReductionsForPartnerParams{})
	if err == nil {
		t.Fatal("expected error for plain HTTP")
	}
}

func TestNewNilProvider(t *testing.T) {
	if _, err := New(Production, nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewInvalidEnvironment(t *testing.T) {
	tokens := TokenProviderFunc(func(context.Context) (string, error) { return "abc", nil })
	if _, err := New(Environment(42), tokens); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewUsesEnvironmentURL(t *testing.T) {
	tokens := TokenProviderFunc(func(context.Context) (string, error) { return "abc", nil })
	for env, want := range map[Environment]string{
		Production: "https://api.otto.market/",
		Sandbox:    "https://sandbox.api.otto.market/",
	} {
		c, err := New(env, tokens)
		if err != nil {
			t.Fatal(err)
		}
		if got := c.ClientInterface.(*Client).Server; got != want {
			t.Errorf("%s: server = %q, want %q", env, got, want)
		}
	}
}

func newRetryServer(t *testing.T, failures int32) (*httptest.Server, *atomic.Int32, *[]string) {
	t.Helper()
	var calls atomic.Int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if calls.Add(1) <= failures {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &bodies
}

var staticToken = TokenProviderFunc(func(context.Context) (string, error) { return "abc", nil })

func TestRetryOn429(t *testing.T) {
	srv, calls, _ := newRetryServer(t, 2)
	c, _ := New(Production, staticToken, WithBaseURL(srv.URL))
	resp, err := c.PriceReductionsV1GetAllReductionsForPartnerWithResponse(context.Background(),
		&PriceReductionsV1GetAllReductionsForPartnerParams{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode() != http.StatusNoContent || calls.Load() != 3 {
		t.Fatalf("status %d after %d calls", resp.StatusCode(), calls.Load())
	}
}

func TestRetryGivesUp(t *testing.T) {
	srv, calls, _ := newRetryServer(t, 100)
	c, _ := New(Production, staticToken, WithBaseURL(srv.URL), WithRetries(2))
	resp, err := c.PriceReductionsV1GetAllReductionsForPartnerWithResponse(context.Background(),
		&PriceReductionsV1GetAllReductionsForPartnerParams{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode() != http.StatusTooManyRequests || calls.Load() != 3 {
		t.Fatalf("status %d after %d calls", resp.StatusCode(), calls.Load())
	}
}

func TestRetryReplaysBody(t *testing.T) {
	srv, _, bodies := newRetryServer(t, 1)
	c, _ := New(Production, staticToken, WithBaseURL(srv.URL))
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/x", strings.NewReader("payload"))
	if _, err := c.ClientInterface.(*Client).Client.Do(req); err != nil {
		t.Fatal(err)
	}
	if len(*bodies) != 2 || (*bodies)[0] != "payload" || (*bodies)[1] != "payload" {
		t.Fatalf("bodies = %q", *bodies)
	}
}

type countingLimiter struct{ n atomic.Int32 }

func (l *countingLimiter) Wait(context.Context, *http.Request) error { l.n.Add(1); return nil }

func TestCustomRateLimiter(t *testing.T) {
	srv, _, _ := newRetryServer(t, 1)
	l := &countingLimiter{}
	c, _ := New(Production, staticToken, WithBaseURL(srv.URL), WithRateLimiter(l))
	c.PriceReductionsV1GetAllReductionsForPartnerWithResponse(context.Background(),
		&PriceReductionsV1GetAllReductionsForPartnerParams{})
	if l.n.Load() != 2 {
		t.Fatalf("limiter called %d times, want 2 (request and retry)", l.n.Load())
	}
}

func TestRetryWait(t *testing.T) {
	tr := &transport{maxRetryWait: 30 * time.Second}
	resp := &http.Response{Header: http.Header{}}
	for attempt, base := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		if d := tr.retryWait(resp, attempt); d < base || d > base+base/4 {
			t.Errorf("attempt %d: wait %v", attempt, d)
		}
	}
	if d := tr.retryWait(resp, 10); d != 30*time.Second {
		t.Errorf("cap: wait %v", d)
	}
	resp.Header.Set("Retry-After", "5")
	if d := tr.retryWait(resp, 0); d != 5*time.Second {
		t.Errorf("Retry-After: wait %v", d)
	}
}
