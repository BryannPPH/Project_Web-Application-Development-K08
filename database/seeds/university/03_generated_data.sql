INSERT INTO people.persons (
    id, national_id, full_name, email, gender, birth_date, phone, created_at
)
SELECT
    person_no,
    'ID' || lpad(person_no::text, 14, '0'),
    (ARRAY['Aditya', 'Alya', 'Bagas', 'Citra', 'Dimas', 'Farah', 'Galih', 'Hana', 'Indra', 'Jasmine', 'Kevin', 'Laras'])[((person_no - 1) % 12) + 1]
        || ' ' ||
    (ARRAY['Pratama', 'Nugraha', 'Wijaya', 'Lestari', 'Saputra', 'Maharani', 'Kusuma', 'Permata'])[((person_no * 7 - 1) % 8) + 1]
        || ' ' || lpad(person_no::text, 5, '0'),
    'person.' || person_no || '@university.test',
    CASE WHEN person_no % 2 = 0 THEN 'F' ELSE 'M' END,
    DATE '1980-01-01' + ((person_no * 37) % 8500),
    '+6281' || lpad(person_no::text, 9, '0'),
    TIMESTAMPTZ '2021-01-01 08:00:00+07' + (person_no % 1460) * INTERVAL '1 day'
FROM generate_series(1, 12500) AS person_no;

INSERT INTO people.addresses (
    id, person_id, label, address_line, city, province, postal_code, is_primary
)
SELECT
    person.id,
    person.id,
    'home',
    'Jalan Pendidikan No. ' || person.id,
    (ARRAY['Bandung', 'Jakarta', 'Surabaya', 'Yogyakarta', 'Semarang', 'Bogor'])[((person.id - 1) % 6) + 1],
    (ARRAY['Jawa Barat', 'DKI Jakarta', 'Jawa Timur', 'DI Yogyakarta', 'Jawa Tengah', 'Jawa Barat'])[((person.id - 1) % 6) + 1],
    lpad((40000 + person.id % 50000)::text, 5, '0'),
    TRUE
FROM people.persons AS person;

INSERT INTO people.addresses (
    id, person_id, label, address_line, city, province, postal_code, is_primary
)
SELECT
    12500 + person.id,
    person.id,
    'temporary',
    'Asrama Mahasiswa Blok ' || chr(65 + ((person.id - 1) % 10)::integer) || ' No. ' || person.id,
    'Bandung',
    'Jawa Barat',
    '40132',
    FALSE
FROM people.persons AS person
WHERE person.id <= 3000;

INSERT INTO people.students (
    person_id, student_number, program_id, cohort_year, status, admitted_on
)
SELECT
    person.id,
    (2021 + ((person.id - 1) % 5))::text || lpad(person.id::text, 8, '0'),
    ((person.id - 1) % 24) + 1,
    2021 + ((person.id - 1) % 5),
    CASE
        WHEN person.id % 113 = 0 THEN 'dropped'
        WHEN person.id % 67 = 0 THEN 'leave'
        WHEN person.id % 19 = 0 AND ((person.id - 1) % 5) = 0 THEN 'graduated'
        ELSE 'active'
    END,
    make_date((2021 + ((person.id - 1) % 5))::integer, 8, 15)
FROM people.persons AS person
WHERE person.id <= 12000;

INSERT INTO people.staff (
    person_id, employee_number, department_id, staff_type, academic_rank, hired_on, is_active
)
SELECT
    person.id,
    'EMP' || lpad((person.id - 12000)::text, 6, '0'),
    ((person.id - 12001) % 24) + 1,
    CASE
        WHEN person.id % 10 < 7 THEN 'lecturer'
        WHEN person.id % 10 < 9 THEN 'researcher'
        ELSE 'administration'
    END,
    CASE
        WHEN person.id % 10 = 0 THEN NULL
        WHEN person.id % 4 = 0 THEN 'Profesor'
        WHEN person.id % 3 = 0 THEN 'Lektor Kepala'
        WHEN person.id % 2 = 0 THEN 'Lektor'
        ELSE 'Asisten Ahli'
    END,
    DATE '1995-01-01' + (((person.id * 29) % 9500)::integer),
    person.id % 97 <> 0
