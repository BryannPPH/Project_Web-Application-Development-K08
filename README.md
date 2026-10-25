# Database Management Platform

Database Management Platform (DBMP) adalah aplikasi web untuk mengelola akses dan
perubahan pada database PostgreSQL maupun MySQL dari satu antarmuka. Aplikasi ini
dibuat untuk Milestone 1 IF3110 Pengembangan Aplikasi Web, dengan fokus pada
kontrol akses berbasis group, database viewer read-only, dan migration yang
tercatat.

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
migration internal, backend, dan frontend.

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
target-db   ──healthy────────────────────────> backend
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
| `interrupted` | Hasil tidak pasti dan membutuhkan rekonsiliasi manual |
| `needs_reconciliation` | DDL MySQL gagal; periksa kondisi target sebelum migration berikutnya |
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

Saat backend start, entry `running` yang tertinggal dari proses sebelumnya
diubah menjadi `interrupted`. Implementasi Milestone 1 mengasumsikan satu
instance backend aktif.

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
