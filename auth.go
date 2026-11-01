package ottomarket

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Scopes of the OTTO Market API, used when requesting a token.
const (
	ScopeProducts              = "products"
	ScopeOrders                = "orders"
	ScopeReceipts              = "receipts"
	ScopeReturns               = "returns"
	ScopePriceReduction        = "price-reduction"
	ScopeShipments             = "shipments"
	ScopeShippingProfiles      = "shipping-profiles"
	ScopeAvailability          = "availability"
	ScopeReturnsWarehouseRead  = "returns-warehouse-read"
	ScopeReturnsWarehouseWrite = "returns-warehouse-write"
	// ScopeAdvertisingServices covers Sponsored Product Ads and its reporting.
	ScopeAdvertisingServices = "advertising-services"
)

// TokenProvider supplies the access token for API requests.
//
// It is the only way the client authenticates: every request gets the
// header "Authorization: Bearer <token>" with the value returned here.
// Use ClientCredentials to let this package fetch and cache tokens, or
// implement the interface yourself if your application already manages
// OTTO tokens elsewhere.
type TokenProvider interface {
	// Token returns a valid access token without the "Bearer " prefix.
	// It is called for every request and must be safe for concurrent use.
	Token(ctx context.Context) (string, error)
}

// TokenProviderFunc adapts a function to the TokenProvider interface.
type TokenProviderFunc func(ctx context.Context) (string, error)

// Token calls f(ctx).
func (f TokenProviderFunc) Token(ctx context.Context) (string, error) { return f(ctx) }

