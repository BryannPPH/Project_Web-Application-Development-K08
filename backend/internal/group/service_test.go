package group

import (
	"context"
	"errors"
	"testing"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz/authztest"
)

const (
	groupID    = "11111111-1111-4111-8111-111111111111"
	userID     = "22222222-2222-4222-8222-222222222222"
	databaseID = "33333333-3333-4333-8333-333333333333"
)

// stubRepository merekam pemanggilan dan memanggil hook revoke seperti PGRepository.
type stubRepository struct {
	calls        int
	hasGrant     bool
	previous     authz.AccessLevel
	members      []string
	lastName     *string
	lastDesc     *string
	lastClear    bool
	revokeResult error
}

func (r *stubRepository) List(context.Context) ([]Group, error) { r.calls++; return nil, nil }
func (r *stubRepository) Get(context.Context, string) (Group, error) {
	r.calls++
	return Group{ID: groupID}, nil
}
func (r *stubRepository) Create(_ context.Context, name string, description *string) (Group, error) {
	r.calls++
	r.lastName, r.lastDesc = &name, description
	return Group{ID: groupID, Name: name, Description: description}, nil
}
func (r *stubRepository) Update(_ context.Context, _ string, name, description *string, clear bool) (Group, error) {
	r.calls++
	r.lastName, r.lastDesc, r.lastClear = name, description, clear
	return Group{ID: groupID}, nil
}
func (r *stubRepository) Delete(context.Context, string) error { r.calls++; return nil }
func (r *stubRepository) ListMembers(context.Context, string) ([]Member, error) {
	r.calls++
	return nil, nil
}
func (r *stubRepository) AddMember(_ context.Context, _, user string, revoke RevokeUsers) (bool, error) {
	r.calls++
	if r.hasGrant {
		if err := revoke([]string{user}); err != nil {
			return false, err
		}
	}
	return true, nil
}
func (r *stubRepository) RemoveMember(context.Context, string, string) (bool, error) {
	r.calls++
	return true, nil
}
func (r *stubRepository) ListGrantsByGroup(context.Context, string) ([]Grant, error) {
	r.calls++
	return nil, nil
}
func (r *stubRepository) ListGrantsByDatabase(context.Context, string) ([]Grant, error) {
	r.calls++
	return nil, nil
}
func (r *stubRepository) UpsertGrant(_ context.Context, group, database string, level authz.AccessLevel, revoke RevokeUsers) (Grant, error) {
	r.calls++
	if !r.previous.AtLeast(level) {
		if err := revoke(r.members); err != nil {
			return Grant{}, err
		}
	}
	return Grant{GroupID: group, DatabaseID: database, AccessLevel: level}, nil
}
func (r *stubRepository) DeleteGrant(context.Context, string, string) error { r.calls++; return nil }

type fakeRevoker struct {
	revoked []string
	err     error
}

func (f *fakeRevoker) RevokeAllForUser(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.revoked = append(f.revoked, id)
	return nil
}

func newService(t *testing.T) (*Service, *stubRepository, *fakeRevoker, auth.Principal, auth.Principal) {
	t.Helper()
	store := authztest.NewStore()
	admin := store.AddUser("admin", auth.PlatformRoleAdmin)
	user := store.AddUser("user", auth.PlatformRoleUser)
	repository := &stubRepository{previous: authz.AccessNone}
	revoker := &fakeRevoker{}
	service, err := NewService(repository, store.Policy(), revoker)
	if err != nil {
		t.Fatal(err)
	}
	return service, repository, revoker, admin, user
}

func TestNonAdminCannotManageGroups(t *testing.T) {
	t.Parallel()

	service, repository, _, _, user := newService(t)
	ctx := context.Background()
	errs := []error{
		func() error { _, err := service.List(ctx, user); return err }(),
		func() error { _, err := service.Create(ctx, user, CreateInput{Name: "x"}); return err }(),
		func() error { _, err := service.AddMember(ctx, user, groupID, userID); return err }(),
		func() error {
			_, err := service.UpsertGrant(ctx, user, groupID, databaseID, GrantInput{AccessLevel: "view_edit"})
			return err
		}(),
		service.DeleteGrant(ctx, user, groupID, databaseID),
		service.Delete(ctx, user, groupID),
	}
	for index, err := range errs {
		if !errors.Is(err, authz.ErrPlatformAdminRequired) {
			t.Errorf("call %d error = %v, want ErrPlatformAdminRequired", index, err)
		}
	}
	if repository.calls != 0 {
		t.Fatalf("repository calls = %d, want 0", repository.calls)
	}
}

