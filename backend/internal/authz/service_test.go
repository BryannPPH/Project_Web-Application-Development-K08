package authz_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz/authztest"
)

const (
	databaseID      = "11111111-1111-4111-8111-111111111111"
	otherDatabaseID = "22222222-2222-4222-8222-222222222222"
)

func TestEffectiveAccessMatrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		setup func(store *authztest.Store, userID string)
		want  authz.AccessLevel
	}{
		{
			name:  "no groups -> none",
			setup: func(*authztest.Store, string) {},
			want:  authz.AccessNone,
		},
		{
			name: "group without grant -> none",
			setup: func(store *authztest.Store, userID string) {
				store.AddMember("empty", userID)
			},
			want: authz.AccessNone,
		},
		{
			name: "grant for another database -> none",
			setup: func(store *authztest.Store, userID string) {
				store.Grant("other", otherDatabaseID, authz.AccessViewEdit, userID)
			},
			want: authz.AccessNone,
		},
		{
			name: "single view_only -> view_only",
			setup: func(store *authztest.Store, userID string) {
				store.Grant("readers", databaseID, authz.AccessViewOnly, userID)
			},
			want: authz.AccessViewOnly,
		},
		{
			name: "several view_only -> view_only",
			setup: func(store *authztest.Store, userID string) {
				store.Grant("readers-a", databaseID, authz.AccessViewOnly, userID)
				store.Grant("readers-b", databaseID, authz.AccessViewOnly, userID)
			},
			want: authz.AccessViewOnly,
		},
		{
			name: "view_only + view_edit -> view_edit",
			setup: func(store *authztest.Store, userID string) {
				store.Grant("readers", databaseID, authz.AccessViewOnly, userID)
				store.Grant("writers", databaseID, authz.AccessViewEdit, userID)
			},
			want: authz.AccessViewEdit,
		},
		{
			name: "inactive user -> none",
			setup: func(store *authztest.Store, userID string) {
				store.Grant("writers", databaseID, authz.AccessViewEdit, userID)
				store.SetUserActive(userID, false)
			},
			want: authz.AccessNone,
		},
		{
			name: "inactive database -> none",
			setup: func(store *authztest.Store, userID string) {
				store.Grant("writers", databaseID, authz.AccessViewEdit, userID)
				store.AddDatabase(databaseID, false)
			},
			want: authz.AccessNone,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := authztest.NewStore()
			store.AddDatabase(databaseID, true)
			store.AddDatabase(otherDatabaseID, true)
			principal := store.AddUser("member", auth.PlatformRoleUser)
			tc.setup(store, principal.UserID)

			got, err := store.Policy().EffectiveDatabaseAccess(context.Background(), principal, databaseID)
			if err != nil {
				t.Fatalf("EffectiveDatabaseAccess() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("EffectiveDatabaseAccess() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPlatformAdminWithoutGrantHasNoDatabaseAccess(t *testing.T) {
	t.Parallel()

	store := authztest.NewStore()
	store.AddDatabase(databaseID, true)
	admin := store.AddUser("admin", auth.PlatformRoleAdmin)
	policy := store.Policy()

	level, err := policy.EffectiveDatabaseAccess(context.Background(), admin, databaseID)
	if err != nil || level != authz.AccessNone {
		t.Fatalf("EffectiveDatabaseAccess() = %q, %v; want none", level, err)
	}
	if err := policy.RequireDatabaseView(context.Background(), admin, databaseID); !errors.Is(err, authz.ErrDatabaseNotFound) {
		t.Fatalf("RequireDatabaseView(admin) error = %v, want ErrDatabaseNotFound", err)
	}
	if err := policy.RequireDatabaseEdit(context.Background(), admin, databaseID); !errors.Is(err, authz.ErrDatabaseNotFound) {
		t.Fatalf("RequireDatabaseEdit(admin) error = %v, want ErrDatabaseNotFound", err)
	}
	if err := policy.RequirePlatformAdmin(context.Background(), admin); err != nil {
		t.Fatalf("RequirePlatformAdmin(admin) error = %v", err)
	}
}

func TestRequireViewAndEdit(t *testing.T) {
	t.Parallel()

	store := authztest.NewStore()
	store.AddDatabase(databaseID, true)
	viewer := store.AddUser("viewer", auth.PlatformRoleUser)
	editor := store.AddUser("editor", auth.PlatformRoleUser)
	outsider := store.AddUser("outsider", auth.PlatformRoleUser)
	store.Grant("readers", databaseID, authz.AccessViewOnly, viewer.UserID)
	store.Grant("writers", databaseID, authz.AccessViewEdit, editor.UserID)
	policy := store.Policy()
	ctx := context.Background()

	if err := policy.RequireDatabaseView(ctx, viewer, databaseID); err != nil {
		t.Errorf("view_only RequireDatabaseView() error = %v", err)
	}
	if err := policy.RequireDatabaseView(ctx, editor, databaseID); err != nil {
		t.Errorf("view_edit RequireDatabaseView() error = %v", err)
	}
	if err := policy.RequireDatabaseEdit(ctx, viewer, databaseID); !errors.Is(err, authz.ErrDatabaseEditForbidden) {
		t.Errorf("view_only RequireDatabaseEdit() error = %v, want ErrDatabaseEditForbidden", err)
	}
	if err := policy.RequireDatabaseEdit(ctx, editor, databaseID); err != nil {
		t.Errorf("view_edit RequireDatabaseEdit() error = %v", err)
	}
	if err := policy.RequireDatabaseView(ctx, outsider, databaseID); !errors.Is(err, authz.ErrDatabaseNotFound) {
		t.Errorf("no grant RequireDatabaseView() error = %v, want ErrDatabaseNotFound", err)
	}
}

func TestRequireViewHidesUnknownAndInvalidDatabases(t *testing.T) {
	t.Parallel()

	store := authztest.NewStore()
	principal := store.AddUser("viewer", auth.PlatformRoleUser)
	policy := store.Policy()

	for _, id := range []string{otherDatabaseID, "not-a-uuid", "1' OR '1'='1"} {
		if err := policy.RequireDatabaseView(context.Background(), principal, id); !errors.Is(err, authz.ErrDatabaseNotFound) {
			t.Errorf("RequireDatabaseView(%q) error = %v, want ErrDatabaseNotFound", id, err)
		}
	}
}

func TestInactiveDatabaseOnlyRevealedToGrantedUsers(t *testing.T) {
	t.Parallel()

	store := authztest.NewStore()
	store.AddDatabase(databaseID, false)
	granted := store.AddUser("granted", auth.PlatformRoleUser)
	outsider := store.AddUser("outsider", auth.PlatformRoleUser)
	store.Grant("readers", databaseID, authz.AccessViewOnly, granted.UserID)
	policy := store.Policy()

	if err := policy.RequireDatabaseView(context.Background(), granted, databaseID); !errors.Is(err, authz.ErrDatabaseInactive) {
		t.Errorf("granted user error = %v, want ErrDatabaseInactive", err)
	}
	if err := policy.RequireDatabaseView(context.Background(), outsider, databaseID); !errors.Is(err, authz.ErrDatabaseNotFound) {
		t.Errorf("outsider error = %v, want ErrDatabaseNotFound", err)
	}
}

func TestInactiveUserIsRejected(t *testing.T) {
	t.Parallel()

	store := authztest.NewStore()
	store.AddDatabase(databaseID, true)
	principal := store.AddUser("inactive", auth.PlatformRoleUser)
	store.Grant("writers", databaseID, authz.AccessViewEdit, principal.UserID)
	store.SetUserActive(principal.UserID, false)
	policy := store.Policy()

	if err := policy.RequireDatabaseView(context.Background(), principal, databaseID); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Fatalf("RequireDatabaseView() error = %v, want ErrUnauthenticated", err)
	}
	grants, err := policy.AccessibleDatabases(context.Background(), principal)
	if err != nil || len(grants) != 0 {
		t.Fatalf("AccessibleDatabases() = %v, %v; want empty", grants, err)
	}
}

func TestRequirePlatformAdmin(t *testing.T) {
	t.Parallel()

	policy := authztest.NewStore().Policy()
	ctx := context.Background()
	if err := policy.RequirePlatformAdmin(ctx, auth.Principal{}); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("empty principal error = %v, want ErrUnauthenticated", err)
	}
	user := auth.Principal{UserID: databaseID, PlatformRole: auth.PlatformRoleUser}
	if err := policy.RequirePlatformAdmin(ctx, user); !errors.Is(err, authz.ErrPlatformAdminRequired) {
		t.Errorf("user error = %v, want ErrPlatformAdminRequired", err)
	}
}

func TestAccessibleDatabasesOnlyReturnsGrantedActiveDatabases(t *testing.T) {
	t.Parallel()

	store := authztest.NewStore()
	store.AddDatabase(databaseID, true)
	store.AddDatabase(otherDatabaseID, false)
	principal := store.AddUser("member", auth.PlatformRoleUser)
	store.Grant("readers", databaseID, authz.AccessViewOnly, principal.UserID)
	store.Grant("writers", databaseID, authz.AccessViewEdit, principal.UserID)
	store.Grant("inactive", otherDatabaseID, authz.AccessViewEdit, principal.UserID)

	grants, err := store.Policy().AccessibleDatabases(context.Background(), principal)
	if err != nil {
		t.Fatalf("AccessibleDatabases() error = %v", err)
	}
	if len(grants) != 1 || grants[0].DatabaseID != databaseID || grants[0].Level != authz.AccessViewEdit {
		t.Fatalf("AccessibleDatabases() = %+v, want only view_edit on active database", grants)
	}
}

func TestRepositoryErrorsPropagate(t *testing.T) {
	t.Parallel()

	store := authztest.NewStore()
	principal := store.AddUser("member", auth.PlatformRoleUser)
	store.Err = errors.New("platform db down")
	if err := store.Policy().RequireDatabaseView(context.Background(), principal, databaseID); !errors.Is(err, store.Err) {
		t.Fatalf("RequireDatabaseView() error = %v, want repository error", err)
	}
	if _, ok := authz.Describe(store.Err); ok {
		t.Fatal("Describe() treated an infrastructure error as a policy error")
	}
}

func TestDescribeMapsPolicyErrors(t *testing.T) {
	t.Parallel()

	cases := map[error]int{
		authz.ErrUnauthenticated:       http.StatusUnauthorized,
		authz.ErrPlatformAdminRequired: http.StatusForbidden,
		authz.ErrDatabaseNotFound:      http.StatusNotFound,
		authz.ErrDatabaseInactive:      http.StatusConflict,
		authz.ErrDatabaseEditForbidden: http.StatusForbidden,
	}
	for err, status := range cases {
		described, ok := authz.Describe(err)
		if !ok || described.Status != status || described.Code == "" {
			t.Errorf("Describe(%v) = %+v, %v; want status %d", err, described, ok, status)
		}
	}
}

func TestAccessLevelHelpers(t *testing.T) {
	t.Parallel()

	if authz.Highest() != authz.AccessNone {
		t.Error("Highest() of nothing must be none")
	}
	if authz.Highest(authz.AccessViewEdit, authz.AccessViewOnly) != authz.AccessViewEdit {
		t.Error("Highest() must choose view_edit")
	}
	if !authz.AccessViewEdit.CanView() || !authz.AccessViewOnly.CanView() || authz.AccessNone.CanView() {
		t.Error("CanView() ranking is wrong")
	}
	if authz.AccessViewOnly.CanEdit() || !authz.AccessViewEdit.CanEdit() {
		t.Error("CanEdit() ranking is wrong")
	}
	for _, raw := range []string{"view_only", " view_edit "} {
		if _, err := authz.ParseGrantLevel(raw); err != nil {
			t.Errorf("ParseGrantLevel(%q) error = %v", raw, err)
		}
	}
	for _, raw := range []string{"none", "admin", "", "VIEW_ONLY"} {
		if _, err := authz.ParseGrantLevel(raw); !errors.Is(err, authz.ErrInvalidAccessLevel) {
			t.Errorf("ParseGrantLevel(%q) error = %v, want ErrInvalidAccessLevel", raw, err)
		}
	}
}
