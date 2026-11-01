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
		pkg     = flag.String("package", "ottomarket", "Go package name of the generated code")
		timeout = flag.Duration("timeout", 60*time.Second, "timeout for downloading the OpenAPI document")
	)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if err := run(ctx, http.DefaultClient, *specURL, *out, *pkg); err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, client *http.Client, specURL, out, pkg string) error {
	raw, err := fetch(ctx, client, specURL)
	if err != nil {
		return err
	}

	stripped, err := specstrip.Strip(raw)
	if err != nil {
		return err
	}

	code, err := generate(stripped, pkg)
	if err != nil {
		return err
	}
	return writeFile(out, []byte(code))
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

func generate(spec []byte, pkg string) (string, error) {
	loader := openapi3.NewLoader()
	// External references would pull in documents that were not stripped.
	loader.IsExternalRefsAllowed = false
	doc, err := loader.LoadFromData(spec)
	if err != nil {
		return "", fmt.Errorf("load OpenAPI document: %w", err)
	}
	removeAuthorizationParams(doc)

	code, err := codegen.Generate(doc, codegen.Configuration{
		PackageName: pkg,
		Generate: codegen.GenerateOptions{
			Models: true,
			Client: true,
		},
		AllowRemoteRefsFrom: []string{},
	})
	if err != nil {
		return "", fmt.Errorf("generate client: %w", err)
	}
	return code, nil
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
