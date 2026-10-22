package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	defaultArgonMemory      = 19 * 1024 // KiB; batas minimum Argon2id OWASP.
	defaultArgonIterations  = 2
	defaultArgonParallelism = 1
	defaultArgonSaltLength  = 16
	defaultArgonKeyLength   = 32
	minPasswordBytes        = 8
	maxPasswordBytes        = 1024
)

var (
	ErrPasswordRequired    = errors.New("password is required")
	ErrPasswordTooShort    = errors.New("password is too short")
	ErrPasswordTooLong     = errors.New("password is too long")
	ErrInvalidPasswordHash = errors.New("invalid password hash")
)

// PasswordHasher menangani hashing dan verifikasi kata sandi.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, encodedHash string) (bool, error)
}

// Argon2idHasher menyimpan parameter di dalam hash agar dapat ditingkatkan.
type Argon2idHasher struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
	random      io.Reader
}

func NewArgon2idHasher() *Argon2idHasher {
	return &Argon2idHasher{
		memory:      defaultArgonMemory,
		iterations:  defaultArgonIterations,
		parallelism: defaultArgonParallelism,
		saltLength:  defaultArgonSaltLength,
		keyLength:   defaultArgonKeyLength,
		random:      rand.Reader,
	}
}

func (h *Argon2idHasher) Hash(password string) (string, error) {
	if password == "" {
		return "", ErrPasswordRequired
	}
	if len(password) < minPasswordBytes {
		return "", ErrPasswordTooShort
	}
	if len(password) > maxPasswordBytes {
		return "", ErrPasswordTooLong
	}

	salt := make([]byte, h.saltLength)
	if _, err := io.ReadFull(h.random, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, h.iterations, h.memory, h.parallelism, h.keyLength)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		h.memory,
		h.iterations,
		h.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func (h *Argon2idHasher) Verify(password, encodedHash string) (bool, error) {
	if len(password) > maxPasswordBytes {
		return false, nil
	}

	parameters, salt, expected, err := parseArgon2idHash(encodedHash)
	if err != nil {
		return false, err
	}

	actual := argon2.IDKey(
		[]byte(password),
		salt,
		parameters.iterations,
		parameters.memory,
		parameters.parallelism,
		uint32(len(expected)),
	)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

type argon2Parameters struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

func parseArgon2idHash(encoded string) (argon2Parameters, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return argon2Parameters{}, nil, nil, ErrInvalidPasswordHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return argon2Parameters{}, nil, nil, ErrInvalidPasswordHash
	}
	if parts[2] != fmt.Sprintf("v=%d", version) {
		return argon2Parameters{}, nil, nil, ErrInvalidPasswordHash
	}

	var params argon2Parameters
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.memory, &params.iterations, &params.parallelism); err != nil {
		return argon2Parameters{}, nil, nil, ErrInvalidPasswordHash
	}
	if parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", params.memory, params.iterations, params.parallelism) {
		return argon2Parameters{}, nil, nil, ErrInvalidPasswordHash
	}
	// Batasi parameter untuk mencegah penggunaan sumber daya berlebihan.
	if params.memory < 8*1024 || params.memory > 256*1024 ||
		params.iterations == 0 || params.iterations > 10 ||
		params.parallelism == 0 || params.parallelism > 16 {
		return argon2Parameters{}, nil, nil, ErrInvalidPasswordHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return argon2Parameters{}, nil, nil, ErrInvalidPasswordHash
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return argon2Parameters{}, nil, nil, ErrInvalidPasswordHash
	}

	return params, salt, expected, nil
}