FROM people.persons AS person
WHERE person.id > 12000;

INSERT INTO academics.courses (
    id, department_id, code, name, course_number, credits, level, has_laboratory
)
SELECT
    (department.id - 1) * 30 + course_no,
    department.id,
    department.code || '-' || (100 + course_no),
    CASE ((course_no - 1) % 10)
        WHEN 0 THEN 'Pengantar dan Dasar'
        WHEN 1 THEN 'Matematika Terapan'
        WHEN 2 THEN 'Metode Komputasi'
        WHEN 3 THEN 'Analisis Sistem'
        WHEN 4 THEN 'Perancangan dan Implementasi'
        WHEN 5 THEN 'Laboratorium Terpadu'
        WHEN 6 THEN 'Etika Profesi'
        WHEN 7 THEN 'Manajemen Proyek'
        WHEN 8 THEN 'Topik Lanjut'
        ELSE 'Seminar Keilmuan'
    END || ' ' || department.name || ' ' || course_no,
    100 + course_no,
    CASE WHEN course_no % 10 = 0 THEN 2 ELSE 3 END,
    LEAST(8, ((course_no - 1) / 5) + 1),
    course_no % 6 = 0
FROM academics.departments AS department
CROSS JOIN generate_series(1, 30) AS course_no;

INSERT INTO academics.curriculum_courses (
    program_id, course_id, semester_recommended, is_mandatory
)
SELECT
    program.id,
    course.id,
    LEAST(12, ((course.course_number - 101) / 4) + 1),
    course.course_number <= 120 OR course.course_number % 3 <> 0
FROM academics.programs AS program
JOIN academics.courses AS course ON course.department_id = program.department_id;

INSERT INTO academics.course_prerequisites (
    course_id, prerequisite_course_id, minimum_grade
)
SELECT
    course.id,
    previous.id,
    CASE WHEN course.course_number % 4 = 0 THEN 'B' ELSE 'C' END
FROM academics.courses AS course
JOIN academics.courses AS previous
  ON previous.department_id = course.department_id
 AND previous.course_number = course.course_number - 1
WHERE course.course_number > 101;

INSERT INTO academics.classes (
    id, course_id, term_id, lecturer_id, room_id, section,
    weekday, starts_at, capacity, status
)
SELECT
    (term.id - 1) * 720 + course.id,
    course.id,
    term.id,
    12001 + (course.department_id - 1) + 24 * ((course.id + term.id) % 20),
    ((course.id + term.id * 13 - 1) % 160) + 1,
    'A',
    ((course.id + term.id) % 6) + 1,
    TIME '07:00' + ((course.id + term.id) % 6) * INTERVAL '90 minutes',
    CASE WHEN course.has_laboratory THEN 36 ELSE 60 END,
    CASE WHEN term.is_active THEN 'open' ELSE 'closed' END
FROM academics.courses AS course
CROSS JOIN campus.academic_terms AS term;

