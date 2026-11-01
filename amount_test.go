package ottomarket

import (
	"encoding/json"
	"math"
	"testing"
)

func TestParseAmount(t *testing.T) {
	ok := map[string]Amount{
		"0": 0, "7": 700, "12.3": 1230, "12.34": 1234, "12.340": 1234,
		"-0.5": -50, "0.01": 1, "1e2": 10000, "1.23E1": 1230,
		"92233720368547758.07": math.MaxInt64,
	}
	for in, want := range ok {
		got, err := ParseAmount(in)
		if err != nil || got != want {
			t.Errorf("ParseAmount(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "1.234", "0.001", "NaN", "Inf", "92233720368547758.08"} {
		if _, err := ParseAmount(in); err == nil {
			t.Errorf("ParseAmount(%q) succeeded", in)
		}
	}
}

func TestAmountString(t *testing.T) {
	for a, want := range map[Amount]string{
		0: "0.00", 1: "0.01", 1234: "12.34", -50: "-0.50", -1234: "-12.34",
		math.MinInt64: "-92233720368547758.08",
	} {
		if got := a.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", int64(a), got, want)
		}
	}
}

func TestAmountJSON(t *testing.T) {
	var v struct {
		A Amount  `json:"a"`
		B *Amount `json:"b,omitempty"`
	}
	if err := json.Unmarshal([]byte(`{"a": 19.99, "b": 0.1}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.A != 1999 || v.B == nil || *v.B != 10 {
		t.Fatalf("got %d %v", v.A, v.B)
	}
	out, _ := json.Marshal(v)
	if string(out) != `{"a":19.99,"b":0.10}` {
		t.Fatalf("marshal = %s", out)
	}
	for _, in := range []string{`{"a": 1.999}`, `{"a": "1.99"}`, `{"a": true}`} {
		if err := json.Unmarshal([]byte(in), &v); err == nil {
			t.Errorf("Unmarshal(%s) succeeded", in)
		}
	}
}
