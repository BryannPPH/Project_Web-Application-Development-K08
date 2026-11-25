// Package metrics mengekspos metrik Prometheus platform di GET /metrics.
//
// Semua nilai dihitung saat di-scrape: statistik pool koneksi dari pgxpool dan
// ringkasan migration, queue, dan batch dari execution history. Label sengaja
// bermultiplisitas rendah; user ID, migration ID, dan SQL tidak pernah dipakai.
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
	jobs              *prometheus.Desc
	jobRetries        *prometheus.Desc
	queueWait         *prometheus.Desc
	lockWait          *prometheus.Desc
	batchesProcessed  *prometheus.Desc
	rowsProcessed     *prometheus.Desc
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
		jobs:              desc("jobs", "Jumlah migration job per status.", "status"),
		jobRetries:        desc("job_retries_total", "Jumlah retry worker yang tercatat pada migration job.", "kind"),
		queueWait:         desc("queue_wait_seconds", "Waktu dari job dibuat sampai pertama kali diambil worker.", "kind"),
		lockWait:          desc("lock_wait_seconds", "Waktu attempt menunggu advisory lock database.", "kind"),
		batchesProcessed:  desc("batches_processed_total", "Jumlah batch migration per outcome.", "outcome"),
		rowsProcessed:     desc("rows_processed_total", "Jumlah baris yang diproses batch per outcome.", "outcome"),
		scrapeUp:          desc("metrics_database_up", "1 jika metrik platform berhasil dibaca dari database, 0 jika gagal."),
	}
}

// Describe mengirim semua deskriptor metrik.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{c.poolConns, c.poolMaxConns, c.poolAcquires, c.poolEmptyAcquires,
		c.poolCanceled, c.poolAcquireTime, c.poolWaitTime, c.migrations, c.migrationDuration, c.jobs,
		c.jobRetries, c.queueWait, c.lockWait, c.batchesProcessed, c.rowsProcessed, c.scrapeUp} {
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
	if err := c.collectQueue(ctx, ch); err != nil {
		c.logger.Warn("collect migration queue metrics", "error", err)
		ch <- prometheus.MustNewConstMetric(c.scrapeUp, prometheus.GaugeValue, 0)
		return
	}
	if err := c.collectBatchHistory(ctx, ch); err != nil {
		c.logger.Warn("collect migration batch metrics", "error", err)
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

// finishedStatuses: status yang punya started_at dan finished_at (histogram durasi).
var finishedStatuses = []string{"succeeded", "failed", "interrupted", "needs_reconciliation", "reconciled"}

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
	seen := make(map[string]bool, len(finishedStatuses))
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
		seen[status] = true
	}
	if err := durations.Err(); err != nil {
		return err
	}
	// Status selesai tanpa baris tetap dikirim bernilai 0, supaya kejadian pertama
	// terbaca sebagai kenaikan 0 -> 1 oleh increase()/rate().
	empty := make(map[float64]uint64, len(durationBuckets))
	for _, upper := range durationBuckets {
		empty[upper] = 0
	}
	for _, status := range finishedStatuses {
		if !seen[status] {
			ch <- prometheus.MustNewConstHistogram(c.migrationDuration, 0, 0, empty, status)
		}
	}
	return nil
}

var jobStatuses = []string{"queued", "running", "retrying", "succeeded", "failed", "partial", "partially_reverted", "needs_reconciliation"}
var jobKinds = []string{"apply", "retry", "rollback", "forward", "batch"}
var batchOutcomes = []string{"committed", "failed", "reverted"}

type histogramSnapshot struct {
	count   uint64
	sum     float64
	buckets []uint64
}

func newHistogramSnapshot() histogramSnapshot {
	return histogramSnapshot{buckets: make([]uint64, len(durationBuckets))}
}