WITH enrollment_candidates AS (
    SELECT
        class.id AS class_id,
        student.person_id AS student_id,
        term.id AS term_id,
        term.starts_on,
        slot,
        45 + ((student.person_id * 7 + slot * 11) % 56) AS score
    FROM people.students AS student
    JOIN academics.programs AS program ON program.id = student.program_id
    CROSS JOIN generate_series(1, 12) AS slot
    JOIN campus.academic_terms AS term ON term.id = 1 + ((slot - 1) / 3)
    JOIN academics.courses AS course
      ON course.department_id = program.department_id
     AND course.course_number = 100 + (((student.person_id + slot - 2) % 30) + 1)
    JOIN academics.classes AS class
      ON class.course_id = course.id
     AND class.term_id = term.id
)
INSERT INTO academics.enrollments (
    class_id, student_id, enrolled_at, status, final_score, letter_grade
)
SELECT
    class_id,
    student_id,
    starts_on::timestamp + ((student_id + slot) % 10) * INTERVAL '1 hour',
    CASE
        WHEN term_id = 4 THEN 'enrolled'
        WHEN student_id % 97 = 0 THEN 'withdrawn'
        WHEN score < 50 THEN 'failed'
        ELSE 'completed'
    END,
    CASE WHEN term_id = 4 OR student_id % 97 = 0 THEN NULL ELSE score END,
    CASE
        WHEN term_id = 4 OR student_id % 97 = 0 THEN NULL
        WHEN score >= 85 THEN 'A'
        WHEN score >= 80 THEN 'AB'
        WHEN score >= 70 THEN 'B'
        WHEN score >= 65 THEN 'BC'
        WHEN score >= 55 THEN 'C'
        WHEN score >= 50 THEN 'D'
        ELSE 'E'
    END
FROM enrollment_candidates;

INSERT INTO academics.class_meetings (
    class_id, meeting_number, held_on, topic
)
SELECT
    class.id,
    meeting_no,
    term.starts_on + meeting_no * 7,
    'Pertemuan ' || meeting_no || ': pembahasan materi dan studi kasus'
FROM academics.classes AS class
JOIN campus.academic_terms AS term ON term.id = class.term_id
CROSS JOIN generate_series(1, 6) AS meeting_no
WHERE term.is_active;

INSERT INTO academics.attendance (
    meeting_id, student_id, status, recorded_at
)
SELECT
    meeting.id,
    enrollment.student_id,
    CASE (meeting.id + enrollment.student_id) % 20
        WHEN 0 THEN 'absent'
        WHEN 1 THEN 'excused'
        WHEN 2 THEN 'late'
        ELSE 'present'
    END,
    meeting.held_on + TIME '08:00'
FROM academics.class_meetings AS meeting
JOIN academics.enrollments AS enrollment ON enrollment.class_id = meeting.class_id;

INSERT INTO admissions.applications (
    id, application_number, person_id, term_id, admission_track, status, submitted_at
)
SELECT
    applicant_no,
    'APP-2025-' || lpad(applicant_no::text, 7, '0'),
    applicant_no,
    3,
    (ARRAY['achievement', 'national_test', 'independent'])[((applicant_no - 1) % 3) + 1],
    (ARRAY['submitted', 'verified', 'accepted', 'accepted', 'rejected'])[((applicant_no - 1) % 5) + 1],
    TIMESTAMPTZ '2025-02-01 08:00:00+07' + (applicant_no % 120) * INTERVAL '1 hour'
FROM generate_series(1, 9000) AS applicant_no;

INSERT INTO admissions.application_choices (application_id, priority, program_id)
SELECT application.id, 1, ((application.id - 1) % 24) + 1
FROM admissions.applications AS application
UNION ALL
SELECT application.id, 2, ((application.id + 6) % 24) + 1
FROM admissions.applications AS application;

INSERT INTO admissions.documents (
    application_id, document_type, file_name, verification_status, uploaded_at
)
SELECT
    application.id,
    document_type,
    lower(document_type) || '-' || application.application_number || '.pdf',
    CASE WHEN application.id % 19 = 0 THEN 'invalid' WHEN application.id % 7 = 0 THEN 'pending' ELSE 'valid' END,
    application.submitted_at + CASE document_type WHEN 'identity' THEN INTERVAL '1 minute' ELSE INTERVAL '2 minutes' END
FROM admissions.applications AS application
CROSS JOIN (VALUES ('identity'), ('transcript')) AS document(document_type);

