package group

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/uuid"
)

const maxDescriptionRunes = 500

// Service menjalankan aturan bisnis group, membership, dan grant.
type Service struct {
	repository Repository
	policy     authz.Service
	sessions   auth.SessionRevoker
}

func NewService(repository Repository, policy authz.Service, sessions auth.SessionRevoker) (*Service, error) {
	if repository == nil || policy == nil || sessions == nil {
		return nil, errors.New("group service requires repository, policy, and session revoker")
	}
	return &Service{repository: repository, policy: policy, sessions: sessions}, nil
}

func (s *Service) List(ctx context.Context, principal auth.Principal) ([]Group, error) {
	if err := s.policy.RequirePlatformAdmin(ctx, principal); err != nil {
		return nil, err
	}
	return s.repository.List(ctx)
}

func (s *Service) Get(ctx context.Context, principal auth.Principal, id string) (Group, error) {
	if err := s.requireAdminAndID(ctx, principal, id, ErrNotFound); err != nil {
		return Group{}, err
	}
	return s.repository.Get(ctx, id)
}

func (s *Service) Create(ctx context.Context, principal auth.Principal, input CreateInput) (Group, error) {
	if err := s.policy.RequirePlatformAdmin(ctx, principal); err != nil {
		return Group{}, err
	}
	name, err := normalizeName(input.Name)
	if err != nil {
		return Group{}, err
	}
	description, _, err := normalizeDescription(input.Description)
	if err != nil {
		return Group{}, err
	}
	return s.repository.Create(ctx, name, description)
}

func (s *Service) Update(ctx context.Context, principal auth.Principal, id string, input UpdateInput) (Group, error) {
	if err := s.requireAdminAndID(ctx, principal, id, ErrNotFound); err != nil {
		return Group{}, err
	}
	var name *string
	if input.Name != nil {
		normalized, err := normalizeName(*input.Name)
		if err != nil {
			return Group{}, err
		}
		name = &normalized
	}
	description, clearDescription, err := normalizeDescription(input.Description)
	if err != nil {
		return Group{}, err
	}
	return s.repository.Update(ctx, id, name, description, clearDescription)
}

func (s *Service) Delete(ctx context.Context, principal auth.Principal, id string) error {
	if err := s.requireAdminAndID(ctx, principal, id, ErrNotFound); err != nil {
		return err
	}
	return s.repository.Delete(ctx, id)
}

func (s *Service) ListMembers(ctx context.Context, principal auth.Principal, groupID string) ([]Member, error) {
	if err := s.requireAdminAndID(ctx, principal, groupID, ErrNotFound); err != nil {
		return nil, err
	}
	return s.repository.ListMembers(ctx, groupID)
}

// AddMember idempotent. Bila group sudah punya grant, menambah anggota berarti
// privilege naik sehingga seluruh session user dicabut (login ulang = ID baru).
func (s *Service) AddMember(ctx context.Context, principal auth.Principal, groupID, userID string) (bool, error) {
	if err := s.requireAdminAndID(ctx, principal, groupID, ErrNotFound); err != nil {
		return false, err
	}
	if !uuid.Valid(userID) {
		return false, ErrUserNotFound
	}
	return s.repository.AddMember(ctx, groupID, userID, s.revokeUsers(ctx))
}

// RemoveMember idempotent. Pengurangan privilege berlaku seketika karena authz
// membaca membership pada setiap request.
func (s *Service) RemoveMember(ctx context.Context, principal auth.Principal, groupID, userID string) (bool, error) {
	if err := s.requireAdminAndID(ctx, principal, groupID, ErrNotFound); err != nil {
		return false, err
	}
	if !uuid.Valid(userID) {
		return false, nil
	}
	return s.repository.RemoveMember(ctx, groupID, userID)
}

func (s *Service) ListGrantsByGroup(ctx context.Context, principal auth.Principal, groupID string) ([]Grant, error) {
	if err := s.requireAdminAndID(ctx, principal, groupID, ErrNotFound); err != nil {
		return nil, err
	}
	return s.repository.ListGrantsByGroup(ctx, groupID)
}

func (s *Service) ListGrantsByDatabase(ctx context.Context, principal auth.Principal, databaseID string) ([]Grant, error) {
	if err := s.requireAdminAndID(ctx, principal, databaseID, ErrDatabaseNotFound); err != nil {
		return nil, err
	}
	return s.repository.ListGrantsByDatabase(ctx, databaseID)
}

// UpsertGrant menetapkan level akses group terhadap database.
func (s *Service) UpsertGrant(
	ctx context.Context,
	principal auth.Principal,
	groupID, databaseID string,
	input GrantInput,
) (Grant, error) {
	if err := s.requireAdminAndID(ctx, principal, groupID, ErrNotFound); err != nil {
		return Grant{}, err
	}
	if !uuid.Valid(databaseID) {
		return Grant{}, ErrDatabaseNotFound
	}
	level, err := authz.ParseGrantLevel(input.AccessLevel)
	if err != nil {
		return Grant{}, &ValidationError{"accessLevel", "access level must be view_only or view_edit"}
	}
	return s.repository.UpsertGrant(ctx, groupID, databaseID, level, s.revokeUsers(ctx))
}

func (s *Service) DeleteGrant(ctx context.Context, principal auth.Principal, groupID, databaseID string) error {
	if err := s.requireAdminAndID(ctx, principal, groupID, ErrNotFound); err != nil {
		return err
	}
	if !uuid.Valid(databaseID) {
		return ErrGrantNotFound
	}
	return s.repository.DeleteGrant(ctx, groupID, databaseID)
}

func (s *Service) revokeUsers(ctx context.Context) RevokeUsers {
	return func(userIDs []string) error {
		for _, userID := range userIDs {
			if err := s.sessions.RevokeAllForUser(ctx, userID); err != nil {
				return fmt.Errorf("revoke sessions after privilege change: %w", err)
			}
		}
		return nil
	}
}

func (s *Service) requireAdminAndID(ctx context.Context, principal auth.Principal, id string, notFound error) error {
	if err := s.policy.RequirePlatformAdmin(ctx, principal); err != nil {
		return err
	}
	if !uuid.Valid(id) {
		return notFound
	}
	return nil
}

func normalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if length := utf8.RuneCountInString(name); length < 1 || length > 100 {
		return "", &ValidationError{"name", "name must contain between 1 and 100 characters"}
	}
	return name, nil
}

// normalizeDescription: nil = tidak diubah, "" = hapus deskripsi.
func normalizeDescription(raw *string) (*string, bool, error) {
	if raw == nil {
		return nil, false, nil
	}
	description := strings.TrimSpace(*raw)
	if description == "" {
		return nil, true, nil
	}
	if utf8.RuneCountInString(description) > maxDescriptionRunes {
		return nil, false, &ValidationError{"description", "description must contain at most 500 characters"}
	}
	return &description, false, nil
}
