package viewer

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz/authztest"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb/targetdbtest"
)

const databaseID = "11111111-1111-4111-8111-111111111111"

func TestParsePage(t *testing.T) {
	t.Parallel()

	valid := []struct {
		limit, offset string
		want          Page
	}{
		{"", "", Page{Limit: 50, Offset: 0}},
		{"1", "0", Page{Limit: 1, Offset: 0}},
		{"100", "250", Page{Limit: 100, Offset: 250}},
		{" 25 ", " 5 ", Page{Limit: 25, Offset: 5}},
	}
	for _, tc := range valid {
		got, err := ParsePage(tc.limit, tc.offset)
		if err != nil || got != tc.want {
			t.Errorf("ParsePage(%q, %q) = %+v, %v; want %+v", tc.limit, tc.offset, got, err, tc.want)
		}
	}

	invalid := [][2]string{
		{"0", ""}, {"101", ""}, {"-1", ""}, {"abc", ""}, {"1.5", ""},
		{"", "-1"}, {"", "x"}, {"", "99999999999"}, {"10; DROP TABLE x", ""},
	}
	for _, tc := range invalid {
		if _, err := ParsePage(tc[0], tc[1]); !errors.Is(err, ErrInvalidPagination) {
			t.Errorf("ParsePage(%q, %q) error = %v, want ErrInvalidPagination", tc[0], tc[1], err)
		}
	}
}

func TestBuildRowsQueryQuotesIdentifiers(t *testing.T) {
	t.Parallel()

	query := buildRowsQuery(`public"; DROP TABLE users; --`, `we"ird table`, []string{`id`, `Order"By`})
	want := `SELECT * FROM "public""; DROP TABLE users; --"."we""ird table" ORDER BY "id", "Order""By" LIMIT $1 OFFSET $2`
	if query != want {
		t.Fatalf("buildRowsQuery() =\n%s\nwant\n%s", query, want)
	}
	if plain := buildRowsQuery("public", "items", nil); plain != `SELECT * FROM "public"."items" LIMIT $1 OFFSET $2` {
		t.Fatalf("buildRowsQuery(no pk) = %s", plain)
	}
}

func TestIsHiddenSchema(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"pg_catalog", "information_schema", "pg_toast", "pg_temp_3", "pg_toast_temp_3"} {
		if !IsHiddenSchema(name) {
			t.Errorf("IsHiddenSchema(%q) = false", name)
		}
	}
	for _, name := range []string{"public", "sales", "pg_custom", "temp"} {
		if IsHiddenSchema(name) {
			t.Errorf("IsHiddenSchema(%q) = true", name)
		}
	}
}

func TestNormalizeValueProducesSafeJSON(t *testing.T) {
	t.Parallel()

	var numeric pgtype.Numeric
	if err := numeric.Scan("12345.6789"); err != nil {
		t.Fatal(err)
	}
	timestamp := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)
	cases := []struct {
		in   any
		want any
	}{
		{nil, nil},
		{true, true},
		{"text", "text"},
		{int32(7), int32(7)},
		{int64(1 << 60), "1152921504606846976"},
		{math.NaN(), "NaN"},
		{math.Inf(1), "+Inf"},
		{timestamp, "2026-10-22T08:30:00Z"},
		{[]byte{0xde, 0xad}, `\xdead`},
		{[16]byte{0x6f, 0x1c, 0x2a, 0x3b, 0x4d, 0x5e, 0x4f, 0x60, 0x8a, 0x7b, 0x9c, 0x0d, 0x1e, 0x2f, 0x3a, 0x4b}, "6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4b"},
		{numeric, "12345.6789"},
		{big.NewInt(42), "42"},
	}
	for _, tc := range cases {
		got, truncated := normalizeValue(tc.in)
		if truncated || got != tc.want {
			t.Errorf("normalizeValue(%#v) = %#v (truncated=%v), want %#v", tc.in, got, truncated, tc.want)
		}
		if _, err := json.Marshal(got); err != nil {
			t.Errorf("normalizeValue(%#v) not JSON-encodable: %v", tc.in, err)
		}
	}

	jsonValue, _ := normalizeValue(map[string]any{"score": math.Inf(-1), "tags": []any{"a", int64(1)}})
	encoded, err := json.Marshal(jsonValue)
	if err != nil || !strings.Contains(string(encoded), `"-Inf"`) {
		t.Fatalf("nested json = %s, %v", encoded, err)
	}

	long := strings.Repeat("é", maxCellRunes+10)
	value, truncated := normalizeValue(long)
	if !truncated || len([]rune(value.(string))) != maxCellRunes+1 {
		t.Fatalf("long value was not truncated correctly")
	}
}

type viewerFixture struct {
	service    *Service
	provider   *targetdbtest.Provider
	connection *targetdbtest.Connection
	viewer     auth.Principal
	editor     auth.Principal
	outsider   auth.Principal
	admin      auth.Principal
}

