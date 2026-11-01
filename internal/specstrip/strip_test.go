package specstrip

import (
	"encoding/json"
	"strings"
	"testing"
)

// testSpec is a synthetic document. Every piece of prose contains the
// marker PROSE so the test can check that none of it survives.
const testSpec = `{
  "openapi": "3.0.3",
  "info": {"title": "PROSE title", "version": "1.2.3", "description": "PROSE",
           "contact": {"name": "PROSE"}, "termsOfService": "PROSE"},
  "servers": [{"url": "https://example.test", "description": "PROSE"}],
  "tags": [{"name": "orders", "description": "PROSE", "externalDocs": {"url": "https://x", "description": "PROSE"}}],
  "x-tagGroups": [{"name": "PROSE"}],
  "paths": {
    "/description": {
      "summary": "PROSE",
      "get": {
        "operationId": "getDescription",
        "summary": "PROSE",
        "description": "PROSE",
        "x-deprecated-reason": "PROSE",
        "security": [{"description": ["scope:read"]}],
        "parameters": [{"name": "summary", "in": "query", "description": "PROSE",
                        "example": "PROSE", "schema": {"type": "string", "title": "PROSE"}}],
        "responses": {
          "200": {"description": "PROSE",
                  "content": {"application/json": {"schema": {"$ref": "#/components/schemas/description"},
                                                   "examples": {"one": {"summary": "PROSE", "value": "PROSE"}}}}}
        },
        "callbacks": {"description": {"{$request.body#/url}": {"post": {"summary": "PROSE",
          "responses": {"200": {"description": "PROSE"}}}}}}
      }
    }
  },
  "components": {
    "schemas": {
      "description": {
        "type": "object",
        "description": "PROSE",
        "required": ["description", "title"],
        "properties": {
          "description": {"type": "string", "description": "PROSE", "example": "PROSE"},
          "title": {"type": "string", "enum": ["A", "B"], "x-enum-varnames": ["EnumA", "EnumB"]},
          "examples": {"type": "array", "items": {"type": "integer", "description": "PROSE"}}
        }
      }
    },
    "examples": {"one": {"summary": "PROSE", "value": {"a": "PROSE"}}},
    "securitySchemes": {
      "description": {"type": "oauth2", "description": "PROSE",
        "flows": {"clientCredentials": {"tokenUrl": "https://example.test/token",
                                        "scopes": {"scope:read": "PROSE"}}}}
    }
  }
}`

func TestStrip(t *testing.T) {
	out, err := Strip([]byte(testSpec))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "PROSE") {
		t.Fatalf("prose left in stripped document:\n%s", out)
	}

	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}

	// Names that look like prose keywords must survive.
	mustHave := []string{
		"/paths//description/get/operationId",
		"/paths//description/get/parameters/0/name",
		"/paths//description/get/callbacks/description",
		"/components/schemas/description/properties/description",
		"/components/schemas/description/properties/title/enum",
		"/components/schemas/description/properties/title/x-enum-varnames",
		"/components/schemas/description/properties/examples/items/type",
		"/components/securitySchemes/description/flows/clientCredentials/scopes/scope:read",
		"/info/version",
		"/info/title",
		"/servers/0/url",
		"/tags/0/name",
	}
	for _, p := range mustHave {
		if _, ok := lookup(doc, p); !ok {
			t.Errorf("missing %s", p)
		}
	}
	if v, _ := lookup(doc, "/info/version"); v != "1.2.3" {
		t.Errorf("info.version = %v", v)
	}
	if v, _ := lookup(doc, "/info/title"); v != InfoTitle {
		t.Errorf("info.title = %v", v)
	}
	sec, _ := lookup(doc, "/paths//description/get/security/0/description")
	if sec == nil {
		t.Errorf("security requirement named description was removed")
	}

	mustNotHave := []string{
		"/components/examples",
		"/x-tagGroups",
		"/paths//description/get/x-deprecated-reason",
		"/tags/0/externalDocs",
	}
	for _, p := range mustNotHave {
		if _, ok := lookup(doc, p); ok {
			t.Errorf("unexpected %s", p)
		}
	}
}

func TestStripRejectsNonOpenAPI(t *testing.T) {
	for _, in := range []string{`[]`, `{"swagger": "2.0"}`, `not json`} {
		if _, err := Strip([]byte(in)); err == nil {
			t.Errorf("Strip(%q) succeeded", in)
		}
	}
}

// lookup resolves a slash separated path. Map keys may be empty or contain
// slashes ("/paths//description" addresses the path "/description").
func lookup(doc any, path string) (any, bool) {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	cur := doc
	for i := 0; i < len(parts); i++ {
		switch t := cur.(type) {
		case map[string]any:
			key := parts[i]
			if key == "" && i+1 < len(parts) {
				key = "/" + parts[i+1]
				i++
			}
			v, ok := t[key]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			var idx int
			if _, err := fmtSscan(parts[i], &idx); err != nil || idx >= len(t) {
				return nil, false
			}
			cur = t[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

func TestRemainingFindsProse(t *testing.T) {
	var doc any
	if err := json.Unmarshal([]byte(testSpec), &doc); err != nil {
		t.Fatal(err)
	}
	left := strings.Join(Remaining(doc), "\n")
	for _, want := range []string{
		"/paths//description/get/parameters/0/description",
		"/components/schemas/description/properties/description/example",
		"/components/securitySchemes/description/flows/clientCredentials/scopes/scope:read",
	} {
		if !strings.Contains(left, want) {
			t.Errorf("Remaining did not report %s", want)
		}
	}
}
