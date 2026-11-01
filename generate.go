// Package ottomarket is a Go client for the OTTO Market API.
//
// The client in client.gen.go is generated from the live OpenAPI document
// at https://api.otto.market/docs/openapi.json. All descriptions and other
// prose are removed before generation; the document itself is never stored
// in this repository. Regenerate with:
//
//	go generate ./...
package ottomarket

//go:generate go run ./internal/cmd/generate -out client.gen.go -accept-out accept.gen.go -package ottomarket