func newViewerFixture(t *testing.T) *viewerFixture {
	t.Helper()
	store := authztest.NewStore()
	store.AddDatabase(databaseID, true)
	f := &viewerFixture{
		connection: &targetdbtest.Connection{},
		viewer:     store.AddUser("viewer", auth.PlatformRoleUser),
		editor:     store.AddUser("editor", auth.PlatformRoleUser),
		outsider:   store.AddUser("outsider", auth.PlatformRoleUser),
		admin:      store.AddUser("admin", auth.PlatformRoleAdmin),
	}
	store.Grant("readers", databaseID, authz.AccessViewOnly, f.viewer.UserID)
	store.Grant("writers", databaseID, authz.AccessViewEdit, f.editor.UserID)
	f.provider = &targetdbtest.Provider{Connection: f.connection}
	var err error
	f.service, err = NewService(store.Policy(), f.provider, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestDeniedPrincipalsNeverOpenTargetConnection(t *testing.T) {
	t.Parallel()

	f := newViewerFixture(t)
	for _, principal := range []auth.Principal{f.outsider, f.admin} {
		_, err := f.service.Schemas(context.Background(), principal, databaseID)
		if !errors.Is(err, authz.ErrDatabaseNotFound) {
			t.Errorf("Schemas(%s) error = %v, want ErrDatabaseNotFound", principal.Username, err)
		}
	}
	if _, err := f.service.Rows(context.Background(), f.outsider, databaseID, "public", "items", Page{Limit: 10}); !errors.Is(err, authz.ErrDatabaseNotFound) {
		t.Errorf("Rows(outsider) error = %v", err)
	}
	if f.provider.OpenCount() != 0 {
		t.Fatalf("provider opened %d connections for unauthorized principals", f.provider.OpenCount())
	}
}

func TestViewOnlyAndViewEditReachTarget(t *testing.T) {
	t.Parallel()

	f := newViewerFixture(t)
	beginErr := errors.New("begin failed")
	f.connection.BeginFunc = func(context.Context) (targetdb.Transaction, error) { return nil, beginErr }

	for _, principal := range []auth.Principal{f.viewer, f.editor} {
		_, err := f.service.Schemas(context.Background(), principal, databaseID)
		if !errors.Is(err, ErrTargetQueryFailed) {
			t.Errorf("Schemas(%s) error = %v, want ErrTargetQueryFailed from fake", principal.Username, err)
		}
	}
	if f.provider.OpenCount() != 2 {
		t.Fatalf("OpenCount() = %d, want 2", f.provider.OpenCount())
	}
	if !f.connection.Closed() {
		t.Fatal("connection must be closed when Begin fails")
	}
}

func TestOpenFailureIsReturnedSafely(t *testing.T) {
	t.Parallel()

	f := newViewerFixture(t)
	f.provider.OpenErr = &targetdb.UnavailableError{DatabaseID: databaseID, Reason: targetdb.ReasonNetwork}
	_, err := f.service.Tables(context.Background(), f.viewer, databaseID, "public")
	if !errors.Is(err, targetdb.ErrTargetUnavailable) {
		t.Fatalf("Tables() error = %v, want ErrTargetUnavailable", err)
	}
}

func TestRowsRejectsInvalidPageBeforeAuthorization(t *testing.T) {
	t.Parallel()

	f := newViewerFixture(t)
	if _, err := f.service.Rows(context.Background(), f.viewer, databaseID, "public", "items", Page{Limit: 500}); !errors.Is(err, ErrInvalidPagination) {
		t.Fatalf("Rows() error = %v", err)
	}
	if f.provider.OpenCount() != 0 {
		t.Fatal("invalid pagination must not open a connection")
	}
}

func TestClassifyQueryError(t *testing.T) {
	t.Parallel()

	cases := map[error]error{
		&pgconn.PgError{Code: "42501"}: ErrTargetPermissionDenied,
		&pgconn.PgError{Code: "57014"}: ErrTargetTimeout,
		&pgconn.PgError{Code: "42P01"}: ErrTargetQueryFailed,
		context.DeadlineExceeded:       ErrTargetTimeout,
		ErrTableNotFound:               ErrTableNotFound,
		errors.New("unexpected EOF"):   ErrTargetQueryFailed,
	}
	for input, want := range cases {
		if got := classifyQueryError(input); !errors.Is(got, want) {
			t.Errorf("classifyQueryError(%v) = %v, want %v", input, got, want)
		}
	}
	leaked := classifyQueryError(&pgconn.PgError{Code: "42P01", Message: `relation "secret_table" does not exist`})
	if strings.Contains(leaked.Error(), "secret_table") {
		t.Fatalf("classified error leaked SQL message: %v", leaked)
	}
}

func TestHandlerRejectsInvalidPagination(t *testing.T) {
	t.Parallel()

	f := newViewerFixture(t)
	handler := NewHandler(f.service, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/databases/"+databaseID+"/schemas/public/tables/items/rows?limit=1000", nil)
	request.SetPathValue("id", databaseID)
	request.SetPathValue("schema", "public")
	request.SetPathValue("table", "items")
	request = request.WithContext(auth.WithPrincipal(request.Context(), f.viewer))
	recorder := httptest.NewRecorder()
	handler.rows(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "INVALID_PAGINATION") {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}
