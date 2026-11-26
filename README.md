# Database Management Platform

Database Management Platform (DBMP) adalah aplikasi web untuk mengelola akses dan
perubahan pada database PostgreSQL maupun MySQL dari satu antarmuka. Aplikasi ini
dibuat untuk Milestone 1 IF3110 Pengembangan Aplikasi Web, dengan fokus pada
kontrol akses berbasis group, database viewer read-only, dan migration yang
tercatat.

## Preview Aplikasi

Cuplikan tampilan aplikasi DB Manager.

![Preview aplikasi DB Manager](assets/db-manager-preview.gif)

## Authors

| Nama | NIM |
|---|---|
| Stefani Angeline Oroh | 13524064 |
| Bryan Pratama Putra Hendra | 13524067 |
| Angelina Andra Alanna | 13524079 |

## Struktur Program

```text
.
├── backend/
│   ├── cmd/
│   │   ├── api/                    # HTTP API server
│   │   ├── worker/                 # Persistent migration queue worker
│   │   ├── bootstrap-admin/        # Membuat platform admin pertama
│   │   └── migrate/                # Menjalankan migration platform
│   ├── internal/
│   │   ├── app/                    # Komposisi dependency dan startup aplikasi
│   │   ├── auth/                   # Login, password, dan bootstrap admin
│   │   ├── authz/                  # Resolusi hak akses user ke database
│   │   ├── database/               # Managed database dan enkripsi credential
│   │   ├── group/                  # Group dan membership
│   │   ├── migration/              # Draft, eksekusi, EXPLAIN, dan recovery
│   │   ├── platformdb/             # Koneksi ke database internal platform
│   │   ├── schemadiff/             # Introspeksi dan perbandingan schema
│   │   ├── session/                # Session cookie dan lifecycle session
│   │   ├── targetdb/               # Koneksi dan driver target PostgreSQL/MySQL
│   │   ├── user/                   # Administrasi user
│   │   ├── viewer/                 # Schema, table, metadata, dan row viewer
│   │   └── shared/                 # Komponen bersama backend
│   ├── migrations/                 # Goose migration untuk schema platform
│   └── tests/integration/          # Pengujian dengan database nyata
├── database/
│   └── seeds/university/           # Dataset universitas untuk target PostgreSQL
├── frontend/
│   └── src/
│       ├── app/                    # Router, halaman utama, dan setup aplikasi
│       ├── components/             # Komponen UI bersama
│       ├── lib/                    # API client dan utilitas frontend
│       ├── modules/                # Fitur auth, admin, database, viewer, migration
│       └── styles/                 # Style global dan token tampilan
├── compose.yml                     # Seluruh service untuk development lokal
├── Makefile                        # Shortcut command development dan testing
├── TESTING.md                      # Checklist pengujian manual dan otomatis
└── .env.example                    # Contoh konfigurasi lokal
```

Backend mengelompokkan kode berdasarkan domain di `internal/`; frontend
mengelompokkan layar dan API berdasarkan fitur di `modules/`. Migration platform
berada di `backend/migrations/`, sedangkan migration yang dibuat pengguna
disimpan pada database platform dan dijalankan ke target yang dipilih.

## Fitur Milestone 1

- Login berbasis session cookie `HttpOnly` dengan proteksi CSRF.
- Platform admin untuk mengelola user, group, membership, dan managed database.
- Grant database berbasis group: `view_only` dan `view_edit`.
- PostgreSQL dan MySQL 8.4 viewer untuk schema, table/view, struktur, index, constraint, dan row.
- Migration history dengan draft, versioning, commit, rollback, dan recovery.
- Enkripsi credential managed database menggunakan AES-256-GCM.
- Unit test, integration test PostgreSQL/MySQL, dan frontend test.

## Teknologi

| Bagian | Teknologi |
|---|---|
| Backend | Go 1.26, `net/http`, pgx, go-sql-driver/mysql |
| Frontend | React 19, TypeScript, Vite, TanStack Query |
| Platform database | PostgreSQL 18 |
| Managed target | PostgreSQL 18 atau MySQL 8.4 |
| Migration internal | Goose |
| Runtime frontend Docker | Nginx |
| Package manager frontend | Bun 1.3 |

## Quick Start dengan Docker

