package database

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

func testKey() []byte {
	return bytes.Repeat([]byte{0x42}, 32)
}

func TestCredentialCipherRoundTrip(t *testing.T) {
	t.Parallel()

	cipher, err := NewCredentialCipher(testKey(), 1)
	if err != nil {
		t.Fatalf("NewCredentialCipher() error = %v", err)
	}
	envelope, version, err := cipher.Encrypt("db-1", []byte("target-password"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1", version)
	}
	if bytes.Contains(envelope, []byte("target-password")) {
		t.Fatal("envelope contains plaintext")
	}
	plaintext, err := cipher.Decrypt("db-1", envelope, version)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if string(plaintext) != "target-password" {
		t.Fatalf("Decrypt() = %q", plaintext)
	}
}

func TestCredentialCipherUsesFreshNonce(t *testing.T) {
	t.Parallel()

	cipher, _ := NewCredentialCipher(testKey(), 1)
	first, _, _ := cipher.Encrypt("db-1", []byte("same"))
	second, _, _ := cipher.Encrypt("db-1", []byte("same"))
	if bytes.Equal(first[:12], second[:12]) || bytes.Equal(first, second) {
		t.Fatal("two encryptions reused the nonce")
	}
}

func TestCredentialCipherRejectsTamperingAndWrongBinding(t *testing.T) {
	t.Parallel()

	cipher, _ := NewCredentialCipher(testKey(), 1)
	envelope, _, _ := cipher.Encrypt("db-1", []byte("secret"))

	if _, err := cipher.Decrypt("db-2", envelope, 1); !errors.Is(err, ErrCredentialCorrupted) {
		t.Errorf("Decrypt(other id) error = %v, want ErrCredentialCorrupted", err)
	}
	tampered := append([]byte(nil), envelope...)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := cipher.Decrypt("db-1", tampered, 1); !errors.Is(err, ErrCredentialCorrupted) {
		t.Errorf("Decrypt(tampered) error = %v, want ErrCredentialCorrupted", err)
	}
	if _, err := cipher.Decrypt("db-1", envelope[:5], 1); !errors.Is(err, ErrCredentialCorrupted) {
		t.Errorf("Decrypt(short) error = %v, want ErrCredentialCorrupted", err)
	}
	if _, err := cipher.Decrypt("db-1", envelope, 2); !errors.Is(err, ErrCredentialKeyVersion) {
		t.Errorf("Decrypt(version 2) error = %v, want ErrCredentialKeyVersion", err)
	}

	otherKey := bytes.Repeat([]byte{0x24}, 32)
	otherCipher, _ := NewCredentialCipher(otherKey, 1)
	if _, err := otherCipher.Decrypt("db-1", envelope, 1); !errors.Is(err, ErrCredentialCorrupted) {
		t.Errorf("Decrypt(other key) error = %v, want ErrCredentialCorrupted", err)
	}
}

func TestLoadCredentialCipher(t *testing.T) {
	t.Parallel()

	encoded := base64.StdEncoding.EncodeToString(testKey())
	env := func(values map[string]string) func(string) (string, bool) {
		return func(key string) (string, bool) {
			value, ok := values[key]
			return value, ok
		}
	}

	cipher, err := LoadCredentialCipher(env(map[string]string{CredentialKeyEnv: encoded}))
	if err != nil || cipher.KeyVersion() != 1 {
		t.Fatalf("LoadCredentialCipher() = %v, %v", cipher, err)
	}
	cipher, err = LoadCredentialCipher(env(map[string]string{
		CredentialKeyEnv:        base64.RawURLEncoding.EncodeToString(testKey()),
		CredentialKeyVersionEnv: "3",
	}))
	if err != nil || cipher.KeyVersion() != 3 {
		t.Fatalf("LoadCredentialCipher(url, v3) = %v, %v", cipher, err)
	}

	if _, err := LoadCredentialCipher(env(nil)); !errors.Is(err, ErrCredentialKeyMissing) {
		t.Errorf("missing key error = %v", err)
	}
	if _, err := LoadCredentialCipher(env(map[string]string{CredentialKeyEnv: "c2hvcnQ="})); !errors.Is(err, ErrCredentialKeyInvalid) {
		t.Errorf("short key error = %v", err)
	}
	if _, err := LoadCredentialCipher(env(map[string]string{CredentialKeyEnv: "%%%"})); !errors.Is(err, ErrCredentialKeyInvalid) {
		t.Errorf("non-base64 key error = %v", err)
	}
	if _, err := LoadCredentialCipher(env(map[string]string{CredentialKeyEnv: encoded, CredentialKeyVersionEnv: "0"})); err == nil {
		t.Error("version 0 must be rejected")
	}
}
