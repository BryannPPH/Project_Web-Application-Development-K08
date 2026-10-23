package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/uuid"
)

// Service menjalankan aturan bisnis administrasi user.
type Service struct {
	repository Repository
	policy     authz.Service
	passwords  auth.PasswordHasher
	sessions   auth.SessionRevoker
}

func NewService(
	repository Repository,
	policy authz.Service,
	passwords auth.PasswordHasher,
	sessions auth.SessionRevoker,
) (*Service, error) {
	if repository == nil || policy == nil || passwords == nil || sessions == nil {
		return nil, errors.New("user service requires repository, policy, password hasher, and session revoker")
	}
	return &Service{repository: repository, policy: policy, passwords: passwords, sessions: sessions}, nil
}

func (s *Service) List(ctx context.Context, principal auth.Principal) ([]User, error) {
	if err := s.policy.RequirePlatformAdmin(ctx, principal); err != nil {
		return nil, err
	}
	return s.repository.List(ctx)
}

func (s *Service) Get(ctx context.Context, principal auth.Principal, id string) (User, error) {
	if err := s.policy.RequirePlatformAdmin(ctx, principal); err != nil {
		return User{}, err
	}
	if !uuid.Valid(id) {
		return User{}, ErrNotFound
	}
	return s.repository.Get(ctx, id)
}

// Create membuat user baru dengan password yang di-hash memakai hasher Person 1.
func (s *Service) Create(ctx context.Context, principal auth.Principal, input CreateInput) (User, error) {
	if err := s.policy.RequirePlatformAdmin(ctx, principal); err != nil {
		return User{}, err
	}
	username, err := normalizeUsername(input.Username)
	if err != nil {
		return User{}, err
	}
	role := input.PlatformRole
	if role == "" {
		role = auth.PlatformRoleUser
	}
	if !role.Valid() {
		return User{}, &ValidationError{"platformRole", "platform role must be user or platform_admin"}
	}
	passwordHash, err := s.hash(input.Password)
	if err != nil {
		return User{}, err
	}
	active := true
	if input.IsActive != nil {
		active = *input.IsActive
	}
	return s.repository.Create(ctx, createRecord{
		Username:     username,
		PasswordHash: passwordHash,
		PlatformRole: role,
		IsActive:     active,
	})
}

// Update mengubah username/role/status. Perubahan role atau deaktivasi mencabut
// seluruh session user SEBELUM commit; bila revoke gagal, perubahan dibatalkan.
func (s *Service) Update(ctx context.Context, principal auth.Principal, id string, input UpdateInput) (User, error) {
	if err := s.policy.RequirePlatformAdmin(ctx, principal); err != nil {
		return User{}, err
	}
	if !uuid.Valid(id) {
		return User{}, ErrNotFound
	}
	var record updateRecord
	if input.Username != nil {
		username, err := normalizeUsername(*input.Username)
		if err != nil {
			return User{}, err
		}
		record.Username = &username
	}
	if input.PlatformRole != nil {
		if !input.PlatformRole.Valid() {
			return User{}, &ValidationError{"platformRole", "platform role must be user or platform_admin"}
		}
		record.PlatformRole = input.PlatformRole
	}
	record.IsActive = input.IsActive

	return s.repository.Update(ctx, id, record, func(before, after User) error {
		if !privilegeOrStatusChanged(before, after) {
			return nil
		}
		return s.revoke(ctx, after.ID)
	})
}

// ResetPassword mengganti password lalu mencabut seluruh session user.
func (s *Service) ResetPassword(ctx context.Context, principal auth.Principal, id string, input ResetPasswordInput) (User, error) {
	if err := s.policy.RequirePlatformAdmin(ctx, principal); err != nil {
		return User{}, err
	}
	if !uuid.Valid(id) {
		return User{}, ErrNotFound
	}
	passwordHash, err := s.hash(input.Password)
	if err != nil {
		return User{}, err
	}
	return s.repository.Update(ctx, id, updateRecord{PasswordHash: &passwordHash}, func(_, after User) error {
		return s.revoke(ctx, after.ID)
	})
}

func (s *Service) revoke(ctx context.Context, userID string) error {
	if err := s.sessions.RevokeAllForUser(ctx, userID); err != nil {
		return fmt.Errorf("revoke user sessions: %w", err)
	}
	return nil
}

func (s *Service) hash(password string) (string, error) {
	passwordHash, err := s.passwords.Hash(password)
	switch {
	case errors.Is(err, auth.ErrPasswordRequired):
		return "", &ValidationError{"password", "password is required"}
	case errors.Is(err, auth.ErrPasswordTooShort):
		return "", &ValidationError{"password", "password must contain at least 8 bytes"}
	case errors.Is(err, auth.ErrPasswordTooLong):
		return "", &ValidationError{"password", "password must contain at most 1024 bytes"}
	case err != nil:
		return "", err
	}
	return passwordHash, nil
}

// privilegeOrStatusChanged: role berubah (naik/turun) atau user dinonaktifkan.
func privilegeOrStatusChanged(before, after User) bool {
	return before.PlatformRole != after.PlatformRole || (before.IsActive && !after.IsActive)
}

func normalizeUsername(raw string) (string, error) {
	username := strings.TrimSpace(raw)
	if length := utf8.RuneCountInString(username); length < 3 || length > 64 {
		return "", &ValidationError{"username", "username must contain between 3 and 64 characters"}
	}
	for _, character := range username {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return "", &ValidationError{"username", "username must not contain spaces or control characters"}
		}
	}
	return username, nil
}
