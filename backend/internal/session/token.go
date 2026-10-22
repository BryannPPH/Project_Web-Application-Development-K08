package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

const tokenByteLength = 32

type TokenSource interface {
	Generate() (string, error)
}

// RandomTokenSource menghasilkan token acak 256-bit yang aman untuk URL.
type RandomTokenSource struct {
	random io.Reader
}

func NewRandomTokenSource() *RandomTokenSource {
	return &RandomTokenSource{random: rand.Reader}
}

func (s *RandomTokenSource) Generate() (string, error) {
	value := make([]byte, tokenByteLength)
	if _, err := io.ReadFull(s.random, value); err != nil {
		return "", fmt.Errorf("generate secure token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashToken(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return digest[:]
}
