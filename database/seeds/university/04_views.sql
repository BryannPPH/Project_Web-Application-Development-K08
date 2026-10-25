CREATE VIEW reporting.student_academic_summary AS
SELECT
    student.person_id AS student_id,
    student.student_number,
    person.full_name,
    faculty.code AS faculty_code,
    department.code AS department_code,
    program.code AS program_code,
    student.cohort_year,
    student.status AS student_status,
    count(enrollment.id) AS enrollment_count,
    count(enrollment.id) FILTER (WHERE enrollment.status = 'completed') AS completed_courses,
    round(avg(enrollment.final_score) FILTER (WHERE enrollment.final_score IS NOT NULL), 2) AS average_score
FROM people.students AS student
JOIN people.persons AS person ON person.id = student.person_id
JOIN academics.programs AS program ON program.id = student.program_id
JOIN academics.departments AS department ON department.id = program.department_id
JOIN academics.faculties AS faculty ON faculty.id = department.faculty_id
LEFT JOIN academics.enrollments AS enrollment ON enrollment.student_id = student.person_id
GROUP BY
    student.person_id,
    student.student_number,
    person.full_name,
    faculty.code,
    department.code,
    program.code,
    student.cohort_year,
    student.status;

CREATE VIEW reporting.class_performance AS
SELECT
    class.id AS class_id,
    term.code AS term_code,
    course.code AS course_code,
    course.name AS course_name,
    class.section,
    lecturer.full_name AS lecturer_name,
    count(enrollment.id) AS enrolled_students,
    count(enrollment.id) FILTER (WHERE enrollment.status = 'completed') AS completed_students,
    round(avg(enrollment.final_score) FILTER (WHERE enrollment.final_score IS NOT NULL), 2) AS average_score,
    min(enrollment.final_score) AS minimum_score,
    max(enrollment.final_score) AS maximum_score
FROM academics.classes AS class
JOIN campus.academic_terms AS term ON term.id = class.term_id
JOIN academics.courses AS course ON course.id = class.course_id
JOIN people.persons AS lecturer ON lecturer.id = class.lecturer_id
LEFT JOIN academics.enrollments AS enrollment ON enrollment.class_id = class.id
GROUP BY class.id, term.code, course.code, course.name, class.section, lecturer.full_name;

CREATE VIEW reporting.student_financial_balance AS
WITH paid AS (
    SELECT
        payment.bill_id,
        sum(payment.amount) FILTER (WHERE payment.status = 'settled') AS paid_amount
    FROM finance.payments AS payment
    GROUP BY payment.bill_id
)
SELECT
    bill.id AS bill_id,
    bill.bill_number,
    bill.student_id,
    student.student_number,
    person.full_name,
    term.code AS term_code,
    bill.total_amount,
    coalesce(paid.paid_amount, 0) AS paid_amount,
    bill.total_amount - coalesce(paid.paid_amount, 0) AS outstanding_amount,
    bill.status,
    bill.due_on
FROM finance.student_bills AS bill
JOIN people.students AS student ON student.person_id = bill.student_id
JOIN people.persons AS person ON person.id = bill.student_id
JOIN campus.academic_terms AS term ON term.id = bill.term_id
LEFT JOIN paid ON paid.bill_id = bill.id;

CREATE VIEW reporting.library_active_loans AS
SELECT
    loan.id AS loan_id,
    loan.borrower_id,
    student.student_number,
    person.full_name,
    book.isbn,
    book.title,
    copy.barcode,
    loan.borrowed_at,
    loan.due_at,
    CURRENT_DATE > loan.due_at::date AS is_overdue
FROM library.loans AS loan
JOIN library.book_copies AS copy ON copy.id = loan.copy_id
JOIN library.books AS book ON book.id = copy.book_id
JOIN people.students AS student ON student.person_id = loan.borrower_id
JOIN people.persons AS person ON person.id = loan.borrower_id
WHERE loan.returned_at IS NULL;

CREATE VIEW reporting.admissions_funnel AS
SELECT
    term.code AS term_code,
    application.admission_track,
    application.status,
    count(*) AS application_count,
    count(*) FILTER (
        WHERE EXISTS (
            SELECT 1
            FROM admissions.documents AS document
            WHERE document.application_id = application.id
              AND document.verification_status = 'valid'
        )
    ) AS applications_with_valid_document
FROM admissions.applications AS application
JOIN campus.academic_terms AS term ON term.id = application.term_id
GROUP BY term.code, application.admission_track, application.status;

CREATE VIEW reporting.department_dashboard AS
WITH program_totals AS (
    SELECT department_id, count(*) AS program_count
    FROM academics.programs
    GROUP BY department_id
),
course_totals AS (
    SELECT department_id, count(*) AS course_count
    FROM academics.courses
    GROUP BY department_id
),
student_totals AS (
    SELECT program.department_id, count(*) AS student_count
    FROM people.students AS student
    JOIN academics.programs AS program ON program.id = student.program_id
    GROUP BY program.department_id
),
staff_totals AS (
    SELECT department_id, count(*) AS staff_count
    FROM people.staff
    GROUP BY department_id
),
project_totals AS (
    SELECT department_id, count(*) AS research_project_count
    FROM research.projects
    GROUP BY department_id
)
SELECT
    department.id AS department_id,
    faculty.code AS faculty_code,
    department.code AS department_code,
    department.name AS department_name,
    coalesce(program_totals.program_count, 0) AS program_count,
    coalesce(course_totals.course_count, 0) AS course_count,
    coalesce(student_totals.student_count, 0) AS student_count,
    coalesce(staff_totals.staff_count, 0) AS staff_count,
    coalesce(project_totals.research_project_count, 0) AS research_project_count
FROM academics.departments AS department
JOIN academics.faculties AS faculty ON faculty.id = department.faculty_id
LEFT JOIN program_totals ON program_totals.department_id = department.id
LEFT JOIN course_totals ON course_totals.department_id = department.id
LEFT JOIN student_totals ON student_totals.department_id = department.id
LEFT JOIN staff_totals ON staff_totals.department_id = department.id
LEFT JOIN project_totals ON project_totals.department_id = department.id;
