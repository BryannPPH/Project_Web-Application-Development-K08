package targetdb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func validConfig() ConnectionConfig {
	return ConnectionConfig{
		DatabaseID: "11111111-1111-4111-8111-111111111111",
		Host:       "localhost",
		Port:       5432,
		Database:   "target_db",
		Username:   "target_owner",
		Password:   "s3cr3t-password",
		SSLMode:    "disable",
	}
}

func TestValidateRejectsUnsafeFields(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*ConnectionConfig){
		"empty host":        func(c *ConnectionConfig) { c.Host = "" },
		"host with space":   func(c *ConnectionConfig) { c.Host = "localhost sslmode=disable" },
		"host with slash":   func(c *ConnectionConfig) { c.Host = "/tmp/socket" },
		"host with quote":   func(c *ConnectionConfig) { c.Host = "local'host" },
		"port zero":         func(c *ConnectionConfig) { c.Port = 0 },
		"port too large":    func(c *ConnectionConfig) { c.Port = 70000 },
		"empty database":    func(c *ConnectionConfig) { c.Database = "" },
		"database too long": func(c *ConnectionConfig) { c.Database = strings.Repeat("a", 64) },
		"control username":  func(c *ConnectionConfig) { c.Username = "user\nname" },
		"empty password":    func(c *ConnectionConfig) { c.Password = "" },
		"nul password":      func(c *ConnectionConfig) { c.Password = "pa\x00ss" },
		"unknown ssl mode":  func(c *ConnectionConfig) { c.SSLMode = "maybe" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := validConfig()
			mutate(&config)
			if err := config.Validate(); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("Validate() error = %v, want ErrInvalidConfig", err)
			}
		})
	}
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}
	ipv6 := validConfig()
	ipv6.Host = "::1"
	if err := ipv6.Validate(); err != nil {
		t.Fatalf("Validate(ipv6) error = %v", err)
	}
}

func TestPGXConfigQuotesEveryValue(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config.Database = `db' host=evil.example`
	config.Username = `user\name`
	config.Password = `pa'ss word\ sslmode=disable`
	config.SSLMode = "require"

	parsed, err := config.pgxConfig(3 * time.Second)
	if err != nil {
		t.Fatalf("pgxConfig() error = %v", err)
	}
	if parsed.Host != "localhost" || parsed.Port != 5432 {
		t.Fatalf("host/port = %s:%d, injection changed connection target", parsed.Host, parsed.Port)
	}
	if parsed.Database != config.Database || parsed.User != config.Username || parsed.Password != config.Password {
		t.Fatalf("parsed = db %q user %q, want values preserved literally", parsed.Database, parsed.User)
	}
	if parsed.TLSConfig == nil {
		t.Fatal("sslmode=require must enable TLS")
	}
	if parsed.ConnectTimeout != 3*time.Second {
		t.Fatalf("ConnectTimeout = %v, want 3s", parsed.ConnectTimeout)
	}
}

func TestConnectionConfigStringHidesCredential(t *testing.T) {
	t.Parallel()

	config := validConfig()
	for _, formatted := range []string{fmt.Sprint(config), fmt.Sprintf("%v", config), fmt.Sprintf("%+v", config), fmt.Sprintf("%#v", config)} {
		if strings.Contains(formatted, config.Password) || strings.Contains(formatted, config.Username) {
			t.Fatalf("formatted config leaked credential: %s", formatted)
		}
	}
}

func TestClassifyConnectError(t *testing.T) {
	t.Parallel()

	cases := map[FailureReason]error{
		ReasonAuthentication:   &pgconn.PgError{Code: "28P01", Message: "password authentication failed"},
		ReasonDatabaseNotFound: &pgconn.PgError{Code: "3D000"},
		ReasonTooManyClients:   &pgconn.PgError{Code: "53300"},
		ReasonTimeout:          context.DeadlineExceeded,
		ReasonNetwork:          &net.OpError{Op: "dial", Err: errors.New("connection refused")},
		ReasonInvalidConfig:    ErrInvalidConfig,
		ReasonTLS:              errors.New("tls: handshake failure"),
		ReasonUnknown:          errors.New("something odd"),
	}
	for want, err := range cases {
		if got := ClassifyConnectError(err); got != want {
			t.Errorf("ClassifyConnectError(%v) = %q, want %q", err, got, want)
		}
	}
}

func TestUnavailableErrorIsRedacted(t *testing.T) {
	t.Parallel()

	cause := errors.New("failed to connect to `user=secret_user database=secret_db`: password=hunter2")
	err := newUnavailableError("db-1", cause)
	if !errors.Is(err, ErrTargetUnavailable) {
		t.Fatal("UnavailableError must unwrap to ErrTargetUnavailable")
	}
	for _, secret := range []string{"secret_user", "secret_db", "hunter2"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error message leaked %q: %s", secret, err.Error())
		}
	}
	described, ok := Describe(err)
	if !ok || described.Code != "TARGET_UNAVAILABLE" {
		t.Fatalf("Describe() = %+v, %v", described, ok)
	}
}

type stubResolver struct {
	config ConnectionConfig
	err    error
}

func (r stubResolver) ResolveConnection(context.Context, string) (ConnectionConfig, error) {
	return r.config, r.err
}

func TestProviderPropagatesResolverErrors(t *testing.T) {
	t.Parallel()

	for _, want := range []error{ErrDatabaseNotFound, ErrDatabaseInactive, ErrCredentialUnavailable} {
		provider := NewPGProvider(stubResolver{err: want}, time.Second, nil)
		if _, err := provider.Open(context.Background(), "db"); !errors.Is(err, want) {
			t.Errorf("Open() error = %v, want %v", err, want)
		}
	}
}

func TestProviderLogsOnlySafeFailureDetails(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	config := validConfig()
	provider := NewPGProvider(stubResolver{config: config}, time.Second, slog.New(slog.NewJSONHandler(&logs, nil)))
	provider.connect = func(context.Context, ConnectionConfig, time.Duration) (Connection, error) {
		return nil, newUnavailableError(config.DatabaseID, &pgconn.PgError{Code: "28P01", Message: "auth failed for target_owner"})
	}

	_, err := provider.Open(context.Background(), config.DatabaseID)
	if !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("Open() error = %v, want ErrTargetUnavailable", err)
	}
	output := logs.String()
	if !strings.Contains(output, string(ReasonAuthentication)) || !strings.Contains(output, config.DatabaseID) {
		t.Fatalf("log missing safe context: %s", output)
	}
	for _, secret := range []string{config.Password, config.Username, config.Host + ":"} {
		if strings.Contains(output, secret) {
			t.Fatalf("log leaked %q: %s", secret, output)
		}
	}
}

func TestConnectRejectsInvalidConfigWithoutDialing(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config.Host = "bad host"
	_, err := Connect(context.Background(), config, time.Second)
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) || unavailable.Reason != ReasonInvalidConfig {
		t.Fatalf("Connect() error = %v, want invalid configuration", err)
	}
}

func TestPingUnreachableTargetFailsFast(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	config := validConfig()
	config.Host = "127.0.0.1"
	config.Port = port
	startedAt := time.Now()
	err = Ping(context.Background(), config, 2*time.Second)
	if !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("Ping() error = %v, want ErrTargetUnavailable", err)
	}
	if time.Since(startedAt) > 5*time.Second {
		t.Fatal("Ping() did not respect timeout")
	}
}
