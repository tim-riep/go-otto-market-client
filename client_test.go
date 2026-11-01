package ottomarket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewSetsAuthorization(t *testing.T) {
	var got []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer api.Close()

	tokens := TokenProviderFunc(func(context.Context) (string, error) { return "abc", nil })
	c, err := New(tokens, WithBaseURL(api.URL))
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
	c, _ := New(tokens, WithBaseURL(api.URL))
	_, err := c.PriceReductionsV1GetAllReductionsForPartnerWithResponse(context.Background(),
		&PriceReductionsV1GetAllReductionsForPartnerParams{})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewRefusesPlainHTTP(t *testing.T) {
	tokens := TokenProviderFunc(func(context.Context) (string, error) { return "abc", nil })
	c, _ := New(tokens, WithBaseURL("http://example.com"))
	_, err := c.PriceReductionsV1GetAllReductionsForPartnerWithResponse(context.Background(),
		&PriceReductionsV1GetAllReductionsForPartnerParams{})
	if err == nil {
		t.Fatal("expected error for plain HTTP")
	}
}

func TestNewNilProvider(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("expected error")
	}
}
