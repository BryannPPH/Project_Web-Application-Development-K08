// Package metrics mengekspos metrik Prometheus platform di GET /metrics.
//
// Semua nilai dihitung saat di-scrape: statistik pool koneksi dari pgxpool dan
// ringkasan migration dari tabel migration_entries. Label sengaja bermultiplisitas
// rendah (status, state); user ID, migration ID, dan SQL tidak pernah dipakai.
package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "dbmp"

// durationBuckets dalam detik, dari migration kecil sampai batas timeout eksekusi.
var durationBuckets = []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}

// Collector membaca metrik platform setiap kali Prometheus melakukan scrape.
type Collector struct {
	pool    *pgxpool.Pool
	logger  *slog.Logger
	timeout time.Duration

	poolConns         *prometheus.Desc
	poolMaxConns      *prometheus.Desc
	poolAcquires      *prometheus.Desc
	poolEmptyAcquires *prometheus.Desc
	poolCanceled      *prometheus.Desc
	poolAcquireTime   *prometheus.Desc
	poolWaitTime      *prometheus.Desc
	migrations        *prometheus.Desc
	migrationDuration *prometheus.Desc
	scrapeUp          *prometheus.Desc
}

// NewCollector membuat collector untuk pool platform DB.
func NewCollector(pool *pgxpool.Pool, logger *slog.Logger) *Collector {
	if logger == nil {
		logger = slog.Default()
	}
	desc := func(name, help string, labels ...string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(namespace, "", name), help, labels, nil)
	}
	return &Collector{
		pool:    pool,
		logger:  logger,
		timeout: 3 * time.Second,

		poolConns:         desc("db_pool_connections", "Koneksi platform DB saat ini menurut state (acquired = sedang dipakai, idle = menganggur, constructing = sedang dibuat).", "state"),
		poolMaxConns:      desc("db_pool_max_connections", "Batas maksimum koneksi pool platform DB."),
		poolAcquires:      desc("db_pool_acquires_total", "Jumlah pengambilan koneksi dari pool."),
		poolEmptyAcquires: desc("db_pool_empty_acquires_total", "Jumlah pengambilan koneksi yang harus menunggu karena pool kosong."),
		poolCanceled:      desc("db_pool_canceled_acquires_total", "Jumlah pengambilan koneksi yang dibatalkan sebelum berhasil."),
		poolAcquireTime:   desc("db_pool_acquire_seconds_total", "Total waktu yang dihabiskan untuk mengambil koneksi dari pool."),
		poolWaitTime:      desc("db_pool_wait_seconds_total", "Total waktu menunggu koneksi saat pool kosong."),
		migrations:        desc("migrations", "Jumlah migration per status.", "status"),
		migrationDuration: desc("migration_duration_seconds", "Durasi eksekusi migration yang sudah selesai.", "status"),
		scrapeUp:          desc("metrics_database_up", "1 jika ringkasan migration berhasil dibaca dari platform DB, 0 jika gagal."),
	}
}

// Describe mengirim semua deskriptor metrik.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{c.poolConns, c.poolMaxConns, c.poolAcquires, c.poolEmptyAcquires,
		c.poolCanceled, c.poolAcquireTime, c.poolWaitTime, c.migrations, c.migrationDuration, c.scrapeUp} {
		ch <- d
	}
}

// Collect dipanggil pada setiap scrape.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.collectPool(ch)

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	if err := c.collectMigrations(ctx, ch); err != nil {
		c.logger.Warn("collect migration metrics", "error", err)
		ch <- prometheus.MustNewConstMetric(c.scrapeUp, prometheus.GaugeValue, 0)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.scrapeUp, prometheus.GaugeValue, 1)
}

func (c *Collector) collectPool(ch chan<- prometheus.Metric) {
	stat := c.pool.Stat()
	ch <- prometheus.MustNewConstMetric(c.poolConns, prometheus.GaugeValue, float64(stat.AcquiredConns()), "acquired")
	ch <- prometheus.MustNewConstMetric(c.poolConns, prometheus.GaugeValue, float64(stat.IdleConns()), "idle")
	ch <- prometheus.MustNewConstMetric(c.poolConns, prometheus.GaugeValue, float64(stat.ConstructingConns()), "constructing")
	ch <- prometheus.MustNewConstMetric(c.poolMaxConns, prometheus.GaugeValue, float64(stat.MaxConns()))
	ch <- prometheus.MustNewConstMetric(c.poolAcquires, prometheus.CounterValue, float64(stat.AcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.poolEmptyAcquires, prometheus.CounterValue, float64(stat.EmptyAcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.poolCanceled, prometheus.CounterValue, float64(stat.CanceledAcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.poolAcquireTime, prometheus.CounterValue, stat.AcquireDuration().Seconds())
	ch <- prometheus.MustNewConstMetric(c.poolWaitTime, prometheus.CounterValue, stat.EmptyAcquireWaitTime().Seconds())
}

// migrationStatuses: semua status di CHECK constraint, supaya status dengan 0 baris tetap muncul.
var migrationStatuses = []string{"draft", "running", "succeeded", "failed", "interrupted", "needs_reconciliation", "reconciled"}

func (c *Collector) collectMigrations(ctx context.Context, ch chan<- prometheus.Metric) error {
	counts := make(map[string]float64, len(migrationStatuses))
	for _, status := range migrationStatuses {
		counts[status] = 0
	}
	rows, err := c.pool.Query(ctx, `SELECT status, count(*) FROM migration_entries GROUP BY status`)
	if err != nil {
		return fmt.Errorf("count migrations: %w", err)
	}
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			rows.Close()
			return fmt.Errorf("scan migration count: %w", err)
		}
		counts[status] = float64(count)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate migration counts: %w", err)
	}
	for _, status := range migrationStatuses {
		ch <- prometheus.MustNewConstMetric(c.migrations, prometheus.GaugeValue, counts[status], status)
	}

	// Histogram kumulatif dari semua migration yang selesai. Baris migration tidak
	// pernah dihapus, sehingga nilai bucket hanya bertambah (aman untuk rate()).
	durations, err := c.pool.Query(ctx, durationQuery())
	if err != nil {
		return fmt.Errorf("load migration durations: %w", err)
	}
	defer durations.Close()
	for durations.Next() {
		var status string
		var count uint64
		var sum float64
		bucketCounts := make([]uint64, len(durationBuckets))
		dest := []any{&status, &count, &sum}
		for i := range bucketCounts {
			dest = append(dest, &bucketCounts[i])
		}
		if err := durations.Scan(dest...); err != nil {
			return fmt.Errorf("scan migration durations: %w", err)
		}
		buckets := make(map[float64]uint64, len(durationBuckets))
		for i, upper := range durationBuckets {
			buckets[upper] = bucketCounts[i]
		}
		ch <- prometheus.MustNewConstHistogram(c.migrationDuration, count, sum, buckets, status)
	}
	return durations.Err()
}

// durationQuery menghitung count, sum, dan isi tiap bucket per status di SQL.
// Batas bucket adalah konstanta di kode, bukan input user.
func durationQuery() string {
	var b strings.Builder
	b.WriteString(`SELECT status, count(*)::bigint, coalesce(sum(d), 0)::float8`)
	for _, upper := range durationBuckets {
		fmt.Fprintf(&b, `, count(*) FILTER (WHERE d <= %g)::bigint`, upper)
	}
	b.WriteString(` FROM (SELECT status, extract(epoch FROM finished_at - started_at)::float8 AS d
		FROM migration_entries
		WHERE started_at IS NOT NULL AND finished_at IS NOT NULL) finished
		GROUP BY status`)
	return b.String()
}
