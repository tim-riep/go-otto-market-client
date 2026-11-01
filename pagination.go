package ottomarket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"strings"
)

// PageError is returned by a Pages iterator when the API answers a page
// request with a status other than 2xx.
type PageError struct {
	StatusCode int
	Body       []byte
}

func (e *PageError) Error() string {
	b := e.Body
	if len(b) > 512 {
		b = append(b[:512:512], "..."...)
	}
	return fmt.Sprintf("ottomarket: page request failed with status %d: %s", e.StatusCode, b)
}

// pages implements the generated *Pages methods. It requests the first
// page with first and then follows the "next" link of every page until a
// page has none. The iterator stops after the first error.
//
// OTTO uses three shapes for the link to the next page, all supported:
//
//	{"links": [{"rel": "next", "href": "..."}]}
//	{"links": {"rel": "next", "href": "..."}}
//	{"_links": {"next": {"href": "..."}}}
func pages[R any](
	ctx context.Context,
	c *ClientWithResponses,
	first func(context.Context) (*R, error),
	parse func(*http.Response) (*R, error),
	raw func(*R) ([]byte, *http.Response),
	reqEditors []RequestEditorFn,
) iter.Seq2[*R, error] {
	return func(yield func(*R, error) bool) {
		page, err := first(ctx)
		seen := map[string]bool{}
		for {
			if err != nil {
				yield(nil, err)
				return
			}
			body, resp := raw(page)
			if resp.StatusCode < 200 || resp.StatusCode > 299 {
				yield(nil, &PageError{StatusCode: resp.StatusCode, Body: body})
				return
			}
			if !yield(page, nil) {
				return
			}
			next := nextLink(body)
			if next == "" {
				return
			}
			if seen[next] {
				yield(nil, fmt.Errorf("ottomarket: pagination loop at %s", next))
				return
			}
			seen[next] = true
			page, err = fetchPage(ctx, c, next, parse, reqEditors)
		}
	}
}

func fetchPage[R any](ctx context.Context, c *ClientWithResponses, href string,
	parse func(*http.Response) (*R, error), reqEditors []RequestEditorFn) (*R, error) {
	cl, ok := c.ClientInterface.(*Client)
	if !ok {
		return nil, fmt.Errorf("ottomarket: pagination needs a client created by New")
	}
	u, err := resolveLink(cl.Server, href)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if err := cl.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	resp, err := cl.Client.Do(req)
	if err != nil {
		return nil, err
	}
	return parse(resp)
}

// resolveLink resolves href against the server URL. Links to other hosts
// are refused so that the token is never sent elsewhere.
func resolveLink(server, href string) (string, error) {
	base, err := url.Parse(server)
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(href)
	if err != nil {
		return "", fmt.Errorf("ottomarket: invalid next link %q: %w", href, err)
	}
	// Links are absolute paths ("/v4/orders?..."); resolve them against
	// the host only, the server URL has no base path.
	u := base.ResolveReference(ref)
	if u.Scheme != base.Scheme || u.Host != base.Host {
		return "", fmt.Errorf("ottomarket: next link %q points to another host", href)
	}
	return u.String(), nil
}

type link struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

// nextLink extracts the href of the "next" link from a page body.
func nextLink(body []byte) string {
	var page struct {
		Links    json.RawMessage `json:"links"`
		HALLinks struct {
			Next *link `json:"next"`
		} `json:"_links"`
	}
	if json.Unmarshal(body, &page) != nil {
		return ""
	}
	if page.HALLinks.Next != nil && page.HALLinks.Next.Href != "" {
		return page.HALLinks.Next.Href
	}
	l := bytes.TrimSpace(page.Links)
	if len(l) == 0 {
		return ""
	}
	var links []link
	if l[0] == '{' {
		var one link
		if json.Unmarshal(l, &one) != nil {
			return ""
		}
		links = []link{one}
	} else if json.Unmarshal(l, &links) != nil {
		return ""
	}
	for _, x := range links {
		if strings.EqualFold(x.Rel, "next") && x.Href != "" {
			return x.Href
		}
	}
	return ""
}
