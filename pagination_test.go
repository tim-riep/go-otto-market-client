package ottomarket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// pageServer serves the given bodies keyed by request URI and records
// the requested URIs.
func pageServer(t *testing.T, pages map[string]string) (*httptest.Server, *[]string) {
	t.Helper()
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.RequestURI())
		body, ok := pages[r.URL.RequestURI()]
		if !ok {
			http.Error(w, `{"errors":[]}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &requested
}

func newPagingClient(t *testing.T, url string) *ClientWithResponses {
	t.Helper()
	c, err := New(Production, staticToken, WithBaseURL(url), WithRateLimiter(nil))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPagesLinksArray(t *testing.T) {
	srv, requested := pageServer(t, map[string]string{
		"/v4/orders?limit=2":              `{"resources":[{},{}],"links":[{"rel":"next","href":"/v4/orders?limit=2&nextcursor=a"}]}`,
		"/v4/orders?limit=2&nextcursor=a": `{"resources":[{},{}],"links":[{"rel":"self","href":"/x"},{"rel":"next","href":"/v4/orders?limit=2&nextcursor=b"}]}`,
		"/v4/orders?limit=2&nextcursor=b": `{"resources":[{}],"links":[]}`,
	})
	c := newPagingClient(t, srv.URL)
	limit := "2"
	total := 0
	for page, err := range c.OrdersV4FindPartnerOrdersPages(context.Background(), &OrdersV4FindPartnerOrdersParams{Limit: &limit}) {
		if err != nil {
			t.Fatal(err)
		}
		total += len(*page.JSON200.Resources)
	}
	if total != 5 || len(*requested) != 3 {
		t.Fatalf("total %d, requests %v", total, *requested)
	}
}

func TestPagesLinksObject(t *testing.T) {
	srv, requested := pageServer(t, map[string]string{
		"/v1/shipping-profiles":          `{"results":[],"links":{"rel":"next","href":"/v1/shipping-profiles?cursor=x"}}`,
		"/v1/shipping-profiles?cursor=x": `{"results":[]}`,
	})
	c := newPagingClient(t, srv.URL)
	n := 0
	for _, err := range c.ShippingProfilesV1GetShippingProfilesPages(context.Background(), &ShippingProfilesV1GetShippingProfilesParams{}) {
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 2 {
		t.Fatalf("pages %d, requests %v", n, *requested)
	}
}

func TestPagesHALLinks(t *testing.T) {
	srv, requested := pageServer(t, map[string]string{
		"/v1/sponsored-product-ads/campaigns":          `{"campaigns":[],"nextCursor":"c","_links":{"next":{"href":"/v1/sponsored-product-ads/campaigns?cursor=c"}}}`,
		"/v1/sponsored-product-ads/campaigns?cursor=c": `{"campaigns":[],"_links":{}}`,
	})
	c := newPagingClient(t, srv.URL)
	n := 0
	for _, err := range c.SponsoredProductAdsV1GetCampaignsPages(context.Background(), &SponsoredProductAdsV1GetCampaignsParams{}) {
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 2 {
		t.Fatalf("pages %d, requests %v", n, *requested)
	}
}

func TestPagesStopsEarly(t *testing.T) {
	srv, requested := pageServer(t, map[string]string{
		"/v4/orders": `{"resources":[],"links":[{"rel":"next","href":"/v4/orders?nextcursor=a"}]}`,
	})
	c := newPagingClient(t, srv.URL)
	for range c.OrdersV4FindPartnerOrdersPages(context.Background(), &OrdersV4FindPartnerOrdersParams{}) {
		break
	}
	if len(*requested) != 1 {
		t.Fatalf("requests after break: %v", *requested)
	}
}

func TestPagesErrors(t *testing.T) {
	tests := map[string]map[string]string{
		"status": {
			"/v4/orders": `{"resources":[],"links":[{"rel":"next","href":"/v4/orders?nextcursor=missing"}]}`,
		},
		"foreign host": {
			"/v4/orders": `{"resources":[],"links":[{"rel":"next","href":"https://evil.example/v4/orders"}]}`,
		},
		"loop": {
			"/v4/orders":              `{"resources":[],"links":[{"rel":"next","href":"/v4/orders?nextcursor=a"}]}`,
			"/v4/orders?nextcursor=a": `{"resources":[],"links":[{"rel":"next","href":"/v4/orders?nextcursor=a"}]}`,
		},
	}
	for name, pages := range tests {
		t.Run(name, func(t *testing.T) {
			srv, _ := pageServer(t, pages)
			c := newPagingClient(t, srv.URL)
			var lastErr error
			n := 0
			for _, err := range c.OrdersV4FindPartnerOrdersPages(context.Background(), &OrdersV4FindPartnerOrdersParams{}) {
				n++
				lastErr = err
			}
			if lastErr == nil {
				t.Fatal("expected error")
			}
			var pe *PageError
			if name == "status" && (!errors.As(lastErr, &pe) || pe.StatusCode != http.StatusInternalServerError) {
				t.Fatalf("err = %v", lastErr)
			}
		})
	}
}

func TestNextLink(t *testing.T) {
	tests := map[string]string{
		`{"links":[{"rel":"next","href":"/a"}]}`:      "/a",
		`{"links":[{"rel":"NEXT","href":"/a"}]}`:      "/a",
		`{"links":{"rel":"next","href":"/b"}}`:        "/b",
		`{"links":{"rel":"self","href":"/b"}}`:        "",
		`{"_links":{"next":{"href":"/c"},"self":{}}}`: "/c",
		`{"links":[]}`: "",
		`{}`:           "",
		`[]`:           "",
		`not json`:     "",
	}
	for body, want := range tests {
		if got := nextLink([]byte(body)); got != want {
			t.Errorf("nextLink(%s) = %q, want %q", body, got, want)
		}
	}
}
