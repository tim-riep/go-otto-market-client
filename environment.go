package ottomarket

import (
	"fmt"
	"strings"
)

// Environment selects the OTTO Market system the client talks to.
type Environment int

const (
	// Production is the live OTTO Market API.
	Production Environment = iota
	// Sandbox is the OTTO Market test system.
	Sandbox
)

var environments = [...]struct{ name, baseURL string }{
	Production: {"production", "https://api.otto.market"},
	Sandbox:    {"sandbox", "https://sandbox.api.otto.market"},
}

func (e Environment) valid() bool { return e >= 0 && int(e) < len(environments) }

// BaseURL returns the base URL of the API, e.g. "https://api.otto.market".
func (e Environment) BaseURL() string {
	if !e.valid() {
		return ""
	}
	return environments[e].baseURL
}

// TokenURL returns the OAuth2 token endpoint of the environment.
func (e Environment) TokenURL() string {
	if !e.valid() {
		return ""
	}
	return environments[e].baseURL + "/v1/token"
}

// String returns "production" or "sandbox".
func (e Environment) String() string {
	if !e.valid() {
		return fmt.Sprintf("Environment(%d)", int(e))
	}
	return environments[e].name
}

// ParseEnvironment parses "production" or "sandbox" (case-insensitive),
// for example from a configuration file or environment variable.
func ParseEnvironment(s string) (Environment, error) {
	for i, env := range environments {
		if strings.EqualFold(strings.TrimSpace(s), env.name) {
			return Environment(i), nil
		}
	}
	return 0, fmt.Errorf("ottomarket: unknown environment %q (want \"production\" or \"sandbox\")", s)
}

// MarshalText implements encoding.TextMarshaler.
func (e Environment) MarshalText() ([]byte, error) {
	if !e.valid() {
		return nil, fmt.Errorf("ottomarket: invalid environment %d", int(e))
	}
	return []byte(e.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (e *Environment) UnmarshalText(b []byte) error {
	v, err := ParseEnvironment(string(b))
	if err != nil {
		return err
	}
	*e = v
	return nil
}
