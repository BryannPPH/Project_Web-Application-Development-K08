# Database Management Platform

Proyek Milestone 1 IF3110 Pengembangan Aplikasi Web. Backend menggunakan Go,
frontend menggunakan React + TypeScript, dan database menggunakan PostgreSQL.

## Prasyarat

- Go 1.26+
- Bun 1.3+
- Docker dengan Docker Compose

## Menjalankan database lokal

Salin konfigurasi development terlebih dahulu:

```bash
cp .env.example .env
```

Nilai di `.env.example` hanya ditujukan untuk development lokal. Jangan gunakan
credential tersebut di production dan jangan commit file `.env`.

Jalankan platform database dan target database:

```bash
make db-up
make migrate-up
```

Service lokal yang tersedia:

| Service | Port | Kegunaan |
|---|---:|---|
| `platform-db` | 55432 | User, session, access control, koneksi, dan migration history |
| `target-db` | 55433 | PostgreSQL contoh yang dikelola platform |

## Menjalankan aplikasi

```bash
make backend-run
make frontend-dev
```

Endpoint backend awal:

- `GET http://localhost:8080/api/v1/health` untuk liveness.
- `GET http://localhost:8080/api/v1/ready` untuk kesiapan koneksi platform database.

## Pemeriksaan lokal

```bash
make backend-test
make frontend-check
```

## Migration internal

Migration pada `backend/migrations` hanya mengatur schema internal platform.
Migration yang dibuat pengguna untuk managed database adalah fitur aplikasi yang
terpisah dan tidak dijalankan melalui command ini.
