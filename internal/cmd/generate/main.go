// Command generate downloads the OTTO Market OpenAPI document, removes all
// prose from it (descriptions, summaries, titles, examples, ...) and
// generates the Go client from the remaining technical structure.
//
// The OpenAPI document is only held in memory. It is never written to disk
// and must never be committed to this repository.
//
// Usage (normally via go generate in the repository root):
//
//	go run ./internal/cmd/generate -out client.gen.go -package ottomarket
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oapi-codegen/oapi-codegen/v2/pkg/codegen"

	"github.com/tim-riep/go-otto-market-client/internal/specstrip"
)

const (
	defaultURL  = "https://api.otto.market/docs/openapi.json"
	maxSpecSize = 64 << 20
)

func main() {
	var (
		specURL = flag.String("url", defaultURL, "URL of the OpenAPI document (JSON)")
		out     = flag.String("out", "client.gen.go", "output file for the generated client")
		accept  = flag.String("accept-out", "accept.gen.go", "output file for the Accept header table")
		pagesF  = flag.String("pages-out", "pages.gen.go", "output file for the pagination methods")
		pkg     = flag.String("package", "ottomarket", "Go package name of the generated code")
		timeout = flag.Duration("timeout", 60*time.Second, "timeout for downloading the OpenAPI document")
	)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if err := run(ctx, http.DefaultClient, *specURL, outputs{client: *out, accept: *accept, pages: *pagesF}, *pkg); err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

// outputs are the files written by the generator.
type outputs struct {
	client, accept, pages string
}

func run(ctx context.Context, client *http.Client, specURL string, out outputs, pkg string) error {
	raw, err := fetch(ctx, client, specURL)
	if err != nil {
		return err
	}

	stripped, err := specstrip.Strip(raw)
	if err != nil {
		return err
	}

	files, err := generate(stripped, pkg)
	if err != nil {
		return err
	}
	for _, f := range []struct {
		path string
		data []byte
	}{{out.client, files.client}, {out.accept, files.accept}, {out.pages, files.pages}} {
		if err := writeFile(f.path, f.data); err != nil {
			return err
		}
	}
	return nil
}

type generated struct {
	client, accept, pages []byte
}

func fetch(ctx context.Context, client *http.Client, specURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, specURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", specURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: unexpected status %s", specURL, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSpecSize+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", specURL, err)
	}
	if len(body) > maxSpecSize {
		return nil, fmt.Errorf("download %s: document larger than %d bytes", specURL, maxSpecSize)
	}
	return body, nil
}

func generate(spec []byte, pkg string) (*generated, error) {
	loader := openapi3.NewLoader()
	// External references would pull in documents that were not stripped.
	loader.IsExternalRefsAllowed = false
	doc, err := loader.LoadFromData(spec)
	if err != nil {
		return nil, fmt.Errorf("load OpenAPI document: %w", err)
	}
	removeAuthorizationParams(doc)
	useAmountType(doc)

	code, err := codegen.Generate(doc, codegen.Configuration{
		PackageName: pkg,
		Generate: codegen.GenerateOptions{
			Models: true,
			Client: true,
		},
		OutputOptions: codegen.OutputOptions{
			// oapi-codegen maps "number" without format to float32 by default.
			TypeMapping: &codegen.TypeMapping{
				Number: codegen.FormatMapping{Default: codegen.SimpleTypeSpec{Type: "float64"}},
			},
		},
		AllowRemoteRefsFrom: []string{},
	})
	if err != nil {
		return nil, fmt.Errorf("generate client: %w", err)
	}
	acceptCode, err := generateAccept(doc, pkg)
	if err != nil {
		return nil, fmt.Errorf("generate Accept table: %w", err)
	}
	// Must run after codegen.Generate, which sets up the naming options.
	pagesCode, err := generatePages(doc, pkg)
	if err != nil {
		return nil, fmt.Errorf("generate pagination: %w", err)
	}
	return &generated{client: []byte(code), accept: acceptCode, pages: pagesCode}, nil
}

