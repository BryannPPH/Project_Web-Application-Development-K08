# Bonus Observability: metrik Prometheus dan dashboard Grafana

## Menjalankan

```bash
docker compose --profile observability up -d
```

| Layanan | URL | Keterangan |
|---|---|---|
| Endpoint metrik backend | http://localhost:8080/metrics | format teks Prometheus |
| Prometheus | http://localhost:9090 | scrape `backend:8080/metrics` setiap 15 detik |
| Grafana | http://localhost:3000 | login `admin` / `admin` (ubah lewat `GRAFANA_ADMIN_PASSWORD`) |

Dashboard **DB Manager Overview** dimuat otomatis lewat provisioning:
- data source: [`observability/grafana/provisioning/datasources/prometheus.yml`](grafana/provisioning/datasources/prometheus.yml)
- dashboard: [`observability/grafana/dashboards/dbmp-overview.json`](grafana/dashboards/dbmp-overview.json)
- konfigurasi Prometheus: [`observability/prometheus/prometheus.yml`](prometheus/prometheus.yml)

Tanpa `--profile observability`, Prometheus dan Grafana tidak dijalankan, jadi `docker compose up` biasa tidak berubah.

### Mengamankan /metrics
Isi `METRICS_TOKEN` di `.env` untuk mewajibkan header `Authorization: Bearer <token>`. Lalu aktifkan blok `authorization` di `observability/prometheus/prometheus.yml` dan mount file token-nya. Di production, jangan publikasikan port 8080 langsung ke internet; biarkan `/metrics` hanya dapat dijangkau dari jaringan internal Prometheus.

## Metrik yang tersedia

Kode: [`backend/internal/metrics/`](../backend/internal/metrics/). Semua nilai dihitung saat scrape.

| Metrik | Tipe | Label | Kebutuhan tugas |
|---|---|---|---|
| `dbmp_db_pool_connections` | gauge | `state` = acquired / idle / constructing | koneksi aktif / idle |
| `dbmp_db_pool_max_connections` | gauge | - | kapasitas pool |
| `dbmp_db_pool_wait_seconds_total` | counter | - | waktu tunggu connection pool |
| `dbmp_db_pool_acquire_seconds_total` | counter | - | total waktu acquire koneksi |
| `dbmp_db_pool_acquires_total`, `dbmp_db_pool_empty_acquires_total`, `dbmp_db_pool_canceled_acquires_total` | counter | - | tekanan pada pool |
| `dbmp_migrations` | gauge | `status` | jumlah migration per status |
| `dbmp_migration_duration_seconds` | histogram | `status` | durasi migration |
| `dbmp_metrics_database_up` | gauge | - | 1 jika data migration berhasil dibaca |
| `go_*`, `process_*` | bawaan | - | kesehatan proses backend |

### Kebijakan label
Label hanya bernilai dari himpunan kecil dan tetap (`status`, `state`). **User ID, job ID, migration ID, dan SQL tidak pernah dijadikan label.** Ini dicek di integration test `TestMetricsAgainstPlatformDatabase`.

### Histogram durasi dari database
`dbmp_migration_duration_seconds` dihitung dari `started_at` / `finished_at` di `migration_entries`. Baris migration tidak pernah dihapus, sehingga nilai bucket hanya bertambah dan aman dipakai dengan `rate()` / `histogram_quantile()`.

## Menunggu Person C (queue & worker)

Metrik berikut membutuhkan queue dan worker. Nama dan labelnya disepakati di sini agar dashboard tinggal ditambah panel:

| Metrik | Tipe | Label | Dicatat di |
|---|---|---|---|
| `dbmp_jobs` | gauge | `status` (queued / running / succeeded / failed / retrying) | queue |
| `dbmp_job_retries_total` | counter | `kind` (apply / rollback / forward / batch) | worker |
| `dbmp_queue_wait_seconds` | histogram | `kind` | worker (dari enqueue sampai diambil worker) |
| `dbmp_lock_wait_seconds` | histogram | - | worker (menunggu lock database) |
| `dbmp_batches_processed_total` | counter | `outcome` (committed / failed / reverted) | worker batch |
| `dbmp_rows_processed_total` | counter | `outcome` | worker batch |

Setelah tersedia, `dbmp_jobs` menggantikan `dbmp_migrations` sebagai panel "jumlah job per status".

## Bukti

Lampirkan screenshot dashboard (mis. `observability/grafana-dashboard.png`) dan hasil `curl http://localhost:8080/metrics`.
