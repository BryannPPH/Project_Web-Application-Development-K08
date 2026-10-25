INSERT INTO campus.buildings (id, code, name, campus_name, opened_on, floors) VALUES
    (1, 'GKU-1', 'Gedung Kuliah Umum Barat', 'Kampus Utama', '1998-08-17', 6),
    (2, 'GKU-2', 'Gedung Kuliah Umum Timur', 'Kampus Utama', '2001-07-12', 6),
    (3, 'LAB-A', 'Laboratorium Sains Terpadu', 'Kampus Utama', '2008-03-20', 5),
    (4, 'LAB-B', 'Laboratorium Rekayasa', 'Kampus Utama', '2012-09-01', 7),
    (5, 'LIB', 'Perpustakaan Pusat', 'Kampus Utama', '1995-05-02', 8),
    (6, 'FT-A', 'Gedung Teknologi', 'Kampus Utara', '2016-01-15', 10),
    (7, 'MED', 'Gedung Ilmu Kesehatan', 'Kampus Selatan', '2018-06-30', 9),
    (8, 'ART', 'Gedung Seni dan Humaniora', 'Kampus Barat', '2010-11-10', 5);

INSERT INTO campus.rooms (id, building_id, code, floor, capacity, room_type)
SELECT
    (building.id - 1) * 20 + room_no,
    building.id,
    building.code || '-' || lpad(room_no::text, 3, '0'),
    ((room_no - 1) % building.floors) + 1,
    CASE
        WHEN room_no % 10 = 0 THEN 250
        WHEN room_no % 4 = 0 THEN 36
        ELSE 60 + (room_no % 3) * 20
    END,
    CASE
        WHEN room_no % 10 = 0 THEN 'auditorium'
        WHEN room_no % 4 = 0 THEN 'laboratory'
        WHEN room_no % 7 = 0 THEN 'office'
        ELSE 'classroom'
    END
FROM campus.buildings AS building
CROSS JOIN generate_series(1, 20) AS room_no;

INSERT INTO campus.academic_terms (id, code, name, starts_on, ends_on, is_active) VALUES
    (1, '2024-1', 'Semester Ganjil 2024/2025', '2024-08-19', '2024-12-20', FALSE),
    (2, '2024-2', 'Semester Genap 2024/2025', '2025-01-20', '2025-05-30', FALSE),
    (3, '2025-1', 'Semester Ganjil 2025/2026', '2025-08-18', '2025-12-19', FALSE),
    (4, '2025-2', 'Semester Genap 2025/2026', '2026-01-19', '2026-05-29', TRUE);

INSERT INTO academics.faculties (id, code, name, established_year) VALUES
    (1, 'STEI', 'Sekolah Teknik Elektro dan Informatika', 1947),
    (2, 'FTI', 'Fakultas Teknologi Industri', 1973),
    (3, 'FTTM', 'Fakultas Teknik Pertambangan dan Perminyakan', 1959),
    (4, 'FTSL', 'Fakultas Teknik Sipil dan Lingkungan', 1959),
    (5, 'FMIPA', 'Fakultas Matematika dan Ilmu Pengetahuan Alam', 1947),
    (6, 'SITH', 'Sekolah Ilmu dan Teknologi Hayati', 2006),
    (7, 'SBM', 'Sekolah Bisnis dan Manajemen', 2003),
    (8, 'FSRD', 'Fakultas Seni Rupa dan Desain', 1984);

