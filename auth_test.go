package ottomarket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type tokenServer struct {
	*httptest.Server
	calls atomic.Int32
}

func newTokenServer(t *testing.T, expiresIn int) *tokenServer {
	t.Helper()
	ts := &tokenServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := ts.calls.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.PostForm.Get("grant_type") != "client_credentials" ||
			r.PostForm.Get("client_id") != "id" || r.PostForm.Get("client_secret") != "secret" {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("token-%d", n),
			"expires_in":   expiresIn,
			"token_type":   "Bearer",
			"scope":        r.PostForm.Get("scope"),
		})
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestClientCredentialsCachesToken(t *testing.T) {
	ts := newTokenServer(t, 1800)
	cache := NewMemoryCache()
	p := NewClientCredentials(Production, "id", "secret", []string{ScopeProducts, ScopeOrders, ScopeOrders},
		WithTokenURL(ts.URL), WithTokenCache(cache))

	for range 3 {
		tok, err := p.Token(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if tok != "token-1" {
			t.Fatalf("token = %q", tok)
		}
	}
	if n := ts.calls.Load(); n != 1 {
		t.Fatalf("token endpoint called %d times", n)
	}

	stored, _ := cache.Get(context.Background(), p.CacheKey())
	if stored == nil || stored.Scope != "orders products" {
		t.Fatalf("cached token = %+v", stored)
	}
}

func TestClientCredentialsRenewsBeforeExpiry(t *testing.T) {
	ts := newTokenServer(t, 1800)
	now := time.Now()
	p := NewClientCredentials(Production, "id", "secret", nil, WithTokenURL(ts.URL), WithExpiryMargin(time.Minute))
	p.now = func() time.Time { return now }

	if tok, _ := p.Token(context.Background()); tok != "token-1" {
		t.Fatalf("token = %q", tok)
	}
	now = now.Add(28 * time.Minute) // 2 minutes left: still valid
	if tok, _ := p.Token(context.Background()); tok != "token-1" {
		t.Fatalf("token = %q", tok)
	}
	now = now.Add(90 * time.Second) // 30 seconds left: within margin
	if tok, _ := p.Token(context.Background()); tok != "token-2" {
		t.Fatalf("token = %q", tok)
	}
}

func TestClientCredentialsConcurrent(t *testing.T) {
	ts := newTokenServer(t, 1800)
	p := NewClientCredentials(Production, "id", "secret", nil, WithTokenURL(ts.URL))
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.Token(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := ts.calls.Load(); n != 1 {
		t.Fatalf("token endpoint called %d times", n)
	}
}

func TestClientCredentialsInvalidClient(t *testing.T) {
	ts := newTokenServer(t, 1800)
	p := NewClientCredentials(Production, "id", "wrong", nil, WithTokenURL(ts.URL))
	_, err := p.Token(context.Background())
	var te *TokenError
	if !errors.As(err, &te) || te.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err = %v", err)
	}
}

type failingCache struct{}

func (failingCache) Get(context.Context, string) (*Token, error) { return nil, errors.New("down") }
func (failingCache) Set(context.Context, string, Token) error    { return errors.New("down") }

func TestClientCredentialsCacheError(t *testing.T) {
	ts := newTokenServer(t, 1800)
	p := NewClientCredentials(Production, "id", "secret", nil, WithTokenURL(ts.URL), WithTokenCache(failingCache{}))
	if _, err := p.Token(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestCacheKey(t *testing.T) {
	a := NewClientCredentials(Production, "id", "s1", []string{"b", "a"}).CacheKey()
	b := NewClientCredentials(Production, "id", "s2", []string{"a", "b"}).CacheKey()
	c := NewClientCredentials(Production, "id", "s1", []string{"a"}).CacheKey()
	if a != b {
		t.Error("key depends on secret or scope order")
	}
	if a == c {
		t.Error("key does not depend on scopes")
	}
	if len(a) > 100 {
		t.Error("key too long")
	}
}

func TestMemoryCacheExpiry(t *testing.T) {
	var c MemoryCache
	ctx := context.Background()
	c.Set(ctx, "k", Token{AccessToken: "x", ExpiresAt: time.Now().Add(-time.Second)})
	if got, _ := c.Get(ctx, "k"); got != nil {
		t.Fatalf("expired token returned: %+v", got)
	}
	c.Set(ctx, "k", Token{AccessToken: "x", ExpiresAt: time.Now().Add(time.Hour)})
	if got, _ := c.Get(ctx, "k"); got == nil || got.AccessToken != "x" {
		t.Fatalf("got %+v", got)
	}
}

func TestCacheKeyDependsOnEnvironment(t *testing.T) {
	if NewClientCredentials(Production, "id", "s", nil).CacheKey() ==
		NewClientCredentials(Sandbox, "id", "s", nil).CacheKey() {
		t.Error("production and sandbox share a cache key")
	}
}
