package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/metrics"
)

func TestMetricsAgainstPlatformDatabase(t *testing.T) {
	databaseURL := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_PLATFORM_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect platform database: %v", err)
	}
	t.Cleanup(pool.Close)

	registry := prometheus.NewRegistry()
	registry.MustRegister(metrics.NewCollector(pool, nil))
	recorder := httptest.NewRecorder()
	metrics.Handler(registry, "").ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()

	for _, want := range []string{
		`dbmp_db_pool_connections{state="idle"}`,
		`dbmp_db_pool_max_connections`,
		`dbmp_db_pool_wait_seconds_total`,
		`dbmp_migrations{status="draft"}`,
		`dbmp_migration_duration_seconds_bucket`,
		`dbmp_metrics_database_up 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output is missing %q", want)
		}
	}
	for _, forbidden := range []string{"user_id", "migration_id", "sql"} {
		if strings.Contains(body, forbidden+"=") {
			t.Errorf("metrics must not use high-cardinality label %q", forbidden)
		}
	}
}

// Status selesai tanpa data tetap muncul bernilai 0, supaya kegagalan pertama
// terhitung oleh increase() sebagai kenaikan 0 -> 1.
func TestMetricsExposeEveryFinishedStatus(t *testing.T) {
	databaseURL := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_PLATFORM_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect platform database: %v", err)
	}
	t.Cleanup(pool.Close)

	registry := prometheus.NewRegistry()
	registry.MustRegister(metrics.NewCollector(pool, nil))
	recorder := httptest.NewRecorder()
	metrics.Handler(registry, "").ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, status := range []string{"succeeded", "failed", "interrupted", "needs_reconciliation", "reconciled"} {
		if !strings.Contains(recorder.Body.String(), `dbmp_migration_duration_seconds_count{status="`+status+`"}`) {
			t.Errorf("duration histogram is missing status %q", status)
		}
	}
}
