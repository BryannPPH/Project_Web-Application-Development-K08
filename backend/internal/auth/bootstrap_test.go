package auth

import (
	"context"
	"errors"
	"testing"
)

type bootstrapStoreStub struct {
	username     string
	passwordHash string
	err          error
}

func (s *bootstrapStoreStub) CreateInitialAdmin(_ context.Context, username, passwordHash string) error {
	s.username = username
	s.passwordHash = passwordHash
	return s.err
}

type bootstrapPasswordHasherStub struct {
	hashInput string
	hash      string
	err       error
}

func (s *bootstrapPasswordHasherStub) Hash(password string) (string, error) {
	s.hashInput = password
	return s.hash, s.err
}

func (s *bootstrapPasswordHasherStub) Verify(string, string) (bool, error) {
	return false, nil
}

func TestBootstrapperCreatesTrimmedAdminWithPasswordHash(t *testing.T) {
	t.Parallel()

	store := &bootstrapStoreStub{}
	passwords := &bootstrapPasswordHasherStub{hash: "encoded-password-hash"}
	bootstrapper := newBootstrapper(store, passwords)

	if err := bootstrapper.CreateInitialAdmin(context.Background(), "  administrator  ", "secret-password"); err != nil {
		t.Fatalf("CreateInitialAdmin() error = %v", err)
	}
	if passwords.hashInput != "secret-password" {
		t.Fatalf("hashed password = %q, want original password", passwords.hashInput)
	}
	if store.username != "administrator" {
		t.Fatalf("stored username = %q, want administrator", store.username)
	}
	if store.passwordHash != "encoded-password-hash" {
		t.Fatalf("stored password = %q, want encoded hash", store.passwordHash)
	}
}

func TestBootstrapperRejectsInvalidUsernameBeforeHashing(t *testing.T) {
	t.Parallel()

	store := &bootstrapStoreStub{}
	passwords := &bootstrapPasswordHasherStub{hash: "encoded-password-hash"}
	bootstrapper := newBootstrapper(store, passwords)

	err := bootstrapper.CreateInitialAdmin(context.Background(), "ab", "secret-password")
	if !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("CreateInitialAdmin() error = %v, want ErrInvalidUsername", err)
	}
	if passwords.hashInput != "" || store.username != "" {
		t.Fatal("invalid username reached password hasher or store")
	}
}

func TestBootstrapperPropagatesHashAndStoreErrors(t *testing.T) {
	t.Parallel()

	hashError := errors.New("hash failed")
	bootstrapper := newBootstrapper(
		&bootstrapStoreStub{},
		&bootstrapPasswordHasherStub{err: hashError},
	)
	if err := bootstrapper.CreateInitialAdmin(context.Background(), "administrator", "secret-password"); !errors.Is(err, hashError) {
		t.Fatalf("hash error = %v, want %v", err, hashError)
	}

	storeError := ErrPlatformAdminExists
	bootstrapper = newBootstrapper(
		&bootstrapStoreStub{err: storeError},
		&bootstrapPasswordHasherStub{hash: "encoded-password-hash"},
	)
	if err := bootstrapper.CreateInitialAdmin(context.Background(), "administrator", "secret-password"); !errors.Is(err, storeError) {
		t.Fatalf("store error = %v, want %v", err, storeError)
	}
}