INSERT INTO academics.departments (id, faculty_id, code, name, office_email) VALUES
    (1, 1, 'IF', 'Teknik Informatika', 'if@university.test'),
    (2, 1, 'EL', 'Teknik Elektro', 'el@university.test'),
    (3, 1, 'TI', 'Teknologi Informasi', 'ti@university.test'),
    (4, 2, 'TK', 'Teknik Kimia', 'tk@university.test'),
    (5, 2, 'TF', 'Teknik Fisika', 'tf@university.test'),
    (6, 2, 'MR', 'Manajemen Rekayasa', 'mr@university.test'),
    (7, 3, 'TA', 'Teknik Pertambangan', 'ta@university.test'),
    (8, 3, 'TM', 'Teknik Perminyakan', 'tm@university.test'),
    (9, 3, 'TG', 'Teknik Geofisika', 'tg@university.test'),
    (10, 4, 'SI', 'Teknik Sipil', 'si@university.test'),
    (11, 4, 'TL', 'Teknik Lingkungan', 'tl@university.test'),
    (12, 4, 'KL', 'Teknik Kelautan', 'kl@university.test'),
    (13, 5, 'MA', 'Matematika', 'ma@university.test'),
    (14, 5, 'FI', 'Fisika', 'fi@university.test'),
    (15, 5, 'KI', 'Kimia', 'ki@university.test'),
    (16, 6, 'BI', 'Biologi', 'bi@university.test'),
    (17, 6, 'BM', 'Mikrobiologi', 'bm@university.test'),
    (18, 6, 'BE', 'Rekayasa Hayati', 'be@university.test'),
    (19, 7, 'MB', 'Manajemen Bisnis', 'mb@university.test'),
    (20, 7, 'KWU', 'Kewirausahaan', 'kwu@university.test'),
    (21, 7, 'MBA', 'Administrasi Bisnis', 'mba@university.test'),
    (22, 8, 'SR', 'Seni Rupa', 'sr@university.test'),
    (23, 8, 'DKV', 'Desain Komunikasi Visual', 'dkv@university.test'),
    (24, 8, 'DI', 'Desain Interior', 'di@university.test');

INSERT INTO academics.programs (
    id, department_id, code, name, degree_level, accreditation, required_credits
)
SELECT
    department.id,
    department.id,
    'S1-' || department.code,
    'Sarjana ' || department.name,
    'bachelor',
    CASE WHEN department.id % 5 = 0 THEN 'baik_sekali' ELSE 'unggul' END,
    144
FROM academics.departments AS department;

INSERT INTO academics.programs (
    id, department_id, code, name, degree_level, accreditation, required_credits
)
SELECT
    24 + department.id / 2,
    department.id,
    'S2-' || department.code,
    'Magister ' || department.name,
    'master',
    CASE WHEN department.id % 6 = 0 THEN 'baik_sekali' ELSE 'unggul' END,
    36
FROM academics.departments AS department
WHERE department.id % 2 = 0;

INSERT INTO finance.fee_types (id, code, name, default_amount) VALUES
    (1, 'TUITION', 'Biaya penyelenggaraan pendidikan', 12500000),
    (2, 'LAB', 'Biaya praktikum dan laboratorium', 1500000),
    (3, 'FACILITY', 'Biaya fasilitas kampus', 750000),
    (4, 'ORIENTATION', 'Biaya orientasi mahasiswa', 500000),
    (5, 'GRADUATION', 'Biaya wisuda', 1250000);

INSERT INTO finance.scholarships (id, code, name, sponsor, coverage_percent)
SELECT
    scholarship_no,
    'SCH-' || lpad(scholarship_no::text, 3, '0'),
    'Beasiswa Prestasi ' || scholarship_no,
    CASE scholarship_no % 4
        WHEN 0 THEN 'Yayasan Pendidikan Nusantara'
        WHEN 1 THEN 'Kementerian Pendidikan'
        WHEN 2 THEN 'Mitra Industri'
        ELSE 'Alumni University'
    END,
    CASE scholarship_no % 4
        WHEN 0 THEN 100
        WHEN 1 THEN 75
        WHEN 2 THEN 50
        ELSE 25
    END
FROM generate_series(1, 20) AS scholarship_no;

INSERT INTO library.publishers (id, name, country)
SELECT
    publisher_no,
    'Penerbit Akademik ' || lpad(publisher_no::text, 3, '0'),
    (ARRAY['Indonesia', 'Singapura', 'Jepang', 'Amerika Serikat', 'Britania Raya'])[((publisher_no - 1) % 5) + 1]
FROM generate_series(1, 100) AS publisher_no;
