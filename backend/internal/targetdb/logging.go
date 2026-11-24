package targetdb

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/observability"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/requestmeta"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Activity struct {
	JobID         string
	MigrationID   string
	ExecutionID   string
	Action        string
	Attempt       int
	BatchNumber   int64
	CorrelationID string
}

type activityContextKey struct{}

func WithActivity(ctx context.Context, activity Activity) context.Context {
	return context.WithValue(ctx, activityContextKey{}, activity)
}

func ActivityFromContext(ctx context.Context) Activity {
	activity, _ := ctx.Value(activityContextKey{}).(Activity)
	return activity
}

type loggingProvider struct {
	next   Provider
	logger *slog.Logger
}

func NewLoggingProvider(next Provider, logger *slog.Logger) Provider {
	if logger == nil {
		logger = slog.Default()
	}
	return &loggingProvider{next: next, logger: logger}
}

func (p *loggingProvider) Open(ctx context.Context, databaseID string) (Connection, error) {
	started := time.Now()
	ctx, span := otel.Tracer("dbmp/targetdb").Start(ctx, "db.connection.open",
		trace.WithAttributes(attribute.String("db.system", "target"), attribute.String("db.namespace", databaseID)))
	connection, err := p.next.Open(ctx, databaseID)
	fields := requestFields(ctx, databaseID)
	fields = append(fields, "duration_ms", time.Since(started).Milliseconds())
	if err != nil {
		fields = append(fields, "status", "failed", "error", safeDBError(err))
		span.RecordError(errors.New("target database connection failed"))
		span.SetStatus(codes.Error, "target connection failed")
		p.logger.ErrorContext(ctx, "target database connection failed", append([]any{"event", "target_connection_failed"}, fields...)...)
		span.End()
		return nil, err
	}
	fields = append(fields, "status", "succeeded")
	p.logger.InfoContext(ctx, "target database connection opened", append([]any{"event", "target_connection_opened"}, fields...)...)
	span.SetAttributes(attribute.String("db.system", string(connection.Engine())))
	span.End()
	return &loggingConnection{Connection: connection, databaseID: databaseID, logger: p.logger}, nil
}

type loggingConnection struct {
	Connection
	databaseID string
	logger     *slog.Logger
}

func (c *loggingConnection) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	ctx, span, started := startQuery(ctx, c.logger, c.databaseID, "query", query, args)
	rows, err := c.Connection.Query(ctx, query, args...)
	if err != nil {
		finishQuery(ctx, c.logger, c.databaseID, "query", query, args, started, 0, err, span)
		return nil, err
	}
	return &loggingRows{Rows: rows, ctx: ctx, databaseID: c.databaseID, logger: c.logger,
		query: query, args: args, started: started, span: span}, nil
}

func (c *loggingConnection) QueryRow(ctx context.Context, query string, args ...any) Row {
	ctx, span, started := startQuery(ctx, c.logger, c.databaseID, "query_row", query, args)
	return &loggingRow{Row: c.Connection.QueryRow(ctx, query, args...), ctx: ctx, databaseID: c.databaseID,
		logger: c.logger, query: query, args: args, started: started, span: span}
}

func (c *loggingConnection) Exec(ctx context.Context, query string, args ...any) (CommandResult, error) {
	ctx, span, started := startQuery(ctx, c.logger, c.databaseID, "exec", query, args)
	result, err := c.Connection.Exec(ctx, query, args...)
	var affected int64
	if result != nil {
		affected = result.RowsAffected()
	}
	finishQuery(ctx, c.logger, c.databaseID, "exec", query, args, started, affected, err, span)
	return result, err
}

func (c *loggingConnection) Begin(ctx context.Context, options ...TxOptions) (Transaction, error) {
	started := time.Now()
	ctx, span := otel.Tracer("dbmp/targetdb").Start(ctx, "db.transaction",
		trace.WithAttributes(attribute.String("db.namespace", c.databaseID)))
	transaction, err := c.Connection.Begin(ctx, options...)
	logTransaction(ctx, c.logger, c.databaseID, "started", time.Since(started), err)
	if err != nil {
		span.RecordError(errors.New("target transaction begin failed"))
		span.SetStatus(codes.Error, "transaction begin failed")
		span.End()
		return nil, err
	}
	return &loggingTransaction{Transaction: transaction, databaseID: c.databaseID, engine: c.Connection.Engine(),
		logger: c.logger, span: span}, nil
}

type loggingTransaction struct {
	Transaction
	databaseID string
	engine     Engine
	logger     *slog.Logger
	span       trace.Span
}