// collectQueue derives durable queue metrics from job rows and execution attempts.
// IDs and SQL never become metric labels.
func (c *Collector) collectQueue(ctx context.Context, ch chan<- prometheus.Metric) error {
	counts := make(map[string]float64, len(jobStatuses))
	retries := make(map[string]float64, len(jobKinds))
	queueHistograms := make(map[string]histogramSnapshot, len(jobKinds))
	lockHistograms := make(map[string]histogramSnapshot, len(jobKinds))
	for _, status := range jobStatuses {
		counts[status] = 0
	}
	for _, kind := range jobKinds {
		retries[kind] = 0
		queueHistograms[kind] = newHistogramSnapshot()
		lockHistograms[kind] = newHistogramSnapshot()
	}

	var queueQuery strings.Builder
	queueQuery.WriteString(`SELECT status,
		CASE WHEN execution_mode='batch' THEN 'batch' ELSE action END AS kind,
		count(*)::bigint, COALESCE(sum(retry_count),0)::float8,
		count(*) FILTER (WHERE started_at IS NOT NULL)::bigint,
		COALESCE(sum(extract(epoch FROM started_at-created_at)) FILTER (WHERE started_at IS NOT NULL),0)::float8`)
	for _, upper := range durationBuckets {
		fmt.Fprintf(&queueQuery, `, count(*) FILTER (WHERE started_at IS NOT NULL AND extract(epoch FROM started_at-created_at) <= %g)::bigint`, upper)
	}
	queueQuery.WriteString(` FROM migration_jobs GROUP BY status, kind`)
	rows, err := c.pool.Query(ctx, queueQuery.String())
	if err != nil {
		return fmt.Errorf("load queue metrics: %w", err)
	}
	for rows.Next() {
		var status, kind string
		var count, queueCount int64
		var retryCount, queueSum float64
		buckets := make([]uint64, len(durationBuckets))
		dest := []any{&status, &kind, &count, &retryCount, &queueCount, &queueSum}
		for i := range buckets {
			dest = append(dest, &buckets[i])
		}
		if err := rows.Scan(dest...); err != nil {
			rows.Close()
			return fmt.Errorf("scan queue metrics: %w", err)
		}
		counts[status] += float64(count)
		retries[kind] += retryCount
		if histogram, ok := queueHistograms[kind]; ok {
			histogram.count += uint64(queueCount)
			histogram.sum += queueSum
			for i := range buckets {
				histogram.buckets[i] += buckets[i]
			}
			queueHistograms[kind] = histogram
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate queue metrics: %w", err)
	}
	for _, status := range jobStatuses {
		ch <- prometheus.MustNewConstMetric(c.jobs, prometheus.GaugeValue, counts[status], status)
	}
	for _, kind := range jobKinds {
		ch <- prometheus.MustNewConstMetric(c.jobRetries, prometheus.CounterValue, retries[kind], kind)
		histogram := queueHistograms[kind]
		ch <- prometheus.MustNewConstHistogram(c.queueWait, histogram.count, histogram.sum,
			toBucketMap(histogram.buckets), kind)
	}

	var lockQuery strings.Builder
	lockQuery.WriteString(`SELECT CASE WHEN m.execution_mode='batch' THEN 'batch' ELSE a.action END AS kind,
		count(*)::bigint, COALESCE(sum(a.lock_wait_micros)::float8 / 1000000,0)::float8`)
	for _, upper := range durationBuckets {
		fmt.Fprintf(&lockQuery, `, count(*) FILTER (WHERE a.lock_wait_micros <= %g)::bigint`, upper*1000000)
	}
	lockQuery.WriteString(` FROM migration_execution_attempts a
		JOIN migration_entries m ON m.id=a.migration_id GROUP BY kind`)
	lockRows, err := c.pool.Query(ctx, lockQuery.String())
	if err != nil {
		return fmt.Errorf("load lock wait metrics: %w", err)
	}
	for lockRows.Next() {
		var kind string
		var count int64
		var sum float64
		buckets := make([]uint64, len(durationBuckets))
		dest := []any{&kind, &count, &sum}
		for i := range buckets {
			dest = append(dest, &buckets[i])
		}
		if err := lockRows.Scan(dest...); err != nil {
			lockRows.Close()
			return fmt.Errorf("scan lock wait metrics: %w", err)
		}
		if histogram, ok := lockHistograms[kind]; ok {
			histogram.count += uint64(count)
			histogram.sum += sum
			for i := range buckets {
				histogram.buckets[i] += buckets[i]
			}
			lockHistograms[kind] = histogram
		}
	}
	lockRows.Close()
	if err := lockRows.Err(); err != nil {
		return fmt.Errorf("iterate lock wait metrics: %w", err)
	}
	for _, kind := range jobKinds {
		histogram := lockHistograms[kind]
		ch <- prometheus.MustNewConstHistogram(c.lockWait, histogram.count, histogram.sum,
			toBucketMap(histogram.buckets), kind)
	}
	return nil
}

func (c *Collector) collectBatchHistory(ctx context.Context, ch chan<- prometheus.Metric) error {
	counts := map[string]float64{"committed": 0, "failed": 0, "reverted": 0}
	rowsProcessed := map[string]float64{"committed": 0, "failed": 0, "reverted": 0}
	rows, err := c.pool.Query(ctx, `SELECT event,count(*)::bigint,COALESCE(sum(rows_affected),0)::float8
		FROM migration_execution_events
		WHERE event IN ('batch_committed','batch_failed','batch_reverted') GROUP BY event`)
	if err != nil {
		return fmt.Errorf("load batch history metrics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var event string
		var count int64
		var rowCount float64
		if err := rows.Scan(&event, &count, &rowCount); err != nil {
			return fmt.Errorf("scan batch history metrics: %w", err)
		}
		outcome := map[string]string{"batch_committed": "committed", "batch_failed": "failed", "batch_reverted": "reverted"}[event]
		counts[outcome] = float64(count)
		rowsProcessed[outcome] = rowCount
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate batch history metrics: %w", err)
	}
	for _, outcome := range batchOutcomes {
		ch <- prometheus.MustNewConstMetric(c.batchesProcessed, prometheus.CounterValue, counts[outcome], outcome)
		ch <- prometheus.MustNewConstMetric(c.rowsProcessed, prometheus.CounterValue, rowsProcessed[outcome], outcome)
	}
	return nil
}

func toBucketMap(values []uint64) map[float64]uint64 {
	buckets := make(map[float64]uint64, len(durationBuckets))
	for i, upper := range durationBuckets {
		buckets[upper] = values[i]
	}
	return buckets
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
