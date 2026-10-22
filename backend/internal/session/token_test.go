package session

import (
	"encoding/base64"
	"errors"
	"testing"
)

func TestRandomTokenSourceProducesURLSafe256BitToken(t *testing.T) {
	t.Parallel()

	source := NewRandomTokenSource()
	token, err := source.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("token is not raw URL-safe base64: %v", err)
	}
	if len(decoded) != tokenByteLength {
		t.Fatalf("decoded token length = %d, want %d", len(decoded), tokenByteLength)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

func TestRandomTokenSourcePropagatesEntropyFailure(t *testing.T) {
	t.Parallel()

	source := &RandomTokenSource{random: failingReader{}}
	if _, err := source.Generate(); err == nil {
		t.Fatal("Generate() error = nil, want entropy error")
	}
}