Docker Compose menjalankan seluruh stack: platform database, target database,
migration internal, backend API, migration worker, dan frontend.

### 1. Siapkan environment

```bash
cp .env.example .env
```

Nilai pada `.env.example` hanya untuk development lokal. Jangan commit `.env`
atau memakai credential contoh di production.

Jika `.env` berasal dari versi repository yang lebih lama, pastikan variabel
berikut tersedia:

```dotenv
MANAGED_DB_CREDENTIAL_KEY=<base64 dari tepat 32 byte>
MANAGED_DB_CREDENTIAL_KEY_VERSION=1
BACKEND_PORT=8080
FRONTEND_PORT=5173
HTTP_WRITE_TIMEOUT=3m
```

Generate encryption key baru bila diperlukan:

```bash
openssl rand -base64 32
```

Jangan mengganti key setelah credential managed database tersimpan. Credential
lama tidak dapat didekripsi dengan key yang berbeda.

### 2. Jalankan seluruh aplikasi

```bash
docker compose up --build -d --wait
```

Alternatif melalui Makefile:

```bash
make app-up
```

Urutan startup dikelola melalui healthcheck dan dependency:

```text
platform-db ──healthy──> migrate ──completed──> backend ──healthy──> frontend
                    └────────────────────────> worker
target-db / mysql-target ────────────────────> backend dan worker
```

### 3. Buat admin pertama

Jalankan satu kali secara interaktif:

```bash
docker compose run --rm bootstrap-admin
```

atau:

```bash
make docker-bootstrap-admin
```

Password dibaca tanpa echo dan tidak disimpan pada image. Bootstrap akan ditolak
jika platform sudah memiliki admin aktif.

### 4. Buka aplikasi

- Frontend: `http://localhost:5173`
- Backend health: `http://localhost:8080/api/v1/health`
- Backend readiness: `http://localhost:8080/api/v1/ready`

Frontend Nginx meneruskan request `/api` ke backend melalui jaringan Compose,
sehingga browser tetap menggunakan origin frontend.

### 5. Daftarkan target database contoh

Karena backend berjalan di jaringan Compose, gunakan konfigurasi berikut pada
halaman **Admin → Databases**:

| Field | Nilai |
|---|---|
| Engine | `PostgreSQL` |
| Host | `target-db` |
| Port | `5432` |
| Database | nilai `TARGET_DB_NAME` |
| Username | nilai `TARGET_DB_USER` |
| Password | nilai `TARGET_DB_PASSWORD` |
| SSL mode | `disable` |

Jangan menggunakan `localhost:55433` dari backend container. `localhost` di
dalam container menunjuk ke container backend itu sendiri.

Target MySQL contoh dapat didaftarkan dengan engine `MySQL 8.4`, host
`mysql-target`, port `3306`, database/username/password dari variabel
`MYSQL_TARGET_DB_*`, dan SSL mode `disable`.

### 6. Isi target PostgreSQL dengan data universitas

Seed opsional ini membuat delapan schema domain, puluhan table/view, serta
ratusan ribu row untuk mencoba viewer, schema diff, migration, dan EXPLAIN:

```bash
make seed-university
```

Command dapat dijalankan ulang. Setiap eksekusi hanya menghapus dan membuat
ulang schema `campus`, `people`, `academics`, `admissions`, `finance`,
`library`, `research`, dan `reporting` pada `target-db`. Schema lain tidak
diubah. Rincian jumlah data dan contoh query tersedia di
[panduan seed universitas](./database/seeds/university/README.md).

## Operasional Docker

Lihat status seluruh service:

```bash
make app-status
```

Ikuti log backend dan frontend:

```bash
make app-logs
```

Lihat log service tertentu:

```bash
docker compose logs -f migrate
docker compose logs -f backend
docker compose logs -f frontend
```

Hentikan stack tanpa menghapus data:

```bash
make app-down
```

Build ulang setelah source berubah:

```bash
docker compose up --build -d --wait
```

Reset seluruh database lokal hanya jika datanya tidak lagi dibutuhkan:

```bash
docker compose down -v
```

Perintah terakhir menghapus user, session, grant, migration history, dan seluruh
data target lokal.

## Local Development dengan Hot Reload Frontend

Mode ini menjalankan PostgreSQL melalui Docker, tetapi backend dan frontend dari
host. Gunakan mode ini ketika aktif mengembangkan source.

