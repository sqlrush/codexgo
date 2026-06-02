package paritytest

import "testing"

func TestCanonicalizeJSON_SortsKeysAndNormalizesWhitespace(t *testing.T) {
	a, err := CanonicalizeJSON([]byte(`{ "b": 1,  "a": 2 }`))
	if err != nil {
		t.Fatalf("canonicalize a: %v", err)
	}
	b, err := CanonicalizeJSON([]byte(`{"a":2,"b":1}`))
	if err != nil {
		t.Fatalf("canonicalize b: %v", err)
	}
	if string(a) != string(b) {
		t.Fatalf("expected canonical forms to match: %q vs %q", a, b)
	}
	if want := `{"a":2,"b":1}`; string(a) != want {
		t.Fatalf("canonical form = %q, want %q", a, want)
	}
}

func TestCanonicalizeJSON_RejectsInvalid(t *testing.T) {
	if _, err := CanonicalizeJSON([]byte(`{not json`)); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}
