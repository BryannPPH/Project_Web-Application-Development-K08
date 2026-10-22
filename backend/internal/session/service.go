package session

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
)

var (
	ErrUnauthenticated = errors.New("authentication required")
	ErrInvalidCSRF     = errors.New("invalid csrf token")
)

type Authentication struct {
	SessionID         string
	Principal         auth.Principal
	CSRFTokenHash     []byte
	AbsoluteExpiresAt time.Time
	// RotatedSessionToken hanya diisi saat rotasi berhasil.
	RotatedSessionToken string
}

type Service struct {
	repository Repository
	tokens     TokenSource
	clock      Clock
	config     Config
}

func NewService(repository Repository, tokens TokenSource, clock Clock, cfg Config) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Service{
		repository: repository,
		tokens:     tokens,
		clock:      clock,
		config:     cfg,
	}, nil
}

// Create membuat sesi baru beserta token CSRF terpisah.
func (s *Service) Create(ctx context.Context, principal auth.Principal) (auth.SessionCredentials, error) {
	sessionToken, err := s.tokens.Generate()
	if err != nil {
		return auth.SessionCredentials{}, err
	}
	csrfToken, err := s.tokens.Generate()
	if err != nil {
		return auth.SessionCredentials{}, err
	}

	now := s.clock.Now().UTC()
	absoluteExpiresAt := now.Add(s.config.AbsoluteTimeout)
	_, err = s.repository.Create(ctx, CreateParams{
		UserID:            principal.UserID,
		CurrentTokenHash:  hashToken(sessionToken),
		CSRFTokenHash:     hashToken(csrfToken),
		Now:               now,
		AbsoluteExpiresAt: absoluteExpiresAt,
	})
	if err != nil {
		return auth.SessionCredentials{}, err
	}

	return auth.SessionCredentials{
		SessionToken:      sessionToken,
		CSRFToken:         csrfToken,
		AbsoluteExpiresAt: absoluteExpiresAt,
	}, nil
}

func (s *Service) Authenticate(ctx context.Context, rawToken string) (Authentication, error) {
	if strings.TrimSpace(rawToken) == "" || len(rawToken) > 4096 {
		return Authentication{}, ErrUnauthenticated
	}

	now := s.clock.Now().UTC()
	tokenHash := hashToken(rawToken)
	record, err := s.repository.FindByTokenHash(ctx, tokenHash, now)
	if errors.Is(err, ErrNotFound) {
		return Authentication{}, ErrUnauthenticated
	}
	if err != nil {
		return Authentication{}, err
	}
	if !s.recordIsValid(record, now) {
		return Authentication{}, ErrUnauthenticated
	}

	authentication := authenticationFromRecord(record)
	if record.TokenIsCurrent && now.Sub(record.RotatedAt) >= s.config.RotationInterval {
		rotated, err := s.rotate(ctx, record, tokenHash, now)
		if err != nil {
			return Authentication{}, err
		}
		if rotated != "" {
			authentication.RotatedSessionToken = rotated
			return authentication, nil
		}

		// Muat ulang token yang mungkin telah dirotasi permintaan lain.
		record, err = s.repository.FindByTokenHash(ctx, tokenHash, now)
		if errors.Is(err, ErrNotFound) {
			return Authentication{}, ErrUnauthenticated
		}
		if err != nil {
			return Authentication{}, err
		}
		if !s.recordIsValid(record, now) || record.TokenIsCurrent {
			return Authentication{}, ErrUnauthenticated
		}
		authentication = authenticationFromRecord(record)
	}

	touched, err := s.repository.Touch(ctx, record.ID, now)
	if err != nil {
		return Authentication{}, err
	}
	if !touched {
		return Authentication{}, ErrUnauthenticated
	}
	return authentication, nil
}

func (s *Service) rotate(ctx context.Context, record Record, expectedHash []byte, now time.Time) (string, error) {
	newToken, err := s.tokens.Generate()
	if err != nil {
		return "", err
	}
	rotated, err := s.repository.Rotate(ctx, RotateParams{
		SessionID:               record.ID,
		ExpectedCurrentHash:     expectedHash,
		NewCurrentHash:          hashToken(newToken),
		PreviousTokenValidUntil: now.Add(s.config.PreviousTokenGrace),
		Now:                     now,
	})
	if err != nil {
		return "", err
	}
	if !rotated {
		return "", nil
	}
	return newToken, nil
}

func (s *Service) recordIsValid(record Record, now time.Time) bool {
	if record.RevokedAt != nil || !record.UserActive || !record.Principal.PlatformRole.Valid() {
		return false
	}
	if !now.Before(record.AbsoluteExpiresAt) {
		return false
	}
	if now.Sub(record.LastActivityAt) >= s.config.IdleTimeout {
		return false
	}
	return true
}

func authenticationFromRecord(record Record) Authentication {
	return Authentication{
		SessionID:         record.ID,
		Principal:         record.Principal,
		CSRFTokenHash:     append([]byte(nil), record.CSRFTokenHash...),
		AbsoluteExpiresAt: record.AbsoluteExpiresAt,
	}
}

func (s *Service) ValidateCSRF(authentication Authentication, rawToken string) error {
	if rawToken == "" || len(rawToken) > 4096 {
		return ErrInvalidCSRF
	}
	actual := hashToken(rawToken)
	if subtle.ConstantTimeCompare(actual, authentication.CSRFTokenHash) != 1 {
		return ErrInvalidCSRF
	}
	return nil
}

func (s *Service) RotateCSRF(ctx context.Context, sessionID string) (string, error) {
	token, err := s.tokens.Generate()
	if err != nil {
		return "", err
	}
	rotated, err := s.repository.RotateCSRF(ctx, sessionID, hashToken(token), s.clock.Now().UTC())
	if err != nil {
		return "", err
	}
	if !rotated {
		return "", ErrUnauthenticated
	}
	return token, nil
}

func (s *Service) Revoke(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("revoke session: %w", ErrUnauthenticated)
	}
	return s.repository.Revoke(ctx, sessionID, s.clock.Now().UTC())
}

// RevokeAllForUser mencabut seluruh sesi pengguna.
func (s *Service) RevokeAllForUser(ctx context.Context, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return errors.New("user id is required")
	}
	return s.repository.RevokeAllForUser(ctx, userID, s.clock.Now().UTC())
}
