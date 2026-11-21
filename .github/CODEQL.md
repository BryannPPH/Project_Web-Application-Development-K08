# Bonus SAST dengan CodeQL

## Konfigurasi

Workflow: [`.github/workflows/codeql.yml`](workflows/codeql.yml)

| Aspek | Nilai |
|---|---|
| Bahasa | Go (`backend/`, build manual `go build ./...`) dan JavaScript/TypeScript (`frontend/`, tanpa build) |
| Query suite | `security-extended` (query keamanan bawaan + tambahan dengan presisi lebih rendah) |
| Pemicu | push ke `main`, pull request ke `main`, terjadwal setiap Senin 03.00 UTC, manual (`workflow_dispatch`) |
| Hasil | tab **Security → Code scanning** di GitHub, dan anotasi langsung di pull request |

> Code scanning gratis untuk repository publik. Untuk repository privat dibutuhkan GitHub Advanced Security.

## Cara menjalankan

1. Push workflow ke `main` (atau buka PR ke `main`).
2. Buka tab **Actions → CodeQL**, lalu tunggu kedua job (`go`, `javascript-typescript`) selesai.
3. Buka **Security → Code scanning** untuk melihat temuan.
4. Untuk scan ulang setelah perbaikan: **Actions → CodeQL → Run workflow**.

## Bukti scan

Isi setelah workflow berjalan:

| Tanggal | Commit | Job | Hasil | Tautan run |
|---|---|---|---|---|
| | | go | | |
| | | javascript-typescript | | |

Lampirkan screenshot halaman **Security → Code scanning** di `.github/codeql-evidence/` dan tautkan di sini.

## Temuan dan keputusan

Isi satu baris per temuan. Jika tidak ada temuan, hapus tabel dan isi bagian "Jika scan bersih".

| Rule ID | Severity | Lokasi (file:baris) | Dampak | Keputusan | Commit perbaikan | Hasil scan ulang |
|---|---|---|---|---|---|---|
| contoh: `go/sql-injection` | high | `backend/internal/...:42` | input user masuk ke query | diperbaiki: parameter `$1` | `abc1234` | tertutup |

Keputusan yang mungkin:
- **Diperbaiki**: sertakan commit perbaikan dan pastikan temuan tertutup di scan berikutnya.
- **Risiko diterima / false positive**: jelaskan alasannya (misalnya nilai berasal dari konstanta kode, bukan input user) dan tandai *Dismiss* di GitHub dengan alasan yang sama.

### Catatan untuk reviewer
Beberapa pola yang mungkin ditandai CodeQL tetapi sudah aman by design:
- SQL di `backend/internal/metrics/collector.go` (`durationQuery`) dirangkai dari **konstanta** batas bucket di kode, bukan input user.
- SQL migration milik user memang dijalankan ke target database: itu fungsi utama aplikasi, dibatasi oleh akses `view_edit`, approval leader, dan kebijakan SQL (`sql_policy.go`).

## Jika scan bersih

Tuliskan cakupannya: bahasa yang dipindai, jumlah file yang dianalisis (terlihat di log job "Perform CodeQL analysis"), query suite `security-extended`, tanggal dan commit scan, beserta screenshot "No code scanning alerts".