// writeFile replaces path atomically so that a failed run never leaves a
// half-written client behind.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".client-*.go.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// removeAuthorizationParams drops every "Authorization" header parameter.
// Some operations declare the header as a regular parameter, which would
// let callers set the token by hand. Authentication is handled solely by
// the TokenProvider of the hand-written client (see client.go), so the
// generated code must not expose the header.
func removeAuthorizationParams(doc *openapi3.T) {
	isAuth := func(p *openapi3.ParameterRef) bool {
		return p != nil && p.Value != nil &&
			p.Value.In == openapi3.ParameterInHeader &&
			strings.EqualFold(p.Value.Name, "Authorization")
	}
	filter := func(params openapi3.Parameters) openapi3.Parameters {
		out := params[:0]
		for _, p := range params {
			if !isAuth(p) {
				out = append(out, p)
			}
		}
		return out
	}
	if doc.Paths != nil {
		for _, item := range doc.Paths.Map() {
			item.Parameters = filter(item.Parameters)
			for _, op := range item.Operations() {
				op.Parameters = filter(op.Parameters)
			}
		}
	}
	if doc.Components != nil {
		for name, p := range doc.Components.Parameters {
			if isAuth(p) {
				delete(doc.Components.Parameters, name)
			}
		}
	}
}

// amountGoType is the hand-written Go type for monetary amounts (amount.go).
const amountGoType = "Amount"

// useAmountType maps monetary amounts to the Amount type. A monetary amount
// is a property "amount" of type number next to a property "currency",
// e.g. {"amount": 19.99, "currency": "EUR"}.
func useAmountType(doc *openapi3.T) {
	walkSchemas(doc, func(s *openapi3.Schema) {
		amount, ok := s.Properties["amount"]
		if !ok || amount.Value == nil || !amount.Value.Type.Is(openapi3.TypeNumber) {
			return
		}
		if _, ok := s.Properties["currency"]; !ok {
			return
		}
		if amount.Value.Extensions == nil {
			amount.Value.Extensions = map[string]any{}
		}
		amount.Value.Extensions["x-go-type"] = amountGoType
	})
}

// walkSchemas calls fn for every schema reachable from doc, once each.
func walkSchemas(doc *openapi3.T, fn func(*openapi3.Schema)) {
	seen := map[*openapi3.Schema]bool{}
	var schema func(r *openapi3.SchemaRef)
	schema = func(r *openapi3.SchemaRef) {
		if r == nil || r.Value == nil || seen[r.Value] {
			return
		}
		s := r.Value
		seen[s] = true
		fn(s)
		for _, p := range s.Properties {
			schema(p)
		}
		schema(s.Items)
		schema(s.Not)
		for _, l := range []openapi3.SchemaRefs{s.AllOf, s.AnyOf, s.OneOf} {
			for _, x := range l {
				schema(x)
			}
		}
		schema(s.AdditionalProperties.Schema)
	}
	content := func(c openapi3.Content) {
		for _, m := range c {
			if m != nil {
				schema(m.Schema)
			}
		}
	}
	param := func(p *openapi3.ParameterRef) {
		if p != nil && p.Value != nil {
			schema(p.Value.Schema)
			content(p.Value.Content)
		}
	}
	body := func(b *openapi3.RequestBodyRef) {
		if b != nil && b.Value != nil {
			content(b.Value.Content)
		}
	}
	response := func(r *openapi3.ResponseRef) {
		if r == nil || r.Value == nil {
			return
		}
		content(r.Value.Content)
		for _, h := range r.Value.Headers {
			if h != nil && h.Value != nil {
				schema(h.Value.Schema)
			}
		}
	}

	if c := doc.Components; c != nil {
		for _, s := range c.Schemas {
			schema(s)
		}
		for _, p := range c.Parameters {
			param(p)
		}
		for _, b := range c.RequestBodies {
			body(b)
		}
		for _, r := range c.Responses {
			response(r)
		}
	}
	if doc.Paths != nil {
		for _, item := range doc.Paths.Map() {
			for _, p := range item.Parameters {
				param(p)
			}
			for _, op := range item.Operations() {
				for _, p := range op.Parameters {
					param(p)
				}
				body(op.RequestBody)
				if op.Responses != nil {
					for _, r := range op.Responses.Map() {
						response(r)
					}
				}
			}
		}
	}
}
