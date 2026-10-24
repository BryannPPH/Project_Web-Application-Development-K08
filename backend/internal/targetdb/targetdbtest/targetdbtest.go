// Package targetdbtest menyediakan fake Provider/Connection untuk unit test
// consumer targetdb (viewer, migration) tanpa PostgreSQL sungguhan.
//
// Contoh:
//
//	connection := &targetdbtest.Connection{}
//	provider := &targetdbtest.Provider{Connection: connection}
//	// ... jalankan service ...
//	if provider.OpenCount() != 0 { t.Fatal("authorization harus mendahului Open") }
//	if !connection.Closed() { t.Fatal("koneksi harus ditutup") }
package targetdbtest

import (
	"context"
	"errors"
	"sync"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

// ErrNotConfigured dikembalikan method fake yang belum diberi fungsi.
var ErrNotConfigured = errors.New("targetdbtest: behaviour not configured")

// Provider adalah fake targetdb.Provider.
type Provider struct {
	mu sync.Mutex
	// Connection dikembalikan setiap Open bila OpenErr kosong.
	Connection *Connection
	// OpenErr mensimulasikan kegagalan Open (mis. targetdb.ErrTargetUnavailable).
	OpenErr error
	opened  []string
}

var _ targetdb.Provider = (*Provider)(nil)

func (p *Provider) Open(_ context.Context, databaseID string) (targetdb.Connection, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.opened = append(p.opened, databaseID)
	if p.OpenErr != nil {
		return nil, p.OpenErr
	}
	if p.Connection == nil {
		p.Connection = &Connection{}
	}
	return p.Connection, nil
}

// OpenCount mengembalikan jumlah pemanggilan Open.
func (p *Provider) OpenCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.opened)
}

// Connection adalah fake targetdb.Connection yang mencatat Close.
type Connection struct {
	mu          sync.Mutex
	EngineValue targetdb.Engine
	QueryFunc   func(ctx context.Context, sql string, args ...any) (targetdb.Rows, error)
	RowFunc     func(ctx context.Context, sql string, args ...any) targetdb.Row
	ExecFunc    func(ctx context.Context, sql string, args ...any) (targetdb.CommandResult, error)
	BeginFunc   func(ctx context.Context, options ...targetdb.TxOptions) (targetdb.Transaction, error)
	closes      int
}

var _ targetdb.Connection = (*Connection)(nil)

func (c *Connection) Engine() targetdb.Engine {
	return targetdb.NormalizeEngine(c.EngineValue)
}

func (c *Connection) Query(ctx context.Context, sql string, args ...any) (targetdb.Rows, error) {
	if c.QueryFunc == nil {
		return nil, ErrNotConfigured
	}
	return c.QueryFunc(ctx, sql, args...)
}

func (c *Connection) QueryRow(ctx context.Context, sql string, args ...any) targetdb.Row {
	if c.RowFunc == nil {
		return errorRow{err: ErrNotConfigured}
	}
	return c.RowFunc(ctx, sql, args...)
}

func (c *Connection) Exec(ctx context.Context, sql string, args ...any) (targetdb.CommandResult, error) {
	if c.ExecFunc == nil {
		return nil, ErrNotConfigured
	}
	return c.ExecFunc(ctx, sql, args...)
}

func (c *Connection) Begin(ctx context.Context, options ...targetdb.TxOptions) (targetdb.Transaction, error) {
	if c.BeginFunc == nil {
		return nil, ErrNotConfigured
	}
	return c.BeginFunc(ctx, options...)
}

func (c *Connection) Close(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closes++
	return nil
}

// Closed bernilai true bila Close sudah dipanggil minimal sekali.
func (c *Connection) Closed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closes > 0
}

type errorRow struct{ err error }

func (r errorRow) Scan(...any) error { return r.err }