### Prasyarat

- Go 1.26 atau lebih baru;
- Bun 1.3 atau lebih baru;
- Docker dengan Docker Compose v2.

Pastikan `.env` sudah dibuat mengikuti langkah **Siapkan environment** di atas.

Install dependency frontend:

```bash
make frontend-install
```

Jalankan database dan migration internal:

```bash
make db-up
make migrate-up
```

Buat admin pertama dari host:

```bash
make bootstrap-admin
```

Jalankan backend pada terminal pertama:

```bash
make backend-run
```

Jalankan Vite pada terminal kedua:

```bash
make frontend-dev
```

Dalam mode ini, managed target database menggunakan:

| Field | Nilai |
|---|---|
| Engine | `PostgreSQL` |
| Host | `localhost` |
| Port | nilai `TARGET_DB_PORT`, default `55433` |
| Database | nilai `TARGET_DB_NAME` |
| Username | nilai `TARGET_DB_USER` |
| Password | nilai `TARGET_DB_PASSWORD` |
| SSL mode | `disable` |

Vite meneruskan `/api` ke `http://localhost:8080`.

Untuk target MySQL dari host, gunakan engine `MySQL 8.4`, host `localhost`,
port `MYSQL_TARGET_DB_PORT` (default `55434`), dan credential `MYSQL_TARGET_DB_*`.

## Model Akses

Role platform dan akses database merupakan dua hal terpisah:

- `platform_admin` dapat mengelola user, group, database, membership, dan grant;
- role admin tidak otomatis memberikan akses ke isi managed database;
- akses database hanya berasal dari grant group;
- `view_only` dapat memakai viewer dan membaca migration history;
- `view_edit` juga dapat membuat, mengubah, dan mengeksekusi draft migration;
- effective access adalah grant tertinggi dari seluruh group user;
- user atau database nonaktif selalu kehilangan akses;
- database tanpa grant dikembalikan sebagai 404 agar keberadaannya tidak bocor.

Perubahan privilege penting, password, role, atau status user mencabut session
terkait. User perlu login kembali setelah memperoleh privilege lebih tinggi.

## Database Viewer

Viewer menyediakan endpoint terstruktur untuk:

- daftar schema non-system;
- daftar table dan view;
- column, default, nullable, constraint, dan index;
- row dengan pagination (`limit` default 50, maksimum 100).

Viewer tidak menerima SQL bebas dan menjalankan query dalam transaksi read-only.
PostgreSQL memakai `statement_timeout`; MySQL dibatasi oleh deadline request.

## Target Database Migration

Setiap managed database memiliki urutan migration linear dengan status:

| Status | Arti |
|---|---|
| `draft` | Nama dan SQL masih dapat diubah |
| `running` | Sedang dijalankan pada target database |
| `succeeded` | Seluruh SQL berhasil diterapkan; jaminan transaksi bergantung pada engine dan jenis SQL |
| `failed` | SQL transaksional gagal dan perubahan di-rollback |
| `partial` | Batch tertentu sudah commit sebelum eksekusi berhenti; migration belum aktif dan dapat dilanjutkan atau di-rollback setelah approval |
| `partially_reverted` | Sebagian batch `down` sudah commit; migration tetap aktif sampai rollback selesai |
| `rolled_back` | Seluruh perubahan migration dibatalkan melalui Down SQL |
| `interrupted` | Hasil tidak pasti dan membutuhkan rekonsiliasi manual |
| `needs_reconciliation` | DDL non-transaksional gagal atau hasil eksekusi/checkpoint tidak pasti; periksa target sebelum migration berikutnya |
| `reconciled` | Kondisi target sudah diperiksa dan dicatat; ini bukan status sukses migration |

Backend melarang transaction control statement seperti `BEGIN`, `COMMIT`, dan
`ROLLBACK` di dalam SQL migration karena transaction dikelola oleh executor.

