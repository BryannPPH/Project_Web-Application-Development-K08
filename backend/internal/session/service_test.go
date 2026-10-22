package session

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
)

type fakeClock struct {
	now time.Time
}

func (f fakeClock) Now() time.Time { return f.now }

type queuedTokenSource struct {
	tokens []string
}

func (q *queuedTokenSource) Generate() (string, error) {
	if len(q.tokens) == 0 {
		return "", errors.New("no test token available")
	}
	token := q.tokens[0]
	q.tokens = q.tokens[1:]
	return token, nil
}

type fakeRepository struct {
	record          Record
	findErr         error
	createParams    CreateParams
	created         bool
	rotateParams    RotateParams
	rotateResult    bool
	touchResult     bool
	rotatedCSRFHash []byte
	revokeSessionID string
	revokeAllUserID string
}

func (f *fakeRepository) Create(_ context.Context, params CreateParams) (string, error) {
	f.created = true
	f.createParams = params
	return "session-id", nil
}

func (f *fakeRepository) FindByTokenHash(context.Context, []byte, time.Time) (Record, error) {
	return f.record, f.findErr
}

func (f *fakeRepository) Rotate(_ context.Context, params RotateParams) (bool, error) {
	f.rotateParams = params
	return f.rotateResult, nil
}

func (f *fakeRepository) Touch(context.Context, string, time.Time) (bool, error) {
	return f.touchResult, nil
}

func (f *fakeRepository) RotateCSRF(_ context.Context, _ string, csrfHash []byte, _ time.Time) (bool, error) {
	f.rotatedCSRFHash = append([]byte(nil), csrfHash...)
	return true, nil
}

func (f *fakeRepository) Revoke(_ context.Context, sessionID string, _ time.Time) error {
	f.revokeSessionID = sessionID
	return nil
}

func (f *fakeRepository) RevokeAllForUser(_ context.Context, userID string, _ time.Time) error {
	f.revokeAllUserID = userID
	return nil
}

func testConfig() Config {
	return Config{
		IdleTimeout:        30 * time.Minute,
		AbsoluteTimeout:    12 * time.Hour,
		RotationInterval:   15 * time.Minute,
		PreviousTokenGrace: 30 * time.Second,
	}
}

func validRecord(now time.Time) Record {
	return Record{
		ID: "session-id",
		Principal: auth.Principal{
			UserID:       "user-id",
			Username:     "alice",
			PlatformRole: auth.PlatformRoleUser,
		},
		UserActive:        true,
		CSRFTokenHash:     hashToken("csrf-token"),
		LastActivityAt:    now.Add(-time.Minute),
		RotatedAt:         now.Add(-time.Minute),
		AbsoluteExpiresAt: now.Add(time.Hour),
		TokenIsCurrent:    true,
	}
}

func TestCreateStoresHashesAndKeepsAbsoluteExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	repository := &fakeRepository{}
	service, err := NewService(
		repository,
		&queuedTokenSource{tokens: []string{"raw-session-token", "raw-csrf-token"}},
		fakeClock{now: now},
		testConfig(),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	credentials, err := service.Create(context.Background(), auth.Principal{UserID: "user-id"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !repository.created {
		t.Fatal("repository Create() was not called")
	}
	if bytes.Equal(repository.createParams.CurrentTokenHash, []byte(credentials.SessionToken)) {
		t.Fatal("repository received plaintext session token")
	}
	if bytes.Equal(repository.createParams.CSRFTokenHash, []byte(credentials.CSRFToken)) {
		t.Fatal("repository received plaintext csrf token")
	}
	wantExpiry := now.Add(12 * time.Hour)
	if !credentials.AbsoluteExpiresAt.Equal(wantExpiry) {
		t.Fatalf("AbsoluteExpiresAt = %v, want %v", credentials.AbsoluteExpiresAt, wantExpiry)
	}
}

func TestAuthenticateRotatesWithoutExtendingAbsoluteExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	record := validRecord(now)
	record.RotatedAt = now.Add(-15 * time.Minute)
	repository := &fakeRepository{record: record, rotateResult: true, touchResult: true}
	service, err := NewService(
		repository,
		&queuedTokenSource{tokens: []string{"rotated-token"}},
		fakeClock{now: now},
		testConfig(),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	authentication, err := service.Authenticate(context.Background(), "current-token")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if authentication.RotatedSessionToken != "rotated-token" {
		t.Fatalf("RotatedSessionToken = %q, want rotated-token", authentication.RotatedSessionToken)
	}
	if !authentication.AbsoluteExpiresAt.Equal(record.AbsoluteExpiresAt) {
		t.Fatalf("absolute expiry changed from %v to %v", record.AbsoluteExpiresAt, authentication.AbsoluteExpiresAt)
	}
	if !repository.rotateParams.PreviousTokenValidUntil.Equal(now.Add(30 * time.Second)) {
		t.Errorf("grace deadline = %v, want %v", repository.rotateParams.PreviousTokenValidUntil, now.Add(30*time.Second))
	}
}

func TestAuthenticateRejectsExpiryAndRevocationBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	revokedAt := now.Add(-time.Second)
	tests := []struct {
		name   string
		mutate func(*Record)
	}{
		{name: "idle timeout", mutate: func(record *Record) { record.LastActivityAt = now.Add(-30 * time.Minute) }},
		{name: "absolute expiration", mutate: func(record *Record) { record.AbsoluteExpiresAt = now }},
		{name: "revoked", mutate: func(record *Record) { record.RevokedAt = &revokedAt }},
		{name: "inactive user", mutate: func(record *Record) { record.UserActive = false }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := validRecord(now)
			tt.mutate(&record)
			service, err := NewService(
				&fakeRepository{record: record, touchResult: true},
				&queuedTokenSource{},
				fakeClock{now: now},
				testConfig(),
			)
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}
			_, err = service.Authenticate(context.Background(), "token")
			if !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("Authenticate() error = %v, want ErrUnauthenticated", err)
			}
		})
	}
}

func TestCSRFValidationAndRotation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	repository := &fakeRepository{}
	service, err := NewService(
		repository,
		&queuedTokenSource{tokens: []string{"new-csrf-token"}},
		fakeClock{now: now},
		testConfig(),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	authentication := Authentication{CSRFTokenHash: hashToken("csrf-token")}
	if err := service.ValidateCSRF(authentication, "csrf-token"); err != nil {
		t.Fatalf("ValidateCSRF(valid) error = %v", err)
	}
	if err := service.ValidateCSRF(authentication, "wrong"); !errors.Is(err, ErrInvalidCSRF) {
		t.Fatalf("ValidateCSRF(wrong) error = %v, want ErrInvalidCSRF", err)
	}

	token, err := service.RotateCSRF(context.Background(), "session-id")
	if err != nil {
		t.Fatalf("RotateCSRF() error = %v", err)
	}
	if token != "new-csrf-token" || !bytes.Equal(repository.rotatedCSRFHash, hashToken(token)) {
		t.Fatal("RotateCSRF() did not persist the new token hash")
	}
}
