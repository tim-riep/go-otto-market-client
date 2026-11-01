package main

import (
	"context"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const testSpec = `{
  "openapi": "3.0.3",
  "info": {"title": "PROSE", "version": "1", "description": "PROSE"},
  "paths": {
    "/v1/orders/{id}": {
      "get": {
        "operationId": "getOrder",
        "summary": "PROSE",
        "description": "PROSE",
        "parameters": [{"name": "id", "in": "path", "required": true,
                        "description": "PROSE", "schema": {"type": "string"}},
                       {"name": "Authorization", "in": "header", "required": true,
                        "schema": {"type": "string"}},
                       {"$ref": "#/components/parameters/Auth"}],
        "responses": {"200": {"description": "PROSE",
          "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Order"}}}}}
      }
    },
    "/v1/orders": {
      "get": {
        "operationId": "listOrders",
        "parameters": [{"name": "limit", "in": "query", "schema": {"type": "integer"}}],
        "responses": {"200": {"description": "PROSE", "content": {"application/json": {"schema": {
          "type": "object", "properties": {
            "resources": {"type": "array", "items": {"$ref": "#/components/schemas/Order"}},
            "links": {"type": "array", "items": {"type": "object", "properties": {"rel": {"type": "string"}, "href": {"type": "string"}}}}}}}}}}
      }
    },
    "/v1/orders/{id}.pdf": {
      "get": {
        "operationId": "getOrderPdf",
        "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "PROSE", "content": {"application/pdf": {"schema": {"type": "string", "format": "binary"}}}}}
      }
    }
  },
  "components": {"parameters": {"Auth": {"name": "authorization", "in": "header", "schema": {"type": "string"}}},
    "schemas": {"Order": {"type": "object", "description": "PROSE",
    "properties": {"description": {"type": "string", "description": "PROSE", "example": "PROSE"},
                   "total": {"type": "object", "properties": {"amount": {"type": "number"}, "currency": {"type": "string"}}},
                   "weight": {"type": "object", "properties": {"amount": {"type": "number"}, "unit": {"type": "string"}}},
                   "rate": {"type": "number"}}}}}
}`

func TestRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(testSpec))
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "client.gen.go")
	if err := run(context.Background(), srv.Client(), srv.URL, testOutputs(out), "ottomarket"); err != nil {
		t.Fatal(err)
	}
	code, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(code), "PROSE") {
		t.Fatalf("prose in generated code")
	}
	for _, want := range []string{`func \(c \*Client\) GetOrder\(`, `type Order struct`, `Description\s+\*string`,
		`Amount\s+\*Amount\s`, `Amount\s+\*float64\s`, `Rate\s+\*float64\s`} {
		if !regexp.MustCompile(want).Match(code) {
			t.Errorf("generated code lacks %s", want)
		}
	}
	if strings.Contains(string(code), "Authorization") || strings.Contains(string(code), "authorization") {
		t.Errorf("generated code exposes the Authorization header")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), out, code, 0); err != nil {
		t.Fatalf("generated code does not parse: %v", err)
	}

	accept, err := os.ReadFile(filepath.Join(filepath.Dir(out), "accept.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"GET", regexp.MustCompile("^/v1/orders/[^/]+$"), "application/json"`,
		`"GET", regexp.MustCompile("^/v1/orders/[^/]+\\.pdf$"), "application/pdf, application/json;q=0.5"`} {
		if !strings.Contains(string(accept), want) {
			t.Errorf("accept table lacks %s:\n%s", want, accept)
		}
	}

	pages, err := os.ReadFile(filepath.Join(filepath.Dir(out), "pages.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pages), "func (c *ClientWithResponses) ListOrdersPages(ctx context.Context, params *ListOrdersParams,") {
		t.Errorf("pages.gen.go lacks ListOrdersPages:\n%s", pages)
	}
	if strings.Contains(string(pages), "GetOrderPages") {
		t.Errorf("single resource operation got a Pages method")
	}

	// Nothing but the generated files may be written.
	entries, _ := os.ReadDir(filepath.Dir(out))
	if len(entries) != 3 {
		t.Errorf("unexpected files in output dir: %v", entries)
	}
}

func TestRunHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "client.gen.go")
	if err := run(context.Background(), srv.Client(), srv.URL, testOutputs(out), "ottomarket"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("output written despite error")
	}
}

func testOutputs(client string) outputs {
	dir := filepath.Dir(client)
	return outputs{client: client, accept: filepath.Join(dir, "accept.gen.go"), pages: filepath.Join(dir, "pages.gen.go")}
}
