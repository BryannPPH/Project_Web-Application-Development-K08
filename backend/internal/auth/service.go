package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidCredentials = errors.New("invalid username or password")

// SessionCredentials membawa token mentah menuju respons HTTP.
type SessionCredentials struct {
	SessionToken      string
	CSRFToken         string
	AbsoluteExpiresAt time.Time
}

type SessionIssuer interface {
	Create(ctx context.Context, principal Principal) (SessionCredentials, error)
}

// SessionRevoker mencabut seluruh sesi milik pengguna.
type SessionRevoker interface {
	RevokeAllForUser(ctx context.Context, userID string) error
}

type Service struct {
	users             UserRepository
	passwords         PasswordHasher
	sessions          SessionIssuer
	dummyPasswordHash string
}

func NewService(users UserRepository, passwords PasswordHasher, sessions SessionIssuer) (*Service, error) {
	dummyHash, err := passwords.Hash("dummy-password-used-only-for-timing-equalization")
	if err != nil {
		return nil, fmt.Errorf("create authentication dummy hash: %w", err)
	}
	return &Service{
		users:             users,
		passwords:         passwords,
		sessions:          sessions,
		dummyPasswordHash: dummyHash,
	}, nil
}

type LoginResult struct {
	Principal Principal
	Session   SessionCredentials
}

func (s *Service) Login(ctx context.Context, username, password string) (LoginResult, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return LoginResult{}, ErrInvalidCredentials
	}

	record, err := s.users.FindByUsername(ctx, username)
	if errors.Is(err, ErrUserNotFound) {
		// Samarkan perbedaan waktu untuk mencegah penebakan nama pengguna.
		_, _ = s.passwords.Verify(password, s.dummyPasswordHash)
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, err
	}

	matches, err := s.passwords.Verify(password, record.PasswordHash)
	if err != nil {
		return LoginResult{}, fmt.Errorf("verify stored password hash: %w", err)
	}
	if !matches || !record.Active {
		return LoginResult{}, ErrInvalidCredentials
	}
	if !record.PlatformRole.Valid() {
		return LoginResult{}, errors.New("user has an invalid platform role")
	}

	principal := Principal{
		UserID:       record.ID,
		Username:     record.Username,
		PlatformRole: record.PlatformRole,
	}
	credentials, err := s.sessions.Create(ctx, principal)
	if err != nil {
		return LoginResult{}, fmt.Errorf("create login session: %w", err)
	}

	return LoginResult{Principal: principal, Session: credentials}, nil
}
