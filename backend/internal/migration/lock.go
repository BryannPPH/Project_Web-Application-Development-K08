package migration

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PGAdvisoryLocker struct {
	pool *pgxpool.Pool
}

func NewPGAdvisoryLocker(pool *pgxpool.Pool) *PGAdvisoryLocker {
	return &PGAdvisoryLocker{pool: pool}
}

type pgExecutionLock struct {
	connection *pgxpool.Conn
	key1       int32
	key2       int32
}

func (l *PGAdvisoryLocker) Acquire(ctx context.Context, databaseID string) (ExecutionLock, error) {
	connection, err := l.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire platform connection: %w", err)
	}
	key1, key2 := advisoryKeys(databaseID)
	if _, err := connection.Exec(ctx, `SELECT pg_advisory_lock($1, $2)`, key1, key2); err != nil {
		connection.Release()
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	return &pgExecutionLock{connection: connection, key1: key1, key2: key2}, nil
}

func (l *pgExecutionLock) Release(ctx context.Context) error {
	if l.connection == nil {
		return nil
	}
	connection := l.connection
	l.connection = nil
	if _, err := connection.Exec(ctx, `SELECT pg_advisory_unlock($1, $2)`, l.key1, l.key2); err != nil {
		raw := connection.Hijack()
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return errors.Join(fmt.Errorf("release migration lock: %w", err), raw.Close(closeCtx))
	}
	connection.Release()
	return nil
}

func advisoryKeys(databaseID string) (int32, int32) {
	sum := sha256.Sum256([]byte("migration-execution:" + strings.ToLower(databaseID)))
	return int32(binary.BigEndian.Uint32(sum[0:4])), int32(binary.BigEndian.Uint32(sum[4:8]))
}