Pada PostgreSQL, DML dan DDL dijalankan dalam satu transaction. Pada MySQL, DML
(`SELECT`, `INSERT`, `UPDATE`, `DELETE`) dijalankan dalam transaction, sedangkan
DDL table/index yang didukung (`CREATE TABLE`, `ALTER TABLE`, `DROP TABLE`,
`CREATE INDEX`, `DROP INDEX`) dijalankan satu per satu tanpa transaction
pembungkus. MySQL melakukan implicit commit pada DDL, sehingga DDL tidak dapat
di-rollback bersama sebagai satu rangkaian. Karena itu DDL dan DML harus berada
di migration berbeda. Jika statement DDL gagal, status menjadi
`needs_reconciliation` dan migration berikutnya diblokir sampai kondisi target
diperiksa. User dengan akses `view_edit` dapat mencatat rekonsiliasi dari detail
migration setelah memeriksa target. Aksi meminta konfirmasi dan catatan, serta
menyimpan status sebelumnya, pelaksana, dan waktu. Rekonsiliasi tidak menjalankan
ulang SQL dan tidak menyatakan migration berhasil; gunakan migration baru untuk
menerapkan perubahan yang masih diperlukan. `interrupted` juga memakai alur yang
sama.

`view_edit` mengizinkan user menjalankan migration melalui aplikasi, tetapi
tidak menambahkan privilege pada akun database target. Koneksi menggunakan
credential yang disimpan untuk managed database; akun tersebut harus memiliki
privilege SQL yang diperlukan pada database target (misalnya `ALTER` untuk
mengubah tabel). Berikan privilege dengan cakupan sesempit mungkin. Kegagalan
privilege dari target tetap dicatat sebagai kegagalan migration; DDL MySQL
memerlukan pemeriksaan kondisi schema sebelum status rekonsiliasi dicatat.

Eksekusi berjalan lewat job queue persisten. API mengembalikan `202 Accepted`
untuk enqueue apply/retry/rollback/forward dan menyediakan endpoint status job.
Worker terpisah mengambil job dengan lease, memegang advisory lock selama
eksekusi, dan memeriksa ulang grant serta approval. Job sebelum attempt dapat
diambil ulang setelah worker restart; attempt dengan hasil target yang tidak
pasti ditahan untuk rekonsiliasi, tidak dijalankan ulang otomatis. Satu managed
database diproses berurutan, sedangkan database berbeda dapat diproses paralel.
Jika lease worker batch kedaluwarsa, worker pengganti memeriksa checkpoint target
sebelum mengubah status. Checkpoint konsisten menutup attempt sebagai `partial`
atau `partially_reverted` agar operator dapat meminta retry/rollback baru dengan
approval; bukti yang hilang atau tidak konsisten tetap memerlukan rekonsiliasi.
Tidak ada SQL yang dijalankan ulang otomatis oleh recovery.

Benchmark batch wajib memakai database PostgreSQL khusus pengujian. Siapkan
migration batch berstatus draft dengan SQL `UPDATE <TableName> SET value=value+1
WHERE id>$1 AND id<=$2`, selector `SELECT id FROM <TableName> WHERE id>$1 ORDER
BY id LIMIT $2`, dan Down SQL yang membalik perubahan; setujui revisinya sebelum
run. `scripts/benchmark-batch-migration.ps1` membuat fixture, mengukur UPDATE
non-batch sampai melewati satu detik, mereset nilai pada fixture yang sama,
menjalankan migration melalui API/queue/worker, memeriksa job serta batch, lalu
menyimpan JSON hasil tanpa credential. Jalankan hanya setelah mengisi environment
API/session/target dan cocokkan `-TableName` serta `-BatchSize` dengan definition
yang sudah disetujui. Verifikasi koneksi `psql` sama dengan target pada
`DatabaseId`, lalu berikan `-ConfirmTestDatabase` dan
`-ConfirmManagedTargetMatchesConnection`; script akan membuat dan menghapus tabel
fixture.

Hasil aktual 26 November 2026 pada PostgreSQL Compose lokal: fixture yang sama
berisi 2.000.000 row; baseline UPDATE non-batch 3.469,6 ms; migration aplikasi
berhasil dengan batch 10.000 row sebanyak 200 batch dan 2.000.000 row terverifikasi
dalam 5.710 ms. Job terminal tercapai dalam 5.773 ms. Rincian ID dan hasil yang
tidak memuat credential tersimpan di
[`scripts/evidence/batch-benchmark-local-2026-11-26.json`](scripts/evidence/batch-benchmark-local-2026-11-26.json).
Angka ini hasil satu run pada environment lokal tersebut, bukan jaminan performa
di environment lain.