func TestCreateAndUpdateValidation(t *testing.T) {
	t.Parallel()

	service, repository, _, admin, _ := newService(t)
	ctx := context.Background()
	if _, err := service.Create(ctx, admin, CreateInput{Name: "   "}); err == nil {
		t.Fatal("blank name must be rejected")
	}
	long := string(make([]rune, 501))
	if _, err := service.Create(ctx, admin, CreateInput{Name: "ok", Description: &long}); err == nil {
		t.Fatal("long description must be rejected")
	}
	description := "  Analysts  "
	if _, err := service.Create(ctx, admin, CreateInput{Name: "  Analysts ", Description: &description}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if *repository.lastName != "Analysts" || *repository.lastDesc != "Analysts" {
		t.Fatalf("values were not trimmed: %q %q", *repository.lastName, *repository.lastDesc)
	}
	empty := ""
	if _, err := service.Update(ctx, admin, groupID, UpdateInput{Description: &empty}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !repository.lastClear {
		t.Fatal("empty description must clear the field")
	}
}

func TestGrantLevelValidation(t *testing.T) {
	t.Parallel()

	service, _, _, admin, _ := newService(t)
	for _, level := range []string{"", "none", "admin", "VIEW_EDIT"} {
		_, err := service.UpsertGrant(context.Background(), admin, groupID, databaseID, GrantInput{AccessLevel: level})
		var validation *ValidationError
		if !errors.As(err, &validation) {
			t.Errorf("UpsertGrant(%q) error = %v, want validation", level, err)
		}
	}
	if _, err := service.UpsertGrant(context.Background(), admin, groupID, "bad-id", GrantInput{AccessLevel: "view_only"}); !errors.Is(err, ErrDatabaseNotFound) {
		t.Errorf("UpsertGrant(bad id) error = %v", err)
	}
}

func TestPrivilegeElevationRevokesMemberSessions(t *testing.T) {
	t.Parallel()

	service, repository, revoker, admin, _ := newService(t)
	ctx := context.Background()
	repository.members = []string{userID, groupID}

	if _, err := service.UpsertGrant(ctx, admin, groupID, databaseID, GrantInput{AccessLevel: "view_only"}); err != nil {
		t.Fatalf("UpsertGrant() error = %v", err)
	}
	if len(revoker.revoked) != 2 {
		t.Fatalf("revoked = %v, want both members", revoker.revoked)
	}

	revoker.revoked = nil
	repository.previous = authz.AccessViewEdit
	if _, err := service.UpsertGrant(ctx, admin, groupID, databaseID, GrantInput{AccessLevel: "view_only"}); err != nil {
		t.Fatalf("downgrade error = %v", err)
	}
	if len(revoker.revoked) != 0 {
		t.Fatal("downgrade must not revoke sessions")
	}

	repository.hasGrant = true
	if _, err := service.AddMember(ctx, admin, groupID, userID); err != nil {
		t.Fatalf("AddMember() error = %v", err)
	}
	if len(revoker.revoked) != 1 || revoker.revoked[0] != userID {
		t.Fatalf("AddMember revoked = %v", revoker.revoked)
	}
}

func TestRevokeFailureAbortsGrant(t *testing.T) {
	t.Parallel()

	service, repository, revoker, admin, _ := newService(t)
	repository.members = []string{userID}
	revoker.err = errors.New("session store down")
	if _, err := service.UpsertGrant(context.Background(), admin, groupID, databaseID, GrantInput{AccessLevel: "view_edit"}); !errors.Is(err, revoker.err) {
		t.Fatalf("UpsertGrant() error = %v, want revoke failure", err)
	}
}

func TestInvalidIdentifiers(t *testing.T) {
	t.Parallel()

	service, repository, _, admin, _ := newService(t)
	ctx := context.Background()
	if _, err := service.Get(ctx, admin, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(nope) error = %v", err)
	}
	if _, err := service.AddMember(ctx, admin, groupID, "nope"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("AddMember(nope) error = %v", err)
	}
	if removed, err := service.RemoveMember(ctx, admin, groupID, "nope"); err != nil || removed {
		t.Errorf("RemoveMember(nope) = %v, %v; want idempotent false", removed, err)
	}
	if repository.calls != 0 {
		t.Fatal("invalid identifiers reached repository")
	}
}