INSERT INTO finance.student_bills (
    id, bill_number, student_id, term_id, issued_on, due_on, total_amount, status
)
SELECT
    (term.id - 3) * 12000 + student.person_id,
    'BILL-' || term.code || '-' || lpad(student.person_id::text, 8, '0'),
    student.person_id,
    term.id,
    term.starts_on - 30,
    term.starts_on - 5,
    14750000,
    CASE student.person_id % 10
        WHEN 0 THEN 'unpaid'
        WHEN 1 THEN 'partial'
        ELSE 'paid'
    END
FROM people.students AS student
CROSS JOIN campus.academic_terms AS term
WHERE term.id IN (3, 4);

INSERT INTO finance.bill_items (bill_id, fee_type_id, description, amount)
SELECT bill.id, fee.id, fee.name, fee.default_amount
FROM finance.student_bills AS bill
JOIN finance.fee_types AS fee ON fee.id IN (1, 2, 3);

INSERT INTO finance.payments (
    id, payment_number, bill_id, paid_at, amount, method, status
)
SELECT
    row_number() OVER (ORDER BY bill.id),
    'PAY-' || lpad(bill.id::text, 10, '0'),
    bill.id,
    bill.issued_on::timestamp + INTERVAL '10 days' + (bill.student_id % 72) * INTERVAL '1 hour',
    CASE WHEN bill.status = 'partial' THEN bill.total_amount / 2 ELSE bill.total_amount END,
    (ARRAY['bank_transfer', 'virtual_account', 'card', 'cash'])[((bill.student_id - 1) % 4) + 1],
    'settled'
FROM finance.student_bills AS bill
WHERE bill.status IN ('partial', 'paid');

INSERT INTO finance.student_scholarships (
    student_id, scholarship_id, starts_term_id, ends_term_id, status
)
SELECT
    student.person_id,
    ((student.person_id - 1) % 20) + 1,
    3,
    4,
    'active'
FROM people.students AS student
WHERE student.person_id % 5 = 0;

INSERT INTO library.authors (id, name, country)
SELECT
    author_no,
    'Penulis Akademik ' || lpad(author_no::text, 5, '0'),
    (ARRAY['Indonesia', 'Malaysia', 'Jepang', 'Jerman', 'Amerika Serikat', 'Australia'])[((author_no - 1) % 6) + 1]
FROM generate_series(1, 2000) AS author_no;

INSERT INTO library.books (
    id, isbn, title, publisher_id, published_year, category, page_count
)
SELECT
    book_no,
    '978-602-' || lpad((book_no / 100)::text, 4, '0') || '-' || lpad((book_no % 100)::text, 2, '0'),
    (ARRAY['Pengantar', 'Analisis', 'Prinsip', 'Metode', 'Aplikasi', 'Riset Lanjut'])[((book_no - 1) % 6) + 1]
        || ' ' ||
    (ARRAY['Komputasi', 'Rekayasa', 'Sains', 'Bisnis', 'Desain', 'Humaniora'])[((book_no * 5 - 1) % 6) + 1]
        || ' Volume ' || book_no,
    ((book_no - 1) % 100) + 1,
    1990 + (book_no % 36),
    (ARRAY['Teknologi', 'Sains', 'Ekonomi', 'Seni', 'Sosial', 'Referensi'])[((book_no - 1) % 6) + 1],
    120 + (book_no % 780)
FROM generate_series(1, 6000) AS book_no;

INSERT INTO library.book_authors (book_id, author_id, author_order)
SELECT book.id, ((book.id - 1) % 2000) + 1, 1
FROM library.books AS book
UNION ALL
SELECT book.id, ((book.id + 616) % 2000) + 1, 2
FROM library.books AS book;