## Approval Migration (Milestone 2)

Setiap group memiliki satu **leader** yang wajib merupakan anggota group
tersebut. Platform admin memilih leader di **Admin → Group & akses**
(`PUT /api/v1/admin/groups/{id}/leader`). Leader yang dikeluarkan dari group
otomatis berhenti menjadi leader.

Migration hanya dapat dijalankan setelah leader dari **setiap** group yang
memiliki akses `view_only` atau `view_edit` ke database tersebut menyetujui
revisi yang sama:

- setiap perubahan nama/SQL draft menaikkan `revision`; approval lama tidak
  berlaku dan approval ulang diperlukan;
- keputusan leader disimpan append-only (`approved` atau `rejected`, menolak
  wajib disertai catatan); keputusan terbaru per leader yang dihitung;
- group ber-akses tanpa leader membuat status review `blocked`;
- approval untuk `apply`, `rollback`, dan `forward` terpisah;
- gerbang diperiksa ulang tepat sebelum eksekusi, dan eksekusi dibatalkan bila
  SQL diedit setelah pemeriksaan.

| Endpoint | Kegunaan |
|---|---|
| `GET /api/v1/databases/{databaseId}/migrations/{migrationId}/approvals?action=apply` | Status review, group wajib, dan keputusan leader |
| `POST /api/v1/databases/{databaseId}/migrations/{migrationId}/approvals` | Leader memberi keputusan `{action, revision, decision, note}` |
| `POST /api/v1/databases/{databaseId}/migrations/{migrationId}/execute` | Enqueue apply, balas `202` dengan `jobId` dan `Location` |
| `POST /api/v1/databases/{databaseId}/migrations/{migrationId}/retry` | Enqueue retry |
| `POST /api/v1/databases/{databaseId}/migrations/{migrationId}/rollback` | Enqueue rollback |
| `POST /api/v1/databases/{databaseId}/migrations/{migrationId}/forward` | Enqueue forward |
| `GET /api/v1/databases/{databaseId}/migration-jobs/{jobId}` | Baca status job dan progres batch |
| `GET /api/v1/databases/{databaseId}/migration-history?limit=100&offset=0&hideRollback=false` | Riwayat execution berhalaman; filter pasangan rollback sukses dilakukan sebelum pagination |

| Kode error | Arti |
|---|---|
| `409 APPROVAL_REQUIRED` | Belum semua leader wajib menyetujui revisi ini |
| `409 APPROVAL_STALE_REVISION` | Migration diedit sejak halaman review dibuka |
| `409 APPROVAL_NOT_REVIEWABLE` | Status migration tidak sesuai dengan aksi |
| `403 APPROVAL_NOT_LEADER` | User bukan leader group yang punya akses ke database |
| `409 MIGRATION_REVISION_CHANGED` | SQL diedit saat eksekusi sedang diproses |
| `400 LEADER_NOT_MEMBER` / `LEADER_INACTIVE` | Leader harus anggota group yang aktif |

Halaman detail migration menampilkan panel review berisi status, keputusan
setiap leader, dan peringatan kompatibilitas dari SQL (misalnya penghapusan
kolom atau kolom `NOT NULL` tanpa `DEFAULT`). Tombol jalankan terkunci sampai
semua leader menyetujui.

## Bonus

- [SAST dengan CodeQL](.github/CODEQL.md): workflow
  `.github/workflows/codeql.yml` untuk Go dan TypeScript.
- [Observability: metrik Prometheus dan dashboard Grafana](observability/README.md):
  `docker compose --profile observability up -d`, lalu buka Grafana di
  http://localhost:3000.
