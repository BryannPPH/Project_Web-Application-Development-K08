package viewer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

// DefaultQueryTimeout membatasi total waktu satu operasi viewer.
const DefaultQueryTimeout = 15 * time.Second

// statementTimeout dipasang lewat set_config(..., true) agar hanya berlaku
// di transaksi viewer (setara SET LOCAL).
const statementTimeout = "10s"

// Service menjalankan operasi viewer: authorize -> open -> read-only tx -> close.
type Service struct {
	policy   authz.Service
	provider targetdb.Provider
	timeout  time.Duration
}

func NewService(policy authz.Service, provider targetdb.Provider, timeout time.Duration) (*Service, error) {
	if policy == nil || provider == nil {
		return nil, errors.New("viewer service requires policy and target provider")
	}
	if timeout <= 0 {
		timeout = DefaultQueryTimeout
	}
	return &Service{policy: policy, provider: provider, timeout: timeout}, nil
}

func (s *Service) Schemas(ctx context.Context, principal auth.Principal, databaseID string) ([]Schema, error) {
	var schemas []Schema
	err := s.read(ctx, principal, databaseID, func(ctx context.Context, q querier) error {
		var err error
		schemas, err = listSchemas(ctx, q)
		return err
	})
	return schemas, err
}

func (s *Service) Tables(ctx context.Context, principal auth.Principal, databaseID, schema string) ([]Table, error) {
	var tables []Table
	err := s.read(ctx, principal, databaseID, func(ctx context.Context, q querier) error {
		var err error
		tables, err = listTables(ctx, q, schema)
		return err
	})
	return tables, err
}

// TableDetail menggabungkan columns, constraints, dan indexes dalam satu response.
func (s *Service) TableDetail(ctx context.Context, principal auth.Principal, databaseID, schema, table string) (TableDetail, error) {
	var detail TableDetail
	err := s.read(ctx, principal, databaseID, func(ctx context.Context, q querier) error {
		oid, resolved, err := resolveTable(ctx, q, schema, table)
		if err != nil {
			return err
		}
		detail.Table = resolved
		if detail.Columns, err = listColumns(ctx, q, oid); err != nil {
			return err
		}
		if detail.Constraints, err = listConstraints(ctx, q, oid); err != nil {
			return err
		}
		detail.Indexes, err = listIndexes(ctx, q, oid)
		return err
	})
	return detail, err
}

// Rows mengambil satu halaman data. Urutan stabil memakai primary key bila ada.
func (s *Service) Rows(ctx context.Context, principal auth.Principal, databaseID, schema, table string, page Page) (RowsPage, error) {
	if page.Limit < 1 || page.Limit > MaxLimit || page.Offset < 0 {
		return RowsPage{}, ErrInvalidPagination
	}
	var result RowsPage
	err := s.read(ctx, principal, databaseID, func(ctx context.Context, q querier) error {
		oid, resolved, err := resolveTable(ctx, q, schema, table)
		if err != nil {
			return err
		}
		columns, err := listColumns(ctx, q, oid)
		if err != nil {
			return err
		}
		orderBy, err := primaryKeyColumns(ctx, q, oid)
		if err != nil {
			return err
		}
		result, err = fetchRows(ctx, q, resolved, columns, orderBy, page)
		return err
	})
	return result, err
}

// read adalah satu-satunya jalur viewer ke database target:
//  1. authz.RequireDatabaseView SEBELUM koneksi dibuka;
//  2. buka koneksi lewat provider (credential tidak terlihat oleh viewer);
//  3. BEGIN; SET TRANSACTION READ ONLY; statement_timeout lokal;
//  4. jalankan query katalog/data;
//  5. ROLLBACK dan Close pada semua jalur (defer).
func (s *Service) read(
	ctx context.Context,
	principal auth.Principal,
	databaseID string,
	operation func(ctx context.Context, q querier) error,
) error {
	if err := s.policy.RequireDatabaseView(ctx, principal, databaseID); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	connection, err := s.provider.Open(ctx, databaseID)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close(context.WithoutCancel(ctx)) }()

	transaction, err := connection.Begin(ctx)
	if err != nil {
		return classifyQueryError(err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := transaction.Exec(ctx, "SET TRANSACTION READ ONLY"); err != nil {
		return classifyQueryError(err)
	}
	if _, err := transaction.Exec(ctx, "SELECT set_config('statement_timeout', $1, true)", statementTimeout); err != nil {
		return classifyQueryError(err)
	}
	if err := operation(ctx, transaction); err != nil {
		return classifyQueryError(err)
	}
	return nil
}

// classifyQueryError mempertahankan error domain dan mengubah error target
// menjadi kategori aman tanpa membawa pesan SQL asli.
func classifyQueryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrSchemaNotFound) || errors.Is(err, ErrTableNotFound) || errors.Is(err, ErrInvalidPagination) {
		return err
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case "42501":
			return ErrTargetPermissionDenied
		case "57014":
			return ErrTargetTimeout
		}
		return fmt.Errorf("%w (sqlstate %s)", ErrTargetQueryFailed, pgError.Code)
	}
	if errors.Is(err, context.DeadlineExceeded) || pgconn.Timeout(err) {
		return ErrTargetTimeout
	}
	if errors.Is(err, targetdb.ErrTargetUnavailable) {
		return err
	}
	// Error non-SQL (koneksi putus, decode gagal). Pesan asli hanya untuk log
	// internal; handler tetap mengirim pesan generik ke client.
	return fmt.Errorf("%w: %v", ErrTargetQueryFailed, err)
}
