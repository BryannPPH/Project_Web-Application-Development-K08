# Database Management Platform

Proyek Milestone 1 IF3110 Pengembangan Aplikasi Web. Backend menggunakan Go,
frontend menggunakan React + TypeScript, dan database menggunakan PostgreSQL.

## Prasyarat

- Go 1.26+
- Bun 1.3+
- Docker dengan Docker Compose

Instal dependency frontend setelah clone pertama kali:

```bash
make frontend-install
```

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

Buat platform admin pertama secara interaktif. Password dibaca tanpa echo dan
tidak diteruskan sebagai command-line argument:

```bash
make bootstrap-admin
```

Untuk environment non-interaktif, gunakan `BOOTSTRAP_ADMIN_USERNAME` dan
`BOOTSTRAP_ADMIN_PASSWORD` sebagai environment sementara. Jangan simpan password
bootstrap dalam `.env`, script, atau repository. Command menolak membuat admin
baru jika platform sudah memiliki platform admin aktif.

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

Frontend development berjalan di `http://localhost:5173`. Vite meneruskan
request `/api` ke backend `http://localhost:8080`, sehingga cookie autentikasi
tetap digunakan melalui same-origin URL dari browser.

Pada production, sajikan frontend dan endpoint `/api` melalui origin yang sama
menggunakan reverse proxy. Jangan mengaktifkan wildcard CORS untuk request yang
membawa cookie autentikasi.

Endpoint backend awal:

- `GET http://localhost:8080/api/v1/health` untuk liveness.
- `GET http://localhost:8080/api/v1/ready` untuk kesiapan koneksi platform database.
- `POST http://localhost:8080/api/v1/auth/login` untuk login username/password.
- `GET http://localhost:8080/api/v1/auth/me` untuk principal session aktif.
- `GET http://localhost:8080/api/v1/auth/csrf` untuk memperbarui CSRF token.
- `POST http://localhost:8080/api/v1/auth/logout` untuk revoke session aktif.

### Kontrak authentication untuk feature backend

- Feature handler tidak boleh membaca cookie atau tabel `sessions` sendiri.
- Bungkus route baca dengan `AuthMiddleware.RequireAuthenticated`.
- Bungkus route `POST`, `PUT`, `PATCH`, atau `DELETE` dengan
  `AuthMiddleware.ProtectMutation` agar authentication dan CSRF sama-sama dicek.
- Ambil identitas melalui `auth.PrincipalFromContext`. Pemeriksaan akses database
  tetap dilakukan oleh modul `authz`; role `platform_admin` bukan akses database.
- Login mengembalikan CSRF token di JSON dan opaque session hanya melalui cookie
  `HttpOnly`. Kirim CSRF token melalui header `X-CSRF-Token` pada mutation.
- Setelah reload halaman, frontend dapat mengambil CSRF baru melalui
  `GET /api/v1/auth/csrf`. Response authentication menggunakan `Cache-Control:
  no-store` dan tidak pernah mengembalikan session identifier.
- Frontend menyimpan CSRF hanya di memory. Gunakan `apiRequest` dari
  `frontend/src/lib/api-client.ts` untuk seluruh request agar cookie dan header
  CSRF diterapkan secara konsisten.
- Daftarkan modul Person 2/3 melalui `app.WithFeatureFactory`. Factory menerima
  `PasswordHasher` dan `SessionRevoker` tanpa bergantung pada implementasi konkret.
- Perubahan password, status aktif, atau privilege penting wajib membatalkan
  seluruh session user. Lakukan revocation sebelum commit perubahan; bila revoke
  gagal, batalkan perubahan agar session lama tidak memperoleh privilege baru.

Default session development adalah idle timeout 30 menit, absolute expiration
12 jam, rotation interval 15 menit, dan grace period token lama 30 detik.
Rotation tidak memperpanjang absolute expiration.

Login dibatasi berdasarkan username dan alamat koneksi client. Default-nya 10
percobaan per menit dan dapat diubah melalui `LOGIN_RATE_LIMIT_ATTEMPTS` serta
`LOGIN_RATE_LIMIT_WINDOW`. Jangan mempercayai `X-Forwarded-For` langsung; reverse
proxy perlu meneruskan alamat client sebagai peer yang tepercaya atau menerapkan
rate limit tambahan di proxy.

Mutation browser juga memeriksa header `Origin`/`Referer`. Origin frontend yang
berbeda dari host API, seperti Vite development server, harus didaftarkan pada
`HTTP_TRUSTED_ORIGINS` sebagai daftar origin yang dipisahkan koma. CSRF tetap
menjadi perlindungan utama ketika kedua header tersebut tidak tersedia. Jika TLS
berakhir di reverse proxy, masukkan origin HTTPS publik ke daftar tersebut.

## Platform admin, akses database, dan viewer

Backend wajib menerima `MANAGED_DB_CREDENTIAL_KEY` (base64 dari 32 byte) untuk
mengenkripsi password managed database dengan AES-256-GCM; tanpa key yang valid
backend menolak start. Nilai di `.env.example` hanya untuk development lokal.
Buat key production dengan `openssl rand -base64 32`.

Aturan akses yang dikunci tim:

- Role platform (`user`, `platform_admin`) terpisah dari akses database.
  Platform admin tetap membutuhkan grant group untuk membuka viewer/migration.
- Akses database hanya diberikan ke group: `view_only` atau `view_edit`.
- **Effective access** user = level tertinggi dari seluruh group yang diikutinya
  untuk database tersebut (`none < view_only < view_edit`). User nonaktif dan
  database nonaktif selalu tidak memiliki akses.
- Database tanpa grant diperlakukan sama dengan database yang tidak ada (`404`)
  agar keberadaannya tidak bocor.
- Grant dibaca dari database pada setiap request, sehingga pencabutan langsung
  berlaku. Kenaikan privilege (anggota baru di group ber-grant, grant baru, atau
  `view_only` → `view_edit`), perubahan role, deaktivasi, dan reset password
  mencabut seluruh session user terkait sehingga user login ulang dengan
  identifier baru.

Endpoint admin (`/api/v1/admin/users`, `/groups`, `/databases`, beserta
membership dan grant) hanya untuk platform admin. Endpoint user:
`GET /api/v1/databases`, `GET /api/v1/databases/{id}`, dan viewer
`/api/v1/databases/{id}/schemas[/{schema}/tables[/{table}[/rows]]]`.
Detail table menggabungkan column, constraint, dan index dalam satu response;
data rows dipisah karena terpaginasi (`limit` default 50, maksimum 100,
`offset` ≥ 0). Viewer berjalan di transaksi `READ ONLY` dengan
`statement_timeout` 10 detik dan tidak menerima SQL bebas.

Modul migration dapat memakai `authz.Service` dan `targetdb.Provider`; fake untuk
unit test tersedia di `internal/authz/authztest` dan `internal/targetdb/targetdbtest`.

## Pemeriksaan lokal

```bash
make backend-test
make frontend-check
```

Integration test backend menggunakan PostgreSQL platform dan target lokal
(`TEST_TARGET_DB_*` diisi otomatis dari `.env` oleh Makefile) dan hanya
membuat lalu menghapus fixture miliknya sendiri:

```bash
make backend-integration-test
```

## Migration internal

Migration pada `backend/migrations` hanya mengatur schema internal platform.
Migration yang dibuat pengguna untuk managed database adalah fitur aplikasi yang
terpisah dan tidak dijalankan melalui command ini.
