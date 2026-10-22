package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestArgon2idHasherRoundTrip(t *testing.T) {
	t.Parallel()

	hasher := NewArgon2idHasher()
	encoded, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if strings.Contains(encoded, "correct horse battery staple") {
		t.Fatal("encoded hash contains plaintext password")
	}

	matches, err := hasher.Verify("correct horse battery staple", encoded)
	if err != nil {
		t.Fatalf("Verify(correct) error = %v", err)
	}
	if !matches {
		t.Fatal("Verify(correct) = false, want true")
	}

	matches, err = hasher.Verify("wrong password", encoded)
	if err != nil {
		t.Fatalf("Verify(wrong) error = %v", err)
	}
	if matches {
		t.Fatal("Verify(wrong) = true, want false")
	}
}

func TestArgon2idHasherUsesUniqueSalt(t *testing.T) {
	t.Parallel()

	hasher := NewArgon2idHasher()
	first, err := hasher.Hash("same password")
	if err != nil {
		t.Fatalf("first Hash() error = %v", err)
	}
	second, err := hasher.Hash("same password")
	if err != nil {
		t.Fatalf("second Hash() error = %v", err)
	}
	if first == second {
		t.Fatal("two hashes with independent salts are equal")
	}
}

func TestArgon2idHasherRejectsMalformedOrOversizedInput(t *testing.T) {
	t.Parallel()

	hasher := NewArgon2idHasher()
	if _, err := hasher.Hash(""); !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("Hash(empty) error = %v, want ErrPasswordRequired", err)
	}
	if _, err := hasher.Hash("short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("Hash(short) error = %v, want ErrPasswordTooShort", err)
	}
	if _, err := hasher.Hash(strings.Repeat("x", maxPasswordBytes+1)); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("Hash(oversized) error = %v, want ErrPasswordTooLong", err)
	}
	if _, err := hasher.Verify("password", "$argon2id$v=19$m=19456,t=2,p=1garbage$c2FsdHNhbHQ$aGFzaGhhc2hoYXNoaGFzaA"); !errors.Is(err, ErrInvalidPasswordHash) {
		t.Fatalf("Verify(malformed) error = %v, want ErrInvalidPasswordHash", err)
	}
}