func (tx *loggingTransaction) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	return (&loggingConnection{databaseID: tx.databaseID, logger: tx.logger, Connection: transactionConnection{Transaction: tx.Transaction, engine: tx.engine}}).
		Query(ctx, query, args...)
}

func (tx *loggingTransaction) QueryRow(ctx context.Context, query string, args ...any) Row {
	return (&loggingConnection{databaseID: tx.databaseID, logger: tx.logger, Connection: transactionConnection{Transaction: tx.Transaction, engine: tx.engine}}).
		QueryRow(ctx, query, args...)
}

func (tx *loggingTransaction) Exec(ctx context.Context, query string, args ...any) (CommandResult, error) {
	return (&loggingConnection{databaseID: tx.databaseID, logger: tx.logger, Connection: transactionConnection{Transaction: tx.Transaction, engine: tx.engine}}).
		Exec(ctx, query, args...)
}

func (tx *loggingTransaction) ExecScript(ctx context.Context, query string) (CommandResult, error) {
	ctx, span, started := startQuery(ctx, tx.logger, tx.databaseID, "exec_script", query, nil)
	result, err := tx.Transaction.ExecScript(ctx, query)
	var affected int64
	if result != nil {
		affected = result.RowsAffected()
	}
	finishQuery(ctx, tx.logger, tx.databaseID, "exec_script", query, nil, started, affected, err, span)
	return result, err
}

func (tx *loggingTransaction) Commit(ctx context.Context) error {
	started := time.Now()
	err := tx.Transaction.Commit(ctx)
	logTransaction(ctx, tx.logger, tx.databaseID, "commit", time.Since(started), err)
	if err != nil {
		tx.span.RecordError(errors.New("target transaction commit failed"))
		tx.span.SetStatus(codes.Error, "transaction commit failed")
	}
	tx.span.End()
	return err
}

func (tx *loggingTransaction) Rollback(ctx context.Context) error {
	started := time.Now()
	err := tx.Transaction.Rollback(ctx)
	logTransaction(ctx, tx.logger, tx.databaseID, "rollback", time.Since(started), err)
	if err != nil && !errors.Is(err, context.Canceled) {
		tx.span.RecordError(errors.New("target transaction rollback failed"))
		tx.span.SetStatus(codes.Error, "transaction rollback failed")
	}
	tx.span.End()
	return err
}

type transactionConnection struct {
	Transaction
	engine Engine
}

func (c transactionConnection) Engine() Engine              { return c.engine }
func (c transactionConnection) Close(context.Context) error { return nil }
func (c transactionConnection) Begin(context.Context, ...TxOptions) (Transaction, error) {
	return nil, errors.New("nested target transactions are not supported")
}

type loggingRows struct {
	Rows
	ctx        context.Context
	databaseID string
	logger     *slog.Logger
	query      string
	args       []any
	started    time.Time
	span       trace.Span
	count      int64
	once       sync.Once
}

func (r *loggingRows) Next() bool {
	next := r.Rows.Next()
	if next {
		r.count++
	}
	return next
}

func (r *loggingRows) Close() {
	r.Rows.Close()
	r.finish(r.Rows.Err())
}

func (r *loggingRows) Err() error {
	err := r.Rows.Err()
	if err != nil {
		r.finish(err)
	}
	return err
}

func (r *loggingRows) finish(err error) {
	r.once.Do(func() {
		finishQuery(r.ctx, r.logger, r.databaseID, "query", r.query, r.args, r.started, r.count, err, r.span)
	})
}

type loggingRow struct {
	Row
	ctx        context.Context
	databaseID string
	logger     *slog.Logger
	query      string
	args       []any
	started    time.Time
	span       trace.Span
}

func (r *loggingRow) Scan(dest ...any) error {
	err := r.Row.Scan(dest...)
	rows := int64(1)
	if err != nil {
		rows = 0
	}
	if errors.Is(err, pgx.ErrNoRows) {
		finishQuery(r.ctx, r.logger, r.databaseID, "query_row", r.query, r.args, r.started, rows, nil, r.span)
	} else {
		finishQuery(r.ctx, r.logger, r.databaseID, "query_row", r.query, r.args, r.started, rows, err, r.span)
	}
	return err
}

func startQuery(ctx context.Context, logger *slog.Logger, databaseID, operation, query string, args []any) (context.Context, trace.Span, time.Time) {
	statement := redactStatement(query)
	ctx, span := otel.Tracer("dbmp/targetdb").Start(ctx, "db.query",
		trace.WithAttributes(attribute.String("db.namespace", databaseID), attribute.String("db.operation.name", operation),
			attribute.String("db.query.text", statement)))
	started := time.Now()
	fields := requestFields(ctx, databaseID)
	fields = append(fields, "operation", operation, "query", statement, "parameters", safeParameters(args))
	fields = append(fields, observability.TraceFields(ctx)...)
	logger.InfoContext(ctx, "target SQL started", append([]any{"event", "database_query_started"}, fields...)...)
	return ctx, span, started
}

