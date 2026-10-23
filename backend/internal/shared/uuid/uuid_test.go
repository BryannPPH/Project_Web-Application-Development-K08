package uuid

import (
	"bytes"
	"testing"
)

func TestValid(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4b": true,
		"6F1C2A3B-4D5E-4F60-8A7B-9C0D1E2F3A4B": true,
		"":                                     false,
		"not-a-uuid":                           false,
		"6f1c2a3b4d5e4f608a7b9c0d1e2f3a4b":     false,
		"6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4g": false,
		"6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4'": false,
		"6f1c2a3b_4d5e-4f60-8a7b-9c0d1e2f3a4b": false,
	}
	for value, want := range cases {
		if got := Valid(value); got != want {
			t.Errorf("Valid(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestNewProducesVersion4(t *testing.T) {
	t.Parallel()

	value, err := newFrom(bytes.NewReader(bytes.Repeat([]byte{0xff}, 16)))
	if err != nil {
		t.Fatalf("newFrom() error = %v", err)
	}
	if !Valid(value) {
		t.Fatalf("newFrom() = %q, want valid uuid", value)
	}
	if value[14] != '4' {
		t.Fatalf("version nibble = %q, want 4", value[14])
	}
	if value[19] != 'b' {
		t.Fatalf("variant nibble = %q, want b", value[19])
	}

	first, _ := New()
	second, _ := New()
	if first == second {
		t.Fatal("New() returned duplicate values")
	}
}

func TestNewPropagatesRandomError(t *testing.T) {
	t.Parallel()

	if _, err := newFrom(bytes.NewReader(nil)); err == nil {
		t.Fatal("newFrom() error = nil, want error")
	}
}
