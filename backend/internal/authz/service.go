package authz

import (
	"context"
	"errors"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/uuid"
)

// Service adalah kontrak policy yang dipakai handler admin, viewer, dan migration.
// Consumer wajib memanggil policy untuk setiap aksi dan tidak membaca tabel
// group/grant sendiri.
type Service interface {
	RequirePlatformAdmin(ctx context.Context, principal auth.Principal) error
	EffectiveDatabaseAccess(ctx context.Context, principal auth.Principal, databaseID string) (AccessLevel, error)
	RequireDatabaseView(ctx context.Context, principal auth.Principal, databaseID string) error
	RequireDatabaseEdit(ctx context.Context, principal auth.Principal, databaseID string) error
	AccessibleDatabases(ctx context.Context, principal auth.Principal) ([]DatabaseGrant, error)
}

// Policy adalah implementasi Service berbasis Repository.
type Policy struct {
	repository Repository
}

var _ Service = (*Policy)(nil)

func NewPolicy(repository Repository) *Policy {
	return &Policy{repository: repository}
}

// RequirePlatformAdmin memeriksa role platform. Principal berasal dari session
// yang dimuat ulang dari tabel users pada setiap request, sehingga perubahan
// role/status langsung terlihat dan bukan snapshot lama.
func (p *Policy) RequirePlatformAdmin(_ context.Context, principal auth.Principal) error {
	if principal.UserID == "" {
		return ErrUnauthenticated
	}
	if principal.PlatformRole != auth.PlatformRoleAdmin {
		return ErrPlatformAdminRequired
	}
	return nil
}

// EffectiveDatabaseAccess mengembalikan level akses efektif yang dapat dipakai.
// Database yang tidak ada, nonaktif, atau user nonaktif selalu menghasilkan none.
func (p *Policy) EffectiveDatabaseAccess(
	ctx context.Context,
	principal auth.Principal,
	databaseID string,
) (AccessLevel, error) {
	facts, err := p.facts(ctx, principal, databaseID)
	if err != nil {
		if errors.Is(err, errInvalidDatabaseID) {
			return AccessNone, nil
		}
		return AccessNone, err
	}
	if !facts.UserExists || !facts.UserActive || !facts.DatabaseExists || !facts.DatabaseActive {
		return AccessNone, nil
	}
	return facts.GrantedLevel, nil
}

// RequireDatabaseView menerima view_only dan view_edit.
func (p *Policy) RequireDatabaseView(ctx context.Context, principal auth.Principal, databaseID string) error {
	_, err := p.require(ctx, principal, databaseID, AccessViewOnly)
	return err
}

// RequireDatabaseEdit hanya menerima view_edit.
func (p *Policy) RequireDatabaseEdit(ctx context.Context, principal auth.Principal, databaseID string) error {
	_, err := p.require(ctx, principal, databaseID, AccessViewEdit)
	return err
}

// AccessibleDatabases mengembalikan database aktif yang minimal view_only.
func (p *Policy) AccessibleDatabases(ctx context.Context, principal auth.Principal) ([]DatabaseGrant, error) {
	if principal.UserID == "" || !uuid.Valid(principal.UserID) {
		return nil, ErrUnauthenticated
	}
	return p.repository.ActiveDatabaseGrants(ctx, principal.UserID)
}

var errInvalidDatabaseID = errors.New("authz: invalid database id")

func (p *Policy) facts(ctx context.Context, principal auth.Principal, databaseID string) (AccessFacts, error) {
	if principal.UserID == "" || !uuid.Valid(principal.UserID) {
		return AccessFacts{}, ErrUnauthenticated
	}
	if !uuid.Valid(databaseID) {
		return AccessFacts{}, errInvalidDatabaseID
	}
	return p.repository.AccessFacts(ctx, principal.UserID, databaseID)
}

func (p *Policy) require(
	ctx context.Context,
	principal auth.Principal,
	databaseID string,
	minimum AccessLevel,
) (AccessLevel, error) {
	facts, err := p.facts(ctx, principal, databaseID)
	if errors.Is(err, errInvalidDatabaseID) {
		return AccessNone, ErrDatabaseNotFound
	}
	if err != nil {
		return AccessNone, err
	}
	if !facts.UserExists || !facts.UserActive {
		return AccessNone, ErrUnauthenticated
	}
	// Tanpa grant, database diperlakukan sama seperti tidak ada (anti-enumeration).
	if !facts.DatabaseExists || !facts.GrantedLevel.CanView() {
		return AccessNone, ErrDatabaseNotFound
	}
	if !facts.DatabaseActive {
		return AccessNone, ErrDatabaseInactive
	}
	if !facts.GrantedLevel.AtLeast(minimum) {
		return facts.GrantedLevel, ErrDatabaseEditForbidden
	}
	return facts.GrantedLevel, nil
}
