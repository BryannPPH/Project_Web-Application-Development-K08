package targetdb

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

type Row interface {
	Scan(dest ...any) error
}

type Rows interface {
	Close()
	Err() error
	Next() bool
	Scan(dest ...any) error
	Values() ([]any, error)
}

type CommandResult interface {
	RowsAffected() int64
}

type TxOptions struct {
	ReadOnly bool
}

type Connection interface {
	Engine() Engine
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
	Exec(ctx context.Context, sql string, args ...any) (CommandResult, error)
	Begin(ctx context.Context, options ...TxOptions) (Transaction, error)
	Close(ctx context.Context) error
}

type Transaction interface {
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
	Exec(ctx context.Context, sql string, args ...any) (CommandResult, error)
	ExecScript(ctx context.Context, sql string) (CommandResult, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type Provider interface {
	Open(ctx context.Context, databaseID string) (Connection, error)
}

type ConfigResolver interface {
	ResolveConnection(ctx context.Context, databaseID string) (ConnectionConfig, error)
}

type Connector func(ctx context.Context, config ConnectionConfig, timeout time.Duration) (Connection, error)

type MultiProvider struct {
	resolver       ConfigResolver
	connect        Connector
	connectMySQL   Connector
	connectTimeout time.Duration
	logger         *slog.Logger
}

func NewProvider(resolver ConfigResolver, connectTimeout time.Duration, logger *slog.Logger) *MultiProvider {
	if connectTimeout <= 0 {
		connectTimeout = DefaultConnectTimeout
	}
	return &MultiProvider{
		resolver:       resolver,
		connect:        Connect,
		connectMySQL:   ConnectMySQL,
		connectTimeout: connectTimeout,
		logger:         logger,
	}
}

func NewPGProvider(resolver ConfigResolver, connectTimeout time.Duration, logger *slog.Logger) *MultiProvider {
	return NewProvider(resolver, connectTimeout, logger)
}

func (p *MultiProvider) Open(ctx context.Context, databaseID string) (Connection, error) {
	config, err := p.resolver.ResolveConnection(ctx, databaseID)
	if err != nil {
		return nil, err
	}
	engine := NormalizeEngine(config.Engine)
	config.Engine = engine
	connector := p.connect
	if engine == EngineMySQL {
		connector = p.connectMySQL
	}
	connection, err := connector(ctx, config, p.connectTimeout)
	config.Password = ""
	if err != nil {
		var unavailable *UnavailableError
		if errors.As(err, &unavailable) && p.logger != nil {
			p.logger.WarnContext(ctx, "target database connection failed",
				"database_id", databaseID,
				"engine", engine,
				"reason", string(unavailable.Reason),
				"sqlstate", unavailable.SQLState,
			)
		}
		return nil, err
	}
	return connection, nil
}

func Connect(ctx context.Context, config ConnectionConfig, timeout time.Duration) (Connection, error) {
	config.Engine = NormalizeEngine(config.Engine)
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

func Ping(ctx context.Context, config ConnectionConfig, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultConnectTimeout
	}
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	connector := Connector(Connect)
	if NormalizeEngine(config.Engine) == EngineMySQL {
		connector = ConnectMySQL
	}
	connection, err := connector(pingCtx, config, timeout)
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

func (*pgxConnection) Engine() Engine { return EnginePostgreSQL }

func (c *pgxConnection) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	return c.conn.Query(ctx, sql, args...)
}

func (c *pgxConnection) QueryRow(ctx context.Context, sql string, args ...any) Row {
	return c.conn.QueryRow(ctx, sql, args...)
}

func (c *pgxConnection) Exec(ctx context.Context, sql string, args ...any) (CommandResult, error) {
	return c.conn.Exec(ctx, sql, args...)
}

func (c *pgxConnection) Begin(ctx context.Context, options ...TxOptions) (Transaction, error) {
	var txOptions pgx.TxOptions
	if len(options) > 0 && options[0].ReadOnly {
		txOptions.AccessMode = pgx.ReadOnly
	}
	tx, err := c.conn.BeginTx(ctx, txOptions)
	if err != nil {
		return nil, err
	}
	return &pgxTransaction{tx: tx}, nil
}

func (c *pgxConnection) Close(ctx context.Context) error {
	if c.conn.IsClosed() {
		return nil
	}
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return c.conn.Close(closeCtx)
}

type pgxTransaction struct {
	tx pgx.Tx
}

func (t *pgxTransaction) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	return t.tx.Query(ctx, sql, args...)
}

func (t *pgxTransaction) QueryRow(ctx context.Context, sql string, args ...any) Row {
	return t.tx.QueryRow(ctx, sql, args...)
}

func (t *pgxTransaction) Exec(ctx context.Context, sql string, args ...any) (CommandResult, error) {
	return t.tx.Exec(ctx, sql, args...)
}

func (t *pgxTransaction) ExecScript(ctx context.Context, sql string) (CommandResult, error) {
	return t.tx.Exec(ctx, sql, pgx.QueryExecModeSimpleProtocol)
}

func (t *pgxTransaction) Commit(ctx context.Context) error   { return t.tx.Commit(ctx) }
func (t *pgxTransaction) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }
