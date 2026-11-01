package ottomarket

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Server is the base URL of the OTTO Market API.
const Server = "https://api.otto.market"

// New returns an API client that authenticates every request with a token
// from tokens.
//
// The Authorization header is always set from the TokenProvider right
// before a request is sent; a header set by a RequestEditorFn is
// overwritten. Pass WithBaseURL to use another server and WithHTTPClient
// to use your own HTTP client.
func New(tokens TokenProvider, opts ...ClientOption) (*ClientWithResponses, error) {
	if tokens == nil {
		return nil, errors.New("ottomarket: TokenProvider is nil")
	}
	auth := func(c *Client) error {
		next := c.Client
		if next == nil {
			next = &http.Client{Timeout: 60 * time.Second}
		}
		c.Client = &authDoer{next: next, tokens: tokens}
		return nil
	}
	// The auth option runs last so that it wraps any HTTP client set by opts.
	all := append(append([]ClientOption{}, opts...), auth)
	return NewClientWithResponses(Server, all...)
}

// authDoer sets the Authorization header on every request.
type authDoer struct {
	next   HttpRequestDoer
	tokens TokenProvider
}

func (d *authDoer) Do(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" && !isLoopback(req.URL.Hostname()) {
		return nil, fmt.Errorf("ottomarket: refusing to send token over %s to %s", req.URL.Scheme, req.URL.Host)
	}
	tok, err := d.tokens.Token(req.Context())
	if err != nil {
		return nil, fmt.Errorf("ottomarket: get token: %w", err)
	}
	if tok == "" {
		return nil, errors.New("ottomarket: TokenProvider returned an empty token")
	}
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+tok)
	return d.next.Do(r)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
