package user

import (
	"context"
	"errors"
	"testing"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz/authztest"
)

// memoryRepository meniru transaksi: perubahan hanya disimpan bila hook sukses.
type memoryRepository struct {
	users map[string]User
	calls int
}

func (r *memoryRepository) List(context.Context) ([]User, error) {
	r.calls++
	users := make([]User, 0, len(r.users))
	for _, record := range r.users {
		users = append(users, record)
	}
	return users, nil
}

func (r *memoryRepository) Get(_ context.Context, id string) (User, error) {
	r.calls++
	record, ok := r.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return record, nil
}

func (r *memoryRepository) Create(_ context.Context, record createRecord) (User, error) {
	r.calls++
	for _, existing := range r.users {
		if existing.Username == record.Username {
			return User{}, ErrDuplicateUsername
		}
	}
	created := User{
		ID:           "00000000-0000-4000-8000-00000000000" + string(rune('a'+len(r.users))),
		Username:     record.Username,
		PlatformRole: record.PlatformRole,
		IsActive:     record.IsActive,
	}
	r.users[created.ID] = created
	return created, nil
}

func (r *memoryRepository) Update(_ context.Context, id string, record updateRecord, beforeCommit BeforeCommit) (User, error) {
	r.calls++
	before, ok := r.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	after := before
	if record.Username != nil {
		after.Username = *record.Username
	}
	if record.PlatformRole != nil {
		after.PlatformRole = *record.PlatformRole
	}
	if record.IsActive != nil {
		after.IsActive = *record.IsActive
	}
	if isActiveAdmin(before) && !isActiveAdmin(after) {
		remaining := 0
		for otherID, other := range r.users {
			if otherID != id && isActiveAdmin(other) {
				remaining++
			}
		}
		if remaining == 0 {
			return User{}, ErrLastActiveAdmin
		}
	}
	if beforeCommit != nil {
		if err := beforeCommit(before, after); err != nil {
			return User{}, err
		}
	}
	r.users[id] = after
	return after, nil
}

type fakeHasher struct{}

func (fakeHasher) Hash(password string) (string, error) {
	switch {
	case password == "":
		return "", auth.ErrPasswordRequired
	case len(password) < 8:
		return "", auth.ErrPasswordTooShort
	}
	return "hashed:" + password, nil
}

func (fakeHasher) Verify(string, string) (bool, error) { return false, nil }

type fakeRevoker struct {
	revoked []string
	err     error
}

func (r *fakeRevoker) RevokeAllForUser(_ context.Context, userID string) error {
	if r.err != nil {
		return r.err
	}
	r.revoked = append(r.revoked, userID)
	return nil
}

