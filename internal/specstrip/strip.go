// Package specstrip removes all prose from an OpenAPI document.
//
// The OTTO Market OpenAPI document contains descriptions, summaries, titles
// and examples that are protected by copyright (UrhG). The client generator
// only needs the technical structure of the API (paths, operations,
// parameters, schemas, identifiers), so everything else is dropped before
// the document is handed to the code generator. The document is only ever
// held in memory and never written to disk.
package specstrip

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// proseKeys are the OpenAPI / JSON Schema keywords that carry free text.
// They are removed wherever they appear as a keyword (not as a name).
var proseKeys = map[string]bool{
	"description":    true,
	"summary":        true,
	"title":          true,
	"example":        true,
	"examples":       true,
	"externalDocs":   true,
	"$comment":       true,
	"contact":        true,
	"license":        true,
	"termsOfService": true,
}

// nameMapKeys are keywords whose value is a map from user-chosen names to
// objects. The keys of such a map are identifiers (a property may well be
// called "description"), so they must never be removed. The number is how
// many levels of names follow the keyword.
var nameMapKeys = map[string]int{
	"paths":             1,
	"webhooks":          1,
	"pathItems":         1,
	"schemas":           1,
	"responses":         1,
	"parameters":        1,
	"requestBodies":     1,
	"headers":           1,
	"securitySchemes":   1,
	"links":             1,
	"content":           1,
	"encoding":          1,
	"properties":        1,
	"patternProperties": 1,
	"definitions":       1,
	"$defs":             1,
	"dependentSchemas":  1,
	"variables":         1,
	"mapping":           1,
	"callbacks":         2,
}

// keptExtensions are vendor extensions that influence the generated code
// and contain identifiers or types only. All other x-* extensions are
// dropped because they may contain arbitrary text.
var keptExtensions = map[string]bool{
	"x-client-response-wrapper":          true,
	"x-enum-varnames":                    true,
	"x-enumNames":                        true,
	"x-go-json-ignore":                   true,
	"x-go-name":                          true,
	"x-go-type":                          true,
	"x-go-type-import":                   true,
	"x-go-type-name":                     true,
	"x-go-type-skip-optional-pointer":    true,
	"x-oapi-codegen-extra-tags":          true,
	"x-oapi-codegen-only-honour-go-name": true,
	"x-omitempty":                        true,
	"x-omitzero":                         true,
	"x-order":                            true,
	"x-nullable":                         true,
}

// InfoTitle replaces the title of the stripped document. OpenAPI requires
// info.title, so it cannot simply be removed.
const InfoTitle = "OTTO Market API"

// Strip parses an OpenAPI document in JSON form, removes all prose and
// returns the stripped document as JSON.
func Strip(spec []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(spec))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse OpenAPI document: %w", err)
	}
	root, ok := doc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("OpenAPI document is not a JSON object")
	}
	if _, ok := root["openapi"]; !ok {
		return nil, fmt.Errorf("document has no \"openapi\" field, is it an OpenAPI 3 document?")
	}

	info := map[string]any{"title": InfoTitle, "version": "unknown"}
	if old, ok := root["info"].(map[string]any); ok {
		if v, ok := old["version"]; ok {
			info["version"] = v
		}
	}

	stripValue(root, 0)
	root["info"] = info

	if left := Remaining(root); len(left) > 0 {
		return nil, fmt.Errorf("prose left after stripping: %s", strings.Join(left, ", "))
	}
	return json.Marshal(root)
}

// stripValue removes prose from v in place. names is the number of name
// levels v still consists of (see nameMapKeys).
func stripValue(v any, names int) any {
	switch t := v.(type) {
	case map[string]any:
		if names > 0 {
			for k, child := range t {
				t[k] = stripValue(child, names-1)
			}
			return t
		}
		for k, child := range t {
			switch {
			case proseKeys[k]:
				delete(t, k)
			case strings.HasPrefix(k, "x-") && !keptExtensions[k]:
				delete(t, k)
			case k == "scopes":
				// OAuth flow scopes: scope name -> description.
				if m, ok := child.(map[string]any); ok {
					for s := range m {
						m[s] = ""
					}
				}
			case k == "security":
				// Array of security requirements: scheme name -> scopes.
				if arr, ok := child.([]any); ok {
					for i, req := range arr {
						arr[i] = stripValue(req, 1)
					}
				}
			default:
				t[k] = stripValue(child, nameMapKeys[k])
			}
		}
		return t
	case []any:
		// Array elements are never names themselves (for example the
		// "parameters" array of an operation holds parameter objects).
		for i, child := range t {
			t[i] = stripValue(child, 0)
		}
		return t
	default:
		return v
	}
}

// Remaining lists the JSON paths of prose keywords still present in doc.
// It applies the same rules as the stripper and is used as a safety net.
func Remaining(doc any) []string {
	var out []string
	var walk func(v any, path string, names int)
	walk = func(v any, path string, names int) {
		switch t := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				p := path + "/" + k
				if names > 0 {
					walk(t[k], p, names-1)
					continue
				}
				if proseKeys[k] && p != "/info/title" {
					out = append(out, p)
					continue
				}
				if strings.HasPrefix(k, "x-") && !keptExtensions[k] {
					out = append(out, p)
					continue
				}
				if k == "scopes" {
					if m, ok := t[k].(map[string]any); ok {
						for s, d := range m {
							if d != "" {
								out = append(out, p+"/"+s)
							}
						}
					}
					continue
				}
				if k == "security" {
					if arr, ok := t[k].([]any); ok {
						for i, req := range arr {
							walk(req, fmt.Sprintf("%s/%d", p, i), 1)
						}
					}
					continue
				}
				walk(t[k], p, nameMapKeys[k])
			}
		case []any:
			for i, child := range t {
				walk(child, fmt.Sprintf("%s/%d", path, i), 0)
			}
		}
	}
	walk(doc, "", 0)
	return out
}