func finishQuery(ctx context.Context, logger *slog.Logger, databaseID, operation, query string, args []any,
	started time.Time, rows int64, err error, span trace.Span) {
	duration := time.Since(started)
	fields := requestFields(ctx, databaseID)
	fields = append(fields, "operation", operation, "query", redactStatement(query), "parameters", safeParameters(args),
		"duration_ms", duration.Milliseconds(), "rows_affected", rows)
	fields = append(fields, observability.TraceFields(ctx)...)
	if err != nil {
		fields = append(fields, "status", "failed", "error", safeDBError(err), "sqlstate", sqlState(err))
		logger.ErrorContext(ctx, "target SQL failed", append([]any{"event", "database_query_failed"}, fields...)...)
		span.SetAttributes(attribute.String("db.response.sqlstate", sqlState(err)))
		span.RecordError(errors.New("target database query failed"))
		span.SetStatus(codes.Error, "database query failed")
	} else {
		fields = append(fields, "status", "succeeded")
		logger.InfoContext(ctx, "target SQL completed", append([]any{"event", "database_query_completed"}, fields...)...)
	}
	span.End()
}

func logTransaction(ctx context.Context, logger *slog.Logger, databaseID, event string, duration time.Duration, err error) {
	fields := requestFields(ctx, databaseID)
	fields = append(fields, "duration_ms", duration.Milliseconds())
	fields = append(fields, observability.TraceFields(ctx)...)
	if err != nil {
		fields = append(fields, "status", "failed", "error", safeDBError(err), "sqlstate", sqlState(err))
		logger.ErrorContext(ctx, "target transaction "+event+" failed", append([]any{"event", "database_transaction_" + event + "_failed"}, fields...)...)
		return
	}
	fields = append(fields, "status", "succeeded")
	logger.InfoContext(ctx, "target transaction "+event, append([]any{"event", "database_transaction_" + event}, fields...)...)
}

func requestFields(ctx context.Context, databaseID string) []any {
	correlationID := requestmeta.RequestID(ctx)
	activity := ActivityFromContext(ctx)
	if activity.CorrelationID != "" {
		correlationID = activity.CorrelationID
	}
	fields := []any{"database_id", databaseID, "correlation_id", correlationID}
	if principal, ok := auth.PrincipalFromContext(ctx); ok {
		fields = append(fields, "actor", principal.Username, "actor_id", principal.UserID)
	}
	if activity.JobID != "" {
		fields = append(fields, "job_id", activity.JobID)
	}
	if activity.MigrationID != "" {
		fields = append(fields, "migration_id", activity.MigrationID)
	}
	if activity.ExecutionID != "" {
		fields = append(fields, "execution_id", activity.ExecutionID)
	}
	if activity.Action != "" {
		fields = append(fields, "action", activity.Action)
	}
	if activity.Attempt > 0 {
		fields = append(fields, "attempt", activity.Attempt)
	}
	if activity.BatchNumber > 0 {
		fields = append(fields, "batch_number", activity.BatchNumber)
	}
	return fields
}

var (
	quotedLiteralPattern = regexp.MustCompile(`'(?:''|[^'])*'`)
	credentialSQLPattern = regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|session)\s*=\s*[^\s,;]+`)
	postgresURLPattern   = regexp.MustCompile(`(?i)(postgres(?:ql)?://[^:/@\s]+:)[^@\s]+@`)
)

func redactStatement(query string) string {
	redacted := quotedLiteralPattern.ReplaceAllString(query, "'[redacted]'")
	redacted = credentialSQLPattern.ReplaceAllString(redacted, "$1=[redacted]")
	return postgresURLPattern.ReplaceAllString(redacted, "$1[redacted]@")
}

func safeParameters(args []any) []any {
	values := make([]any, len(args))
	for index, value := range args {
		switch value.(type) {
		case nil, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			values[index] = value
		default:
			values[index] = "[redacted]"
		}
	}
	return values
}

func safeDBError(err error) string {
	if err == nil {
		return ""
	}
	if state := sqlState(err); state != "" {
		return "PostgreSQL operation failed (SQLSTATE " + state + ")"
	}
	return "target database operation failed"
}

func sqlState(err error) string {
	type sqlStater interface{ SQLState() string }
	var state sqlStater
	if errors.As(err, &state) {
		return state.SQLState()
	}
	return ""
}
