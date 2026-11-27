package platformdb

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Queries interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type transactionReadKey struct{}

// WithTransactionReads lets a synchronous policy check reuse its caller's
// platform transaction and the locks held by that transaction.
func WithTransactionReads(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, transactionReadKey{}, tx)
}

func TransactionReads(ctx context.Context, fallback Queries) Queries {
	if tx, ok := ctx.Value(transactionReadKey{}).(pgx.Tx); ok {
		return tx
	}
	return fallback
}