INSERT INTO library.book_copies (
    id, book_id, barcode, acquired_on, location_code, status
)
SELECT
    (book.id - 1) * 2 + copy_no,
    book.id,
    'LIB-' || lpad(((book.id - 1) * 2 + copy_no)::text, 10, '0'),
    DATE '2015-01-01' + (((book.id * 13 + copy_no) % 3650)::integer),
    'R' || lpad(((book.id - 1) % 20 + 1)::text, 2, '0') || '-S' || lpad(copy_no::text, 2, '0'),
    CASE WHEN book.id % 997 = 0 THEN 'repair' ELSE 'available' END
FROM library.books AS book
CROSS JOIN generate_series(1, 2) AS copy_no;

INSERT INTO library.loans (
    id, copy_id, borrower_id, borrowed_at, due_at, returned_at, fine_amount
)
SELECT
    loan_no,
    ((loan_no * 17 - 1) % 12000) + 1,
    ((loan_no * 37 - 1) % 12000) + 1,
    TIMESTAMPTZ '2023-01-01 09:00:00+07' + (loan_no % 1000) * INTERVAL '8 hours',
    TIMESTAMPTZ '2023-01-15 09:00:00+07' + (loan_no % 1000) * INTERVAL '8 hours',
    CASE
        WHEN loan_no % 20 = 0 THEN NULL
        ELSE TIMESTAMPTZ '2023-01-10 15:00:00+07' + (loan_no % 1000) * INTERVAL '8 hours'
    END,
    CASE WHEN loan_no % 31 = 0 THEN 25000 ELSE 0 END
FROM generate_series(1, 50000) AS loan_no;

UPDATE library.book_copies AS copy
SET status = 'borrowed'
WHERE EXISTS (
    SELECT 1
    FROM library.loans AS loan
    WHERE loan.copy_id = copy.id
      AND loan.returned_at IS NULL
);

INSERT INTO research.projects (
    id, code, department_id, principal_investigator_id, title,
    starts_on, ends_on, budget, status
)
SELECT
    project_no,
    'RES-' || lpad(project_no::text, 5, '0'),
    ((project_no - 1) % 24) + 1,
    12001 + (((project_no - 1) % 24)) + 24 * (project_no % 20),
    'Proyek Riset Multidisiplin ' || project_no,
    DATE '2022-01-01' + (project_no % 900),
    CASE WHEN project_no % 4 = 0 THEN NULL ELSE DATE '2024-12-31' + (project_no % 700) END,
    50000000 + (project_no % 20) * 25000000,
    (ARRAY['proposed', 'active', 'active', 'completed'])[((project_no - 1) % 4) + 1]
FROM generate_series(1, 400) AS project_no;

INSERT INTO research.project_members (
    project_id, staff_id, member_role, allocation_percent
)
SELECT
    project.id,
    12001 + ((project.id * 13 + member_no * 31 - 1) % 500),
    CASE member_no WHEN 1 THEN 'principal investigator' WHEN 2 THEN 'co-investigator' ELSE 'research member' END,
    CASE member_no WHEN 1 THEN 40 WHEN 2 THEN 25 ELSE 15 END
FROM research.projects AS project
CROSS JOIN generate_series(1, 4) AS member_no;

INSERT INTO research.publications (
    id, project_id, doi, title, publication_type, published_on, citation_count
)
SELECT
    publication_no,
    ((publication_no - 1) % 400) + 1,
    '10.9999/university.' || publication_no,
    'Publikasi Hasil Riset ' || publication_no,
    (ARRAY['journal', 'conference', 'book_chapter', 'patent'])[((publication_no - 1) % 4) + 1],
    DATE '2022-01-01' + (publication_no % 1400),
    (publication_no * 17) % 500
FROM generate_series(1, 2000) AS publication_no;

INSERT INTO research.publication_authors (
    publication_id, staff_id, author_order, is_corresponding
)
SELECT
    publication.id,
    12001 + ((publication.id * 19 + author_no * 47 - 1) % 500),
    author_no,
    author_no = 1
FROM research.publications AS publication
CROSS JOIN generate_series(1, 3) AS author_no;
