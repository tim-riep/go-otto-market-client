package ottomarket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"
)

// OTTO's documented pattern for X-Request-Timestamp.
var timestampPattern = regexp.MustCompile(`^\d{4}-(0\d|1[0-2])-([0-2]\d|3[01])T(2[0-3]|[01]\d):[0-5]\d:[0-5]\d(.\d+)?([+-](2[0-3]|[01]\d):[0-5]\d|Z)$`)

func TestRequestTimestampFormat(t *testing.T) {
	for _, tm := range []time.Time{
		time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		time.Date(2026, 12, 31, 23, 59, 59, 999e6, time.FixedZone("CET", 3600)),
	} {
		r := httptest.NewRequest(http.MethodGet, "/v4/orders", nil)
		setCommonHeaders(r, tm)
		if v := r.Header.Get("X-Request-Timestamp"); !timestampPattern.MatchString(v) {
			t.Errorf("timestamp %q does not match OTTO pattern", v)
		}
	}
}

func TestAcceptFor(t *testing.T) {
	tests := []struct{ method, path, want string }{
		{"GET", "/v4/orders", "application/json"},
		{"GET", "/v3/receipts/12345.pdf", "application/pdf, application/json;q=0.5"},
		{"GET", "/v1/spa-reporting/reports/download", "application/json, text/csv"},
		{"GET", "/unknown", "application/json"},
	}
	for _, tt := range tests {
		if got := acceptFor(tt.method, tt.path); got != tt.want {
			t.Errorf("acceptFor(%s %s) = %q, want %q", tt.method, tt.path, got, tt.want)
		}
	}
}

func TestCommonHeadersSent(t *testing.T) {
	var h http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c, _ := New(Production, staticToken, WithBaseURL(srv.URL))

	c.PriceReductionsV1GetAllReductionsForPartnerWithResponse(context.Background(),
		&PriceReductionsV1GetAllReductionsForPartnerParams{})
	if !timestampPattern.MatchString(h.Get("X-Request-Timestamp")) {
		t.Errorf("X-Request-Timestamp = %q", h.Get("X-Request-Timestamp"))
	}
	if h.Get("Accept") != "application/json" {
		t.Errorf("Accept = %q", h.Get("Accept"))
	}

	// A caller-defined Accept header is kept.
	c.PriceReductionsV1GetAllReductionsForPartnerWithResponse(context.Background(),
		&PriceReductionsV1GetAllReductionsForPartnerParams{},
		func(_ context.Context, r *http.Request) error { r.Header.Set("Accept", "text/plain"); return nil })
	if h.Get("Accept") != "text/plain" {
		t.Errorf("Accept = %q", h.Get("Accept"))
	}
}

func TestContentTypeOnlyWithBody(t *testing.T) {
	var h http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c, _ := New(Production, staticToken, WithBaseURL(srv.URL))

	c.AvailabilityV1StoreProductDeliveryInformationUsingPOSTWithApplicationJSONCharsetUTF8Body(context.Background(),
		AvailabilityV1StoreProductDeliveryInformationUsingPOSTApplicationJSONCharsetUTF8RequestBody{})
	if ct := h.Get("Content-Type"); ct != "application/json;charset=UTF-8" {
		t.Errorf("POST Content-Type = %q", ct)
	}

	c.PriceReductionsV1GetAllReductionsForPartnerWithResponse(context.Background(),
		&PriceReductionsV1GetAllReductionsForPartnerParams{})
	if ct := h.Get("Content-Type"); ct != "" {
		t.Errorf("GET Content-Type = %q", ct)
	}
}
