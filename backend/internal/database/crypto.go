package database

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	// CredentialKeyEnv berisi master key 32 byte dalam base64.
	CredentialKeyEnv = "MANAGED_DB_CREDENTIAL_KEY"
	// CredentialKeyVersionEnv berisi versi key aktif (default 1).
	CredentialKeyVersionEnv = "MANAGED_DB_CREDENTIAL_KEY_VERSION"

	credentialKeyBytes = 32
	// credentialAADPrefix mengikat ciphertext ke ID managed database. Ciphertext
	// yang dipindah ke baris lain tidak akan lolos autentikasi GCM.
	credentialAADPrefix = "dbmp/managed-database-credential/v1:"
)

var (
	ErrCredentialKeyMissing = errors.New(CredentialKeyEnv + " is required")
	ErrCredentialKeyInvalid = errors.New(CredentialKeyEnv + " must be base64 encoding of exactly 32 bytes")
	ErrCredentialKeyVersion = errors.New("credential was encrypted with an unknown key version")
	ErrCredentialCorrupted  = errors.New("credential ciphertext failed authentication")
)

// CredentialCipher mengenkripsi password target dengan AES-256-GCM.
//
// Envelope yang disimpan di credential_ciphertext: nonce(12) || ciphertext || tag(16).
// Nonce acak baru dibuat untuk setiap enkripsi.
type CredentialCipher struct {
	aead    cipher.AEAD
	version int16
	random  io.Reader
}

// NewCredentialCipher membuat cipher dari key 32 byte dan versi key.
func NewCredentialCipher(key []byte, version int16) (*CredentialCipher, error) {
	if len(key) != credentialKeyBytes {
		return nil, ErrCredentialKeyInvalid
	}
	if version <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer", CredentialKeyVersionEnv)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrCredentialKeyInvalid
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrCredentialKeyInvalid
	}
	return &CredentialCipher{aead: aead, version: version, random: rand.Reader}, nil
}

// ParseCredentialKey menerima base64 standar atau URL-safe, dengan/tanpa padding.
func ParseCredentialKey(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, ErrCredentialKeyMissing
	}
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		key, err := encoding.DecodeString(encoded)
		if err == nil && len(key) == credentialKeyBytes {
			return key, nil
		}
	}
	return nil, ErrCredentialKeyInvalid
}

// LoadCredentialCipher membaca key dari environment. Startup harus gagal bila
// error dikembalikan, karena fitur managed database tidak aman tanpa key valid.
func LoadCredentialCipher(lookup func(string) (string, bool)) (*CredentialCipher, error) {
	raw, _ := lookup(CredentialKeyEnv)
	key, err := ParseCredentialKey(raw)
	if err != nil {
		return nil, err
	}
	version := int16(1)
	if rawVersion, ok := lookup(CredentialKeyVersionEnv); ok && strings.TrimSpace(rawVersion) != "" {
		parsed, err := strconv.ParseInt(strings.TrimSpace(rawVersion), 10, 16)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("%s must be a positive integer", CredentialKeyVersionEnv)
		}
		version = int16(parsed)
	}
	return NewCredentialCipher(key, version)
}

// KeyVersion mengembalikan versi key aktif.
func (c *CredentialCipher) KeyVersion() int16 {
	return c.version
}

// Encrypt mengenkripsi plaintext dan mengikatnya ke databaseID melalui AAD.
func (c *CredentialCipher) Encrypt(databaseID string, plaintext []byte) ([]byte, int16, error) {
	nonce := make([]byte, c.aead.NonceSize(), c.aead.NonceSize()+len(plaintext)+c.aead.Overhead())
	if _, err := io.ReadFull(c.random, nonce); err != nil {
		return nil, 0, fmt.Errorf("generate credential nonce: %w", err)
	}
	envelope := c.aead.Seal(nonce, nonce, plaintext, associatedData(databaseID))
	return envelope, c.version, nil
}

// Decrypt membuka envelope. Error tidak pernah memuat plaintext atau key.
func (c *CredentialCipher) Decrypt(databaseID string, envelope []byte, version int16) ([]byte, error) {
	if version != c.version {
		return nil, ErrCredentialKeyVersion
	}
	nonceSize := c.aead.NonceSize()
	if len(envelope) < nonceSize+c.aead.Overhead() {
		return nil, ErrCredentialCorrupted
	}
	plaintext, err := c.aead.Open(nil, envelope[:nonceSize], envelope[nonceSize:], associatedData(databaseID))
	if err != nil {
		return nil, ErrCredentialCorrupted
	}
	return plaintext, nil
}

func associatedData(databaseID string) []byte {
	return []byte(credentialAADPrefix + databaseID)
}