- [Stress test k6 untuk queue migration](scripts/k6/migration-queue.js):
  membaca database dan mengirim job secara bersamaan, lalu polling job sampai
  status terminal. Setiap iterasi membutuhkan migration unik yang sudah
  disetujui pada manifest [`scripts/k6/cases.example.json`](scripts/k6/cases.example.json).
  Untuk beberapa migration berurutan pada database yang sama, isi
  `predecessorMigrationId` pada case berikutnya; script menunggu migration itu
  berstatus `succeeded` sebelum enqueue, sehingga VU paralel tidak mengirim versi
  yang salah urut. Dependency polling ikut dihitung sebagai request API.

  Salin contoh manifest, ganti ID dengan migration approved yang aman untuk
  dijalankan, dan siapkan sesi lokal serta CSRF token. Jalankan:

  ```bash
  export DBMP_API_BASE_URL=http://localhost:8080
  export DBMP_ORIGIN=http://localhost:5173
  export DBMP_SESSION_COOKIE='nilai-cookie-session'
  export DBMP_CSRF_TOKEN='nilai-csrf-token'
  export DBMP_CASES_FILE=/path/to/approved-cases.json
  k6 run scripts/k6/migration-queue.js
  ```

  Stage beban: baseline 1 VU, naik ke 10 lalu 30 VU, kemudian turun ke 10 dan
  0 VU. Threshold awal: p95 API < 1000 ms, error API < 2%, p95 queue wait < 5 s,
  dan p95 waktu eksekusi < 60 s. Atur `K6_P95_API_MS` dan `K6_API_ERROR_RATE`
  mengikuti baseline hasil ukur tim. Ringkasan k6 menampilkan p50/p95 API,
  request per second, error rate, queue wait, durasi job, serta jumlah/rate job
  yang selesai. Respons enqueue `202` dihitung sebagai job diterima, bukan
  keberhasilan migration; `job_execution_error_rate` dihitung dari status
  terminal selain `succeeded`. Setiap kasus manifest hanya dipakai sekali; jika
  jumlah iterasi melebihi jumlah kasus, iterasi berikutnya hanya menjalankan
  request baca dan menaikkan `migration_case_manifest_exhausted`; threshold akan
  menggagalkan run supaya hasilnya tidak dianggap stress test queue yang valid.
  Siapkan migration unik yang cukup untuk seluruh iterasi; file contoh tiga kasus
  hanya menunjukkan format manifest dan tidak cukup untuk ramp 30 VU. Custom
  metrics memiliki label `stage` (`baseline_1_vu`, `ramp_to_10_vu`, `peak_30_vu`,
  `ramp_down_to_10_vu`, `ramp_to_0_vu`, `drain`); throughput per stage dihitung
  dari jumlah `api_requests` atau `migration_jobs_completed` dibagi durasi stage.
  Latensi API dikelompokkan saat request terjadi; hasil job dikelompokkan menurut
  stage enqueue. Masa drain menunggu hingga batas polling job agar VU tidak
  dihentikan sebelum memantau hasilnya. Konfigurasi ramp dan threshold mengikuti
  [dokumentasi scenario k6](https://grafana.com/docs/k6/latest/using-k6/scenarios/)
  dan [threshold k6](https://grafana.com/docs/k6/latest/using-k6/thresholds/).

  Hasil run aktual 26 November 2026 pada Compose lokal yang terisolasi
  (PostgreSQL, 30 database target): k6 `0.56.0`, maksimum 30 VU, 9 menit
  ramp, 9.990 kasus approved unik tersedia, 3.468 iterasi terpakai. Semua
  3.468 job diterima dan berakhir `succeeded`; 10.404/10.404 checks lulus,
  API error `0%`, dan manifest tidak habis. Secara keseluruhan API p50/p95
  `2,487/6,993 ms`, throughput `25,687 request/s`, queue wait p95 `94 ms`,
  dan durasi job p95 `22 ms`. Seluruh threshold k6 lulus. Rincian machine-
  readable tanpa secret ada di
  [`scripts/evidence/k6-stress-local-2026-11-26.json`](scripts/evidence/k6-stress-local-2026-11-26.json).

  Tabel berikut mengelompokkan request menurut stage saat request terjadi,
  penerimaan job menurut stage enqueue, dan completion menurut waktu status
  terminal terpantau. Latensi queue/job dikelompokkan menurut stage enqueue.
  Tidak ada angka VU normal yang berlaku umum: pilih sesuai jumlah operator
  yang diperkirakan aktif bersamaan. Untuk aplikasi internal kelompok kecil,
  1 VU masuk akal bila biasanya hanya satu operator mengelola database; bila
  ketiga anggota memang sering bekerja bersamaan, gunakan 3 VU sebagai normal.
  Pedoman k6 juga menyarankan mencocokkan VU dengan concurrency yang diamati
  atau diperkirakan, bukan angka arbitrer ([panduan load profile k6](https://grafana.com/docs/learning-hub/k6-performance-testing/03-establishing-a-baseline/17-designing-load-profile/)).
  Kelompok menetapkan 1 VU sebagai beban normal pada 27 November 2026; hasil
  run ini memakai baseline yang disepakati tersebut.
  Stage `10 → 30` adalah ramp tiga menit yang mencapai 30 VU di ujung stage;
  bukan hold tiga menit pada 30 VU.

  | Stage | Beban VU | API p50/p95 (ms) | Request/s | Error API | Queue p95 (ms) | Job p95 (ms) | Diterima / selesai terpantau | Selesai/s | Analisis |
  |---|---:|---:|---:|---:|---:|---:|---:|---:|---|
  | Baseline (60 dtk) | 1 | 2,553 / 7,321 | 1,983 | 0% | 91 | 28 | 30 / 29 | 0,483 | Baseline stabil; seluruh latensi jauh di bawah threshold. |
  | Ramp (120 dtk) | 1 → 10 | 2,513 / 5,892 | 10,000 | 0% | 88 | 21 | 302 / 294 | 2,450 | Beban meningkat tanpa lonjakan error atau antrean. |
  | Ramp ke puncak (180 dtk) | 10 → 30 | 2,286 / 5,679 | 38,867 | 0% | 89 | 19 | 1.754 / 1.734 | 9,633 | Mencapai maksimum 30 VU di akhir stage; sistem tetap di bawah threshold. |
  | Ramp turun (120 dtk) | 30 → 10 | 2,830 / 8,321 | 40,750 | 0% | 99 | 23 | 1.218 / 1.236 | 10,300 | Ada completion job dari stage sebelumnya; p95 queue/job tetap rendah. |
  | Ramp ke nol (60 dtk) | 10 → 0 | 2,646 / 8,180 | 11,100 | 0% | 95 | 24 | 164 / 174 | 2,900 | Error tetap nol saat beban diturunkan; antrean tidak menumpuk. |
  | Drain (ekor graceful ramp-down) | 0 | 2,690 / 2,690* | — | 0% | — | — | 0 / 1 | — | Satu polling terminal terakhir; semua job akhirnya terpantau selesai. |

  `Diterima / selesai terpantau` memakai dua waktu berbeda, sehingga angka
  antartahap dapat berbeda: job yang diterima di satu stage dapat selesai pada
  stage berikutnya. Drain hanya berisi satu request, sehingga p95 satu sampel
  (`*`) dan throughput stage drain tidak representatif. Total akhir tetap
  3.468 diterima dan 3.468 selesai; tidak ada job yang tertinggal.

## Command Makefile

| Command | Kegunaan |
|---|---|
| `make app-up` | Build dan jalankan seluruh stack Docker |
| `make app-down` | Hentikan stack tanpa menghapus volume |
| `make app-status` | Lihat seluruh container termasuk one-shot migration |
| `make app-logs` | Ikuti log backend dan frontend |
| `make docker-bootstrap-admin` | Buat admin pertama melalui container |
| `make db-up` | Jalankan platform, target PostgreSQL, dan target MySQL |
| `make db-down` | Hentikan seluruh service Compose |
| `make migrate-up` | Jalankan migration internal dari host |
| `make migrate-status` | Lihat status migration internal |
| `make bootstrap-admin` | Buat admin pertama dari host |
| `make backend-run` | Jalankan backend dari host |
| `make frontend-dev` | Jalankan Vite development server |
| `make backend-test` | Jalankan seluruh backend test |
| `make backend-integration-test` | Jalankan integration test dengan PostgreSQL dan MySQL |
| `make frontend-check` | Typecheck, lint, test, dan build frontend |

## Catatan Production

Konfigurasi Docker Compose repository ini ditujukan untuk development dan
testing lokal. Sebelum production:

- gunakan secret manager dan encryption key unik;
- aktifkan TLS serta `SESSION_COOKIE_SECURE=true`;
- gunakan origin HTTPS publik pada `HTTP_TRUSTED_ORIGINS`;
- jangan publish port PostgreSQL ke jaringan publik;
- gunakan backup dan rotation policy untuk database;
- gunakan reverse proxy/load balancer yang tepercaya;
- evaluasi strategi recovery migration untuk lebih dari satu backend instance.
