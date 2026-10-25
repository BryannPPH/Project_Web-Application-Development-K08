# University Dataset

Dataset ini mengisi PostgreSQL `target-db` untuk demonstrasi Database Management
Platform. Seluruh data bersifat sintetis dan deterministik.

## Menjalankan seed

Dari root repository:

```bash
make db-up
make seed-university
```

Seed dapat dijalankan ulang. Proses berjalan dalam satu transaction dan hanya
mereset schema berikut:

- `campus`: gedung, ruang, dan periode akademik;
- `people`: profil, alamat, mahasiswa, dan staf;
- `academics`: fakultas, program, mata kuliah, kelas, enrollment, dan presensi;
- `admissions`: pendaftaran, pilihan program, dan dokumen;
- `finance`: tagihan, pembayaran, dan beasiswa;
- `library`: koleksi, eksemplar, penulis, dan peminjaman;
- `research`: proyek, anggota, dan publikasi;
- `reporting`: enam view lintas domain.

## Skala data

Dataset berisi sekitar 40 tabel/view dan lebih dari 500.000 row, termasuk:

| Data | Jumlah |
|---|---:|
| Orang | 12.500 |
| Mahasiswa | 12.000 |
| Mata kuliah | 720 |
| Kelas | 2.880 |
| Enrollment | 144.000 |
| Presensi | 216.000 |
| Tagihan dan item | 96.000 |
| Buku dan eksemplar | 18.000 |
| Peminjaman | 50.000 |
| Proyek dan publikasi | 2.400 |

## Koneksi melalui aplikasi

Jika seluruh aplikasi berjalan melalui Docker Compose, daftarkan target berikut:

| Field | Nilai |
|---|---|
| Engine | PostgreSQL |
| Host | `target-db` |
| Port | `5432` |
| Database | nilai `TARGET_DB_NAME` |
| Username | nilai `TARGET_DB_USER` |
| Password | nilai `TARGET_DB_PASSWORD` |
| SSL mode | `disable` |

Contoh query untuk mencoba EXPLAIN:

```sql
SELECT
    student_number,
    full_name,
    average_score
FROM reporting.student_academic_summary
WHERE program_code = 'S1-IF'
ORDER BY average_score DESC
LIMIT 20;
```

```sql
SELECT
    borrower_id,
    count(*) AS overdue_loans
FROM reporting.library_active_loans
WHERE is_overdue
GROUP BY borrower_id
ORDER BY overdue_loans DESC
LIMIT 20;
```
