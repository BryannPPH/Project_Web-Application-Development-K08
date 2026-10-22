package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeUserRepository struct {
	record UserRecord
	err    error
}

func (f fakeUserRepository) FindByUsername(context.Context, string) (UserRecord, error) {
	return f.record, f.err
}

type fakePasswordHasher struct {
	verifyResult bool
	verifyCalls  int
}

func (f *fakePasswordHasher) Hash(string) (string, error) {
	return "dummy-hash", nil
}

func (f *fakePasswordHasher) Verify(string, string) (bool, error) {
	f.verifyCalls++
	return f.verifyResult, nil
}

type fakeSessionIssuer struct {
	created   bool
	principal Principal
}

func (f *fakeSessionIssuer) Create(_ context.Context, principal Principal) (SessionCredentials, error) {
	f.created = true
	f.principal = principal
	return SessionCredentials{
		SessionToken:      "session-token",
		CSRFToken:         "csrf-token",
		AbsoluteExpiresAt: time.Unix(2000, 0),
	}, nil
}

func TestLoginCreatesSessionForActiveUser(t *testing.T) {
	t.Parallel()

	passwords := &fakePasswordHasher{verifyResult: true}
	sessions := &fakeSessionIssuer{}
	service, err := NewService(fakeUserRepository{record: UserRecord{
		ID:           "user-id",
		Username:     "alice",
		PasswordHash: "stored-hash",
		PlatformRole: PlatformRoleUser,
		Active:       true,
	}}, passwords, sessions)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := service.Login(context.Background(), " alice ", "password")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if !sessions.created || sessions.principal.UserID != "user-id" {
		t.Fatalf("session principal = %+v, want user-id", sessions.principal)
	}
	if result.Session.SessionToken != "session-token" {
		t.Errorf("session token = %q, want session-token", result.Session.SessionToken)
	}
}

func TestLoginHidesUnknownAndInactiveUsers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		repository fakeUserRepository
	}{
		{name: "unknown", repository: fakeUserRepository{err: ErrUserNotFound}},
		{name: "inactive", repository: fakeUserRepository{record: UserRecord{
			ID: "user-id", Username: "alice", PasswordHash: "hash", PlatformRole: PlatformRoleUser, Active: false,
		}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			passwords := &fakePasswordHasher{verifyResult: true}
			sessions := &fakeSessionIssuer{}
			service, err := NewService(tt.repository, passwords, sessions)
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}

			_, err = service.Login(context.Background(), "alice", "password")
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
			}
			if sessions.created {
				t.Fatal("Login() created a session for rejected credentials")
			}
			if passwords.verifyCalls != 1 {
				t.Fatalf("Verify() calls = %d, want 1", passwords.verifyCalls)
			}
		})
	}
}
