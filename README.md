# Database Management Platform

Database Management Platform adalah proyek Milestone 1 IF3110 Pengembangan
Aplikasi Web. Aplikasi menyediakan authentication, administrasi user dan group,
kontrol akses database, PostgreSQL viewer read-only, serta migration executor
transactional.

## Fitur Milestone 1

- Login berbasis session cookie `HttpOnly` dengan proteksi CSRF.
- Platform admin untuk mengelola user, group, membership, dan managed database.
- Grant database berbasis group: `view_only` dan `view_edit`.
- PostgreSQL viewer untuk schema, table/view, struktur, index, constraint, dan row.
- Migration history dengan draft, versioning, commit, rollback, dan recovery.
- Enkripsi credential managed database menggunakan AES-256-GCM.
- Unit test, integration test PostgreSQL, race detector, dan frontend test.

## Teknologi

| Bagian | Teknologi |
|---|---|
| Backend | Go 1.26, `net/http`, pgx |
| Frontend | React 19, TypeScript, Vite, TanStack Query |
| Database | PostgreSQL 18 |
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
| Host | `target-db` |
| Port | `5432` |
| Database | nilai `TARGET_DB_NAME` |
| Username | nilai `TARGET_DB_USER` |
| Password | nilai `TARGET_DB_PASSWORD` |
| SSL mode | `disable` |

Jangan menggunakan `localhost:55433` dari backend container. `localhost` di
dalam container menunjuk ke container backend itu sendiri.

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
| Host | `localhost` |
| Port | nilai `TARGET_DB_PORT`, default `55433` |
| Database | nilai `TARGET_DB_NAME` |
| Username | nilai `TARGET_DB_USER` |
| Password | nilai `TARGET_DB_PASSWORD` |
| SSL mode | `disable` |

Vite meneruskan `/api` ke `http://localhost:8080`.

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

Viewer tidak menerima SQL bebas dan menjalankan query dalam transaksi read-only
dengan `statement_timeout`.

## Target Database Migration

Setiap managed database memiliki urutan migration linear dengan status:

| Status | Arti |
|---|---|
| `draft` | Nama dan SQL masih dapat diubah |
| `running` | Sedang dijalankan pada target database |
| `succeeded` | Seluruh SQL berhasil dan transaction di-commit |
| `failed` | SQL gagal dan transaction di-rollback |
| `interrupted` | Hasil tidak pasti dan membutuhkan rekonsiliasi manual |

Backend melarang transaction control statement seperti `BEGIN`, `COMMIT`, dan
`ROLLBACK` di dalam SQL migration karena transaction dikelola oleh executor.

Saat backend start, entry `running` yang tertinggal dari proses sebelumnya
diubah menjadi `interrupted`. Implementasi Milestone 1 mengasumsikan satu
instance backend aktif.

## Testing

Panduan automated test, manual test, data fixture, authorization matrix,
migration commit/rollback, security checklist, dan kriteria kelulusan tersedia di
[TESTING.md](./TESTING.md).

Pemeriksaan utama:

```bash
make backend-test
make backend-integration-test
make frontend-check
```

Integration test membutuhkan `platform-db` dan `target-db` yang healthy.

## Command Makefile

| Command | Kegunaan |
|---|---|
| `make app-up` | Build dan jalankan seluruh stack Docker |
| `make app-down` | Hentikan stack tanpa menghapus volume |
| `make app-status` | Lihat seluruh container termasuk one-shot migration |
| `make app-logs` | Ikuti log backend dan frontend |
| `make docker-bootstrap-admin` | Buat admin pertama melalui container |
| `make db-up` | Jalankan hanya platform dan target database |
| `make db-down` | Hentikan seluruh service Compose |
| `make migrate-up` | Jalankan migration internal dari host |
| `make migrate-status` | Lihat status migration internal |
| `make bootstrap-admin` | Buat admin pertama dari host |
| `make backend-run` | Jalankan backend dari host |
| `make frontend-dev` | Jalankan Vite development server |
| `make backend-test` | Jalankan seluruh backend test |
| `make backend-integration-test` | Jalankan integration test PostgreSQL |
| `make frontend-check` | Typecheck, lint, test, dan build frontend |

## Struktur Repository

```text
.
├── backend/
│   ├── cmd/                  # api, migrate, bootstrap-admin
│   ├── internal/             # domain dan infrastructure backend
│   ├── migrations/           # schema internal platform
│   ├── tests/integration/    # integration test PostgreSQL
│   └── Dockerfile
├── frontend/
│   ├── src/                  # React application
│   ├── Dockerfile
│   └── nginx.conf
├── compose.yml
├── Makefile
├── TESTING.md
└── .env.example
```

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
