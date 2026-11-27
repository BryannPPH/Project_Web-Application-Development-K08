# Bonus SAST dengan CodeQL

## Metode dan cakupan

SAST dijalankan secara lokal dengan CodeQL CLI **2.27.6** dan query suite `security-extended` untuk Go dan JavaScript/TypeScript. Workflow GitHub Actions [`codeql.yml`](workflows/codeql.yml) juga tersedia, tetapi bukti di dokumen ini berasal dari CLI lokal.

Scan dilakukan pada **27 November 2026** terhadap working tree yang berbasis commit `374e813` (`main`). Working tree saat scan memiliki perubahan yang belum di-commit; karena itu `374e813` adalah base commit, bukan identitas commit yang sepenuhnya merepresentasikan source yang dipindai. Setelah scan, perubahan K6, perbaikan sanitizer, dan SARIF dicatat masing-masing di commit `fabea10`, `bed7081`, dan `714ecd4`; tidak ada push.

| Bahasa | Query suite | Cakupan | Temuan pada scan akhir | Bukti |
|---|---|---|---:|---|
| Go | `codeql/go-queries:codeql-suites/go-security-extended.qls` | 98 dari 142 file Go; seluruh 98 file produksi dianalisis, 44 file test tidak masuk database | 16, seluruhnya ditriage di bawah | [SARIF Go](codeql-evidence/codeql-go-2026-11-27.sarif) |
| JavaScript/TypeScript | `codeql/javascript-queries:codeql-suites/javascript-security-extended.qls` | 85 dari 85 file JavaScript/TypeScript; 1 dari 1 file GitHub Actions | 0 | [SARIF JavaScript/TypeScript](codeql-evidence/codeql-javascript-2026-11-27.sarif) |

SARIF berisi hasil dan metadata query dari CodeQL. Pemindaian ini tidak membuktikan aplikasi bebas dari kerentanan. Tidak ada tautan run GitHub Actions atau hasil yang diunggah ke GitHub karena autentikasi GitHub di lingkungan ini tidak valid; spesifikasi meminta bukti hasil CodeQL dan tidak mensyaratkan run GitHub Actions.

## Temuan Go dan keputusan

| Rule ID | Severity CodeQL | Lokasi | Dampak potensial | Keputusan dan alasan |
|---|---:|---|---|---|
| `go/cookie-secure-not-set` (3 temuan) | 4.0 | `backend/internal/httpapi/auth_middleware.go:179,185,192` | Cookie session tanpa atribut `Secure` dapat dikirim melalui koneksi HTTP jika konfigurasi insecure dipakai di lingkungan yang dapat diakses pihak lain. | **Risiko konfigurasi diterima dengan batasan lingkungan.** Cookie memakai `Secure` dari konfigurasi; konfigurasi produksi menolak `SESSION_COOKIE_SECURE=false` dan default produksi adalah `true`. Nilai `false` hanya diperlukan untuk pengembangan lokal melalui HTTP. Jangan gunakan konfigurasi lokal tersebut pada deployment. |
| `go/sql-injection` (6 temuan) | 8.8 | `backend/internal/targetdb/mysql.go:62,70,74,94,102,106` | SQL arbitrer yang tidak tepercaya dapat mengubah atau membaca data target. | **Risiko diterima sebagai fungsi inti aplikasi.** Adapter target memang harus mengirim SQL migration yang disusun pengguna. Jalur eksekusi memerlukan akses admin, approval yang diwajibkan, dan pemeriksaan kebijakan SQL. Dampaknya tetap tinggi jika kontrol akses atau approval dilewati; temuan tidak dianggap hilang atau aman secara umum. |
| `go/log-injection` (7 temuan) | 6.1 | `backend/internal/httpapi/middleware.go:49`; `backend/internal/targetdb/provider.go:102`; `backend/internal/targetdb/logging.go:73,78,277,290,296` | Karakter kontrol dari nilai yang dicatat berpotensi memalsukan batas antarbaris log pada format teks. | **False positive pada jalur produksi yang diperiksa.** API dan worker memakai `slog.JSONHandler`, yang meng-escape karakter kontrol sebagai bagian dari nilai JSON. Query/parameter juga melalui redaksi atau sanitasi. `slog.TextHandler` yang ditemukan hanya digunakan test dengan output discard/fixture. |

## Temuan JavaScript/TypeScript dan perbaikan

Scan awal menemukan dua `js/incomplete-multi-character-sanitization` (severity CodeQL 7.8) pada `frontend/src/assets/brand/senja-rig/scripts/build-rig.mjs:115` dan `:297`. Sanitasi komentar SVG satu-pass dapat membuka delimiter `<!--` baru setelah teks komentar dihapus; hasil SVG kemudian dipasang melalui `dangerouslySetInnerHTML`.

Kedua jalur kini memakai helper `withoutSvgComments`, yang menghapus komentar lengkap maupun komentar tanpa penutup dan mengulang proses sampai tidak ada perubahan. Assertion untuk SVG utama, delimiter komentar yang bertumpuk, dan komentar tanpa penutup lulus. Generator dijalankan dengan scaffold HyperFrames sementara karena template HyperFrames tidak ada di checkout; SVG website dan satu composition yang dihasilkan tidak mengandung delimiter komentar, lalu scaffold dibersihkan. Scan ulang dengan suite yang sama menghasilkan **0 temuan** pada cakupan JavaScript/TypeScript yang dianalisis. Perbaikan ada pada commit `bed7081`; file sumber di commit itu identik dengan file yang dipindai. Bukti SARIF Go dan JavaScript/TypeScript disimpan pada commit `714ecd4`.

## Batasan dan langkah berikutnya

- Hasil lokal serta SARIF memberi bukti scan dan cakupan untuk working tree yang disebut di atas; hasil tersebut bukan hasil scan commit remote. Commit perbaikan JavaScript `bed7081` dibuat sesudah scan; blob file sumbernya sama dengan source yang dipindai.
- Semua 16 temuan Go memiliki keputusan dan alasan di atas. Temuan SQL ditandai sebagai risiko desain dengan kontrol yang ada, bukan sebagai false positive.
- Commit perbaikan dan hasil scan ulang tercatat di atas; tidak ada commit atau push tambahan yang diperlukan untuk bukti lokal ini.
- CodeQL hanya mengidentifikasi pola yang dicakup query dan codebase yang dianalisis. Hasil tanpa alert tidak membuktikan aplikasi pasti aman.


## Scan ulang akhir Milestone 2

CodeQL CLI 2.27.6 dengan suite `security-extended` memeriksa working tree akhir
berbasis `2eb4662`. Go mencakup 101 dari 146 file dan memuat 19 hasil. Pemindaian
JavaScript/TypeScript mencakup 86 dari 86 file serta satu workflow GitHub Actions
dan menghasilkan 0 temuan. Tiga hasil Go tambahan berasal dari structured log
batch. Keputusan setiap hasil, dampak, severity, lokasi, dan checksum SARIF
tersedia dalam [triase akhir](codeql-evidence/milestone2-final-triage-2026-11-27.json).

- [SARIF Go akhir](codeql-evidence/codeql-go-milestone2-final-2026-11-27.sarif)
- [SARIF JavaScript/TypeScript akhir](codeql-evidence/codeql-javascript-milestone2-final-2026-11-27.sarif)
