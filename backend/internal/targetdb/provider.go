package targetdb

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Connection adalah koneksi tunggal ke database target.
//
// Lifecycle: setiap Connection dari Provider.Open WAJIB di-Close pada semua
// jalur (sukses maupun error), sebaiknya lewat defer segera setelah Open.
// Close aman dipanggil lebih dari sekali.
type Connection interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (Transaction, error)
	Close(ctx context.Context) error
}

// Transaction adalah kontrak transaksi bersama untuk viewer dan migration.
// pgx.Tx memenuhi interface ini tanpa adapter tambahan.
type Transaction interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// Provider membuka koneksi target berdasarkan managed database ID.
// Provider TIDAK melakukan authorization; caller wajib memanggil
// authz.Service.RequireDatabaseView/Edit sebelum Open.
type Provider interface {
	Open(ctx context.Context, databaseID string) (Connection, error)
}

// ConfigResolver menyediakan konfigurasi koneksi plaintext (hasil dekripsi).
// Diimplementasikan oleh modul managed database; consumer tidak perlu membaca
// tabel managed_databases sendiri.
type ConfigResolver interface {
	ResolveConnection(ctx context.Context, databaseID string) (ConnectionConfig, error)
}

// Connector membuka koneksi dari konfigurasi; dapat diganti pada unit test.
type Connector func(ctx context.Context, config ConnectionConfig, timeout time.Duration) (Connection, error)

// PGProvider adalah Provider produksi berbasis pgx.Conn (satu koneksi per Open).
type PGProvider struct {
	resolver       ConfigResolver
	connect        Connector
	connectTimeout time.Duration
	logger         *slog.Logger
}

var _ Provider = (*PGProvider)(nil)

// NewPGProvider membuat provider. connectTimeout <= 0 memakai DefaultConnectTimeout.
func NewPGProvider(resolver ConfigResolver, connectTimeout time.Duration, logger *slog.Logger) *PGProvider {
	if connectTimeout <= 0 {
		connectTimeout = DefaultConnectTimeout
	}
	return &PGProvider{
		resolver:       resolver,
		connect:        Connect,
		connectTimeout: connectTimeout,
		logger:         logger,
	}
}

// Open me-resolve credential, membuka koneksi, lalu segera melepas config plaintext.
func (p *PGProvider) Open(ctx context.Context, databaseID string) (Connection, error) {
	config, err := p.resolver.ResolveConnection(ctx, databaseID)
	if err != nil {
		return nil, err
	}
	connection, err := p.connect(ctx, config, p.connectTimeout)
	config.Password = ""
	if err != nil {
		var unavailable *UnavailableError
		if errors.As(err, &unavailable) && p.logger != nil {
			// Log hanya ID dan kategori; tidak ada DSN, host, user, atau password.
			p.logger.WarnContext(ctx, "target database connection failed",
				"database_id", databaseID,
				"reason", string(unavailable.Reason),
				"sqlstate", unavailable.SQLState,
			)
		}
		return nil, err
	}
	return connection, nil
}

// Connect membuka satu koneksi pgx dengan timeout. Error selalu berupa
// *UnavailableError yang sudah diredaksi.
func Connect(ctx context.Context, config ConnectionConfig, timeout time.Duration) (Connection, error) {
	pgxConfig, err := config.pgxConfig(timeout)
	if err != nil {
		return nil, newUnavailableError(config.DatabaseID, err)
	}
	connectCtx, cancel := context.WithTimeout(ctx, pgxConfig.ConnectTimeout)
	defer cancel()

	connection, err := pgx.ConnectConfig(connectCtx, pgxConfig)
	pgxConfig.Password = ""
	if err != nil {
		return nil, newUnavailableError(config.DatabaseID, err)
	}
	return &pgxConnection{conn: connection}, nil
}

// Ping membuka koneksi, menjalankan SELECT 1, lalu menutupnya. Dipakai fitur
// "test connection" admin.
func Ping(ctx context.Context, config ConnectionConfig, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultConnectTimeout
	}
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	connection, err := Connect(pingCtx, config, timeout)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close(context.WithoutCancel(ctx)) }()

	var one int
	if err := connection.QueryRow(pingCtx, "SELECT 1").Scan(&one); err != nil {
		return newUnavailableError(config.DatabaseID, err)
	}
	return nil
}

type pgxConnection struct {
	conn *pgx.Conn
}

func (c *pgxConnection) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return c.conn.Query(ctx, sql, args...)
}

func (c *pgxConnection) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return c.conn.QueryRow(ctx, sql, args...)
}

func (c *pgxConnection) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return c.conn.Exec(ctx, sql, args...)
}

func (c *pgxConnection) Begin(ctx context.Context) (Transaction, error) {
	return c.conn.Begin(ctx)
}

func (c *pgxConnection) Close(ctx context.Context) error {
	if c.conn.IsClosed() {
		return nil
	}
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return c.conn.Close(closeCtx)
}
