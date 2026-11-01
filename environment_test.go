package ottomarket

import (
	"encoding/json"
	"testing"
)

func TestEnvironment(t *testing.T) {
	tests := []struct {
		env             Environment
		name, base, tok string
	}{
		{Production, "production", "https://api.otto.market", "https://api.otto.market/v1/token"},
		{Sandbox, "sandbox", "https://sandbox.api.otto.market", "https://sandbox.api.otto.market/v1/token"},
	}
	for _, tt := range tests {
		if tt.env.String() != tt.name || tt.env.BaseURL() != tt.base || tt.env.TokenURL() != tt.tok {
			t.Errorf("%d: got %s %s %s", tt.env, tt.env, tt.env.BaseURL(), tt.env.TokenURL())
		}
		got, err := ParseEnvironment(" " + tt.name + " ")
		if err != nil || got != tt.env {
			t.Errorf("ParseEnvironment(%q) = %v, %v", tt.name, got, err)
		}
	}
	if _, err := ParseEnvironment("staging"); err == nil {
		t.Error("ParseEnvironment accepted staging")
	}
	if Environment(9).BaseURL() != "" {
		t.Error("invalid environment has a base URL")
	}
}

func TestEnvironmentJSON(t *testing.T) {
	var cfg struct{ Env Environment }
	if err := json.Unmarshal([]byte(`{"Env":"Sandbox"}`), &cfg); err != nil || cfg.Env != Sandbox {
		t.Fatalf("unmarshal: %v %v", cfg.Env, err)
	}
	b, err := json.Marshal(cfg)
	if err != nil || string(b) != `{"Env":"sandbox"}` {
		t.Fatalf("marshal: %s %v", b, err)
	}
}