type fixture struct {
	service    *Service
	repository *memoryRepository
	revoker    *fakeRevoker
	admin      auth.Principal
	user       auth.Principal
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	store := authztest.NewStore()
	f := &fixture{
		repository: &memoryRepository{users: make(map[string]User)},
		revoker:    &fakeRevoker{},
		admin:      store.AddUser("admin", auth.PlatformRoleAdmin),
		user:       store.AddUser("operator", auth.PlatformRoleUser),
	}
	f.repository.users[f.admin.UserID] = User{ID: f.admin.UserID, Username: "admin", PlatformRole: auth.PlatformRoleAdmin, IsActive: true}
	f.repository.users[f.user.UserID] = User{ID: f.user.UserID, Username: "operator", PlatformRole: auth.PlatformRoleUser, IsActive: true}
	var err error
	f.service, err = NewService(f.repository, store.Policy(), fakeHasher{}, f.revoker)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func pointer[T any](value T) *T { return &value }

func TestNonAdminIsRejectedBeforeRepository(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	ctx := context.Background()
	calls := []error{
		func() error { _, err := f.service.List(ctx, f.user); return err }(),
		func() error { _, err := f.service.Get(ctx, f.user, f.admin.UserID); return err }(),
		func() error {
			_, err := f.service.Create(ctx, f.user, CreateInput{Username: "new-user", Password: "password-123"})
			return err
		}(),
		func() error {
			_, err := f.service.Update(ctx, f.user, f.admin.UserID, UpdateInput{IsActive: pointer(false)})
			return err
		}(),
		func() error {
			_, err := f.service.ResetPassword(ctx, f.user, f.admin.UserID, ResetPasswordInput{Password: "password-123"})
			return err
		}(),
	}
	for index, err := range calls {
		if !errors.Is(err, authz.ErrPlatformAdminRequired) {
			t.Errorf("call %d error = %v, want ErrPlatformAdminRequired", index, err)
		}
	}
	if f.repository.calls != 0 {
		t.Fatalf("repository calls = %d, want 0", f.repository.calls)
	}
}

func TestCreateValidatesAndHashes(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	ctx := context.Background()
	cases := []struct {
		input CreateInput
		field string
	}{
		{CreateInput{Username: "ab", Password: "password-123"}, "username"},
		{CreateInput{Username: "has space", Password: "password-123"}, "username"},
		{CreateInput{Username: "valid-user", Password: "short"}, "password"},
		{CreateInput{Username: "valid-user", Password: ""}, "password"},
		{CreateInput{Username: "valid-user", Password: "password-123", PlatformRole: "root"}, "platformRole"},
	}
	for _, tc := range cases {
		_, err := f.service.Create(ctx, f.admin, tc.input)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Field != tc.field {
			t.Errorf("Create(%+v) error = %v, want validation on %s", tc.input, err, tc.field)
		}
	}

	created, err := f.service.Create(ctx, f.admin, CreateInput{Username: "  analyst  ", Password: "password-123"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Username != "analyst" || created.PlatformRole != auth.PlatformRoleUser || !created.IsActive {
		t.Fatalf("Create() = %+v, want trimmed active user", created)
	}
	if _, err := f.service.Create(ctx, f.admin, CreateInput{Username: "analyst", Password: "password-123"}); !errors.Is(err, ErrDuplicateUsername) {
		t.Fatalf("duplicate Create() error = %v", err)
	}
}

func TestDeactivationAndRoleChangeRevokeSessions(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.service.Update(ctx, f.admin, f.user.UserID, UpdateInput{Username: pointer("operator-2")}); err != nil {
		t.Fatalf("rename error = %v", err)
	}
	if len(f.revoker.revoked) != 0 {
		t.Fatal("rename must not revoke sessions")
	}

	role := auth.PlatformRoleAdmin
	if _, err := f.service.Update(ctx, f.admin, f.user.UserID, UpdateInput{PlatformRole: &role}); err != nil {
		t.Fatalf("promote error = %v", err)
	}
	if _, err := f.service.Update(ctx, f.admin, f.user.UserID, UpdateInput{IsActive: pointer(false)}); err != nil {
		t.Fatalf("deactivate error = %v", err)
	}
	if len(f.revoker.revoked) != 2 {
		t.Fatalf("revocations = %v, want two (promotion + deactivation)", f.revoker.revoked)
	}
}

func TestResetPasswordRevokesSessions(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	if _, err := f.service.ResetPassword(context.Background(), f.admin, f.user.UserID, ResetPasswordInput{Password: "new-password-1"}); err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}
	if len(f.revoker.revoked) != 1 || f.revoker.revoked[0] != f.user.UserID {
		t.Fatalf("revocations = %v", f.revoker.revoked)
	}
	if _, err := f.service.ResetPassword(context.Background(), f.admin, f.user.UserID, ResetPasswordInput{Password: "short"}); err == nil {
		t.Fatal("short password must be rejected")
	}
}

func TestRevokeFailureAbortsChange(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.revoker.err = errors.New("session store down")
	_, err := f.service.Update(context.Background(), f.admin, f.user.UserID, UpdateInput{IsActive: pointer(false)})
	if !errors.Is(err, f.revoker.err) {
		t.Fatalf("Update() error = %v, want revoke error", err)
	}
	if !f.repository.users[f.user.UserID].IsActive {
		t.Fatal("user was deactivated although revocation failed")
	}
}

func TestLastActiveAdminGuard(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	ctx := context.Background()
	role := auth.PlatformRoleUser
	if _, err := f.service.Update(ctx, f.admin, f.admin.UserID, UpdateInput{PlatformRole: &role}); !errors.Is(err, ErrLastActiveAdmin) {
		t.Fatalf("demote last admin error = %v", err)
	}
	if _, err := f.service.Update(ctx, f.admin, f.admin.UserID, UpdateInput{IsActive: pointer(false)}); !errors.Is(err, ErrLastActiveAdmin) {
		t.Fatalf("deactivate last admin error = %v", err)
	}
	if len(f.revoker.revoked) != 0 {
		t.Fatal("guard failure must not revoke sessions")
	}
}

func TestUnknownOrInvalidUserID(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	for _, id := range []string{"missing", "11111111-1111-4111-8111-111111111111"} {
		if _, err := f.service.Get(context.Background(), f.admin, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q) error = %v, want ErrNotFound", id, err)
		}
	}
}
