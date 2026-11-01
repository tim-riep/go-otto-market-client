package main

import (
	"context"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
    }
  },
  "components": {"parameters": {"Auth": {"name": "authorization", "in": "header", "schema": {"type": "string"}}},
    "schemas": {"Order": {"type": "object", "description": "PROSE",
    "properties": {"description": {"type": "string", "description": "PROSE", "example": "PROSE"}}}}}
}`

func TestRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(testSpec))
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "client.gen.go")
	if err := run(context.Background(), srv.Client(), srv.URL, out, "ottomarket"); err != nil {
		t.Fatal(err)
	}
	code, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(code), "PROSE") {
		t.Fatalf("prose in generated code")
	}
	for _, want := range []string{"func (c *Client) GetOrder(", "type Order struct", "Description *string"} {
		if !strings.Contains(string(code), want) {
			t.Errorf("generated code lacks %q", want)
		}
	}
	if strings.Contains(string(code), "Authorization") || strings.Contains(string(code), "authorization") {
		t.Errorf("generated code exposes the Authorization header")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), out, code, 0); err != nil {
		t.Fatalf("generated code does not parse: %v", err)
	}

	// Nothing but the generated client may be written.
	entries, _ := os.ReadDir(filepath.Dir(out))
	if len(entries) != 1 {
		t.Errorf("unexpected files in output dir: %v", entries)
	}
}

func TestRunHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "client.gen.go")
	if err := run(context.Background(), srv.Client(), srv.URL, out, "ottomarket"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("output written despite error")
	}
}
