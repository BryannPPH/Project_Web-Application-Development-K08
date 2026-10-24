package schemadiff

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

const (
	defaultTimeout       = 20 * time.Second
	introspectionTimeout = "10s"
	maxSchemaNameLength  = 128
)

// Authorizer cukup memeriksa akses lihat: diff hanya membaca katalog.
// Membuat draft migration dari hasilnya tetap butuh View + Edit di endpoint migration.
type Authorizer interface {
	RequireDatabaseView(context.Context, auth.Principal, string) error
}

type Service struct {
	authorizer Authorizer
	provider   targetdb.Provider
	timeout    time.Duration
}

func NewService(authorizer Authorizer, provider targetdb.Provider) (*Service, error) {
	if authorizer == nil || provider == nil {
		return nil, errors.New("schema diff service requires authorizer and target provider")
	}
	return &Service{authorizer: authorizer, provider: provider, timeout: defaultTimeout}, nil
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func validRef(ref Ref) bool {
	return uuidPattern.MatchString(ref.DatabaseID) && strings.TrimSpace(ref.Schema) != "" && len(ref.Schema) <= maxSchemaNameLength
}

// Compare menghasilkan perubahan agar schema target menyerupai schema sumber.
func (s *Service) Compare(ctx context.Context, principal auth.Principal, request Request) (Result, error) {
	if !validRef(request.Source) || !validRef(request.Target) {
		return Result{}, ErrInvalidInput
	}
	// Kedua sisi diotorisasi sebelum koneksi mana pun dibuka.
	for _, databaseID := range []string{request.Source.DatabaseID, request.Target.DatabaseID} {
		if err := s.authorizer.RequireDatabaseView(ctx, principal, databaseID); err != nil {
			return Result{}, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	desired, err := s.snapshot(ctx, request.Source)
	if err != nil {
		return Result{}, err
	}
	current, err := s.snapshot(ctx, request.Target)
	if err != nil {
		return Result{}, err
	}
	if desired.Engine != current.Engine {
		return Result{}, ErrEngineMismatch
	}

	result := Diff(desired, current)
	result.Source, result.Target = request.Source, request.Target
	result.Executable = current.Engine == targetdb.EnginePostgreSQL
	result.Notes = []string{"SQL ini adalah kandidat otomatis. Tinjau dan ubah bila perlu sebelum dijadikan draft migration."}
	if !result.Executable {
		result.Notes = append(result.Notes,
			"Target MySQL melakukan implicit commit pada DDL sehingga SQL ini tidak dapat dijalankan sebagai migration atomik; salin dan jalankan secara terkontrol.")
	}
	if result.Summary.Destructive > 0 {
		result.Notes = append(result.Notes, "Ada perubahan destruktif (menghapus table/kolom). Pastikan data sudah dicadangkan.")
	}
	return result, nil
}

// snapshot membaca katalog dalam transaksi READ ONLY yang selalu di-rollback.
func (s *Service) snapshot(ctx context.Context, ref Ref) (*Snapshot, error) {
	connection, err := s.provider.Open(ctx, ref.DatabaseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = connection.Close(context.WithoutCancel(ctx)) }()

	transaction, err := connection.Begin(ctx, targetdb.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	if connection.Engine() == targetdb.EngineMySQL {
		return introspectMySQL(ctx, transaction, ref.Schema)
	}
	for _, setting := range [][2]string{{"statement_timeout", introspectionTimeout}, {"search_path", "pg_catalog"}} {
		if _, err := transaction.Exec(ctx, "SELECT set_config($1, $2, true)", setting[0], setting[1]); err != nil {
			return nil, err
		}
	}
	return introspectPostgres(ctx, transaction, ref.Schema)
}