// Token is an access token as stored in a TokenCache.
type Token struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type,omitempty"`
	Scope       string    `json:"scope,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// TokenCache stores access tokens, for example in memory, Redis or a
// database. Token is JSON serialisable for that purpose.
//
// Implementations must be safe for concurrent use. The key is opaque, at
// most 100 characters long and consists of [a-z0-9:] only.
type TokenCache interface {
	// Get returns the token stored under key, or nil (and no error) if
	// there is none. Returning an expired token is allowed; the caller
	// checks the expiry itself.
	Get(ctx context.Context, key string) (*Token, error)
	// Set stores token under key. Implementations should discard the
	// token at token.ExpiresAt (e.g. as Redis TTL).
	Set(ctx context.Context, key string, token Token) error
}

// MemoryCache is a TokenCache that keeps tokens in process memory.
// The zero value is ready to use.
type MemoryCache struct {
	mu     sync.Mutex
	tokens map[string]Token
}

// NewMemoryCache returns an empty in-memory token cache.
func NewMemoryCache() *MemoryCache { return &MemoryCache{} }

// Get implements TokenCache.
func (c *MemoryCache) Get(_ context.Context, key string) (*Token, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tokens[key]
	if !ok {
		return nil, nil
	}
	if !t.ExpiresAt.After(time.Now()) {
		delete(c.tokens, key)
		return nil, nil
	}
	return &t, nil
}

// Set implements TokenCache.
func (c *MemoryCache) Set(_ context.Context, key string, token Token) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tokens == nil {
		c.tokens = make(map[string]Token)
	}
	c.tokens[key] = token
	return nil
}

// ClientCredentials is a TokenProvider that obtains tokens from the OTTO
// token endpoint with the OAuth2 client credentials grant and caches them
// in a TokenCache until shortly before they expire.
//
// The grant has no refresh token; a new token is requested with the
// client credentials whenever the cached one is about to expire.
type ClientCredentials struct {
	clientID     string
	clientSecret string
	scope        string
	tokenURL     string
	httpClient   *http.Client
	cache        TokenCache
	margin       time.Duration
	now          func() time.Time

	mu sync.Mutex // serialises token requests of this instance
}

// CredentialsOption configures ClientCredentials.
type CredentialsOption func(*ClientCredentials)

// WithTokenCache sets the cache for tokens. Default: a MemoryCache.
func WithTokenCache(cache TokenCache) CredentialsOption {
	return func(c *ClientCredentials) { c.cache = cache }
}

// WithTokenURL overrides the token endpoint of the environment, e.g. for
// a proxy or a mock server.
func WithTokenURL(tokenURL string) CredentialsOption {
	return func(c *ClientCredentials) { c.tokenURL = tokenURL }
}

// WithTokenHTTPClient sets the HTTP client for token requests.
// Default: an http.Client with a 30 second timeout.
func WithTokenHTTPClient(client *http.Client) CredentialsOption {
	return func(c *ClientCredentials) { c.httpClient = client }
}

// WithExpiryMargin sets how long before its expiry a token is renewed.
// Default: one minute.
func WithExpiryMargin(d time.Duration) CredentialsOption {
	return func(c *ClientCredentials) { c.margin = d }
}

// NewClientCredentials returns a TokenProvider for the given environment,
// API client and scopes (see the Scope* constants). Use the same
// environment as for New.
func NewClientCredentials(env Environment, clientID, clientSecret string, scopes []string, opts ...CredentialsOption) *ClientCredentials {
	s := slices.Clone(scopes)
	slices.Sort(s)
	s = slices.Compact(s)
	c := &ClientCredentials{
		clientID:     clientID,
		clientSecret: clientSecret,
		scope:        strings.Join(s, " "),
		tokenURL:     env.TokenURL(),
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		cache:        NewMemoryCache(),
		margin:       time.Minute,
		now:          time.Now,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// CacheKey returns the key under which tokens of this provider are cached.
// It depends on token URL, client ID and scopes, but not on the secret.
func (c *ClientCredentials) CacheKey() string {
	sum := sha256.Sum256([]byte(c.tokenURL + "\x00" + c.clientID + "\x00" + c.scope))
	return "ottomarket:token:" + hex.EncodeToString(sum[:])
}

// Token implements TokenProvider.
func (c *ClientCredentials) Token(ctx context.Context) (string, error) {
	key := c.CacheKey()
	if t, err := c.cached(ctx, key); err != nil || t != "" {
		return t, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Another goroutine may have fetched a token while we waited.
	if t, err := c.cached(ctx, key); err != nil || t != "" {
		return t, err
	}

	tok, err := c.fetch(ctx)
	if err != nil {
		return "", err
	}
	if err := c.cache.Set(ctx, key, *tok); err != nil {
		return "", fmt.Errorf("ottomarket: store token in cache: %w", err)
	}
	return tok.AccessToken, nil
}

func (c *ClientCredentials) cached(ctx context.Context, key string) (string, error) {
	t, err := c.cache.Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("ottomarket: read token from cache: %w", err)
	}
	if t == nil || t.AccessToken == "" || !c.now().Add(c.margin).Before(t.ExpiresAt) {
		return "", nil
	}
	return t.AccessToken, nil
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

func (c *ClientCredentials) fetch(ctx context.Context) (*Token, error) {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
	}
	if c.scope != "" {
		form.Set("scope", c.scope)
	}
	if c.tokenURL == "" {
		return nil, errors.New("ottomarket: invalid environment")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("ottomarket: token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	issued := c.now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ottomarket: token request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("ottomarket: token request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &TokenError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("ottomarket: decode token response: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, errors.New("ottomarket: token response without access_token")
	}
	if tr.TokenType != "" && !strings.EqualFold(tr.TokenType, "bearer") {
		return nil, fmt.Errorf("ottomarket: unsupported token type %q", tr.TokenType)
	}
	return &Token{
		AccessToken: tr.AccessToken,
		TokenType:   tr.TokenType,
		Scope:       tr.Scope,
		ExpiresAt:   issued.Add(time.Duration(tr.ExpiresIn) * time.Second),
	}, nil
}

// TokenError is returned when the token endpoint rejects a request,
// for example because of invalid client credentials.
type TokenError struct {
	StatusCode int
	Body       string
}

func (e *TokenError) Error() string {
	b := e.Body
	if len(b) > 512 {
		b = b[:512] + "..."
	}
	return fmt.Sprintf("ottomarket: token request failed with status %d: %s", e.StatusCode, b)
}
