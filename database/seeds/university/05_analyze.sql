SELECT setval(pg_get_serial_sequence('campus.buildings', 'id'), (SELECT max(id) FROM campus.buildings), TRUE);
SELECT setval(pg_get_serial_sequence('campus.rooms', 'id'), (SELECT max(id) FROM campus.rooms), TRUE);
SELECT setval(pg_get_serial_sequence('campus.academic_terms', 'id'), (SELECT max(id) FROM campus.academic_terms), TRUE);
SELECT setval(pg_get_serial_sequence('people.persons', 'id'), (SELECT max(id) FROM people.persons), TRUE);
SELECT setval(pg_get_serial_sequence('people.addresses', 'id'), (SELECT max(id) FROM people.addresses), TRUE);
SELECT setval(pg_get_serial_sequence('academics.faculties', 'id'), (SELECT max(id) FROM academics.faculties), TRUE);
SELECT setval(pg_get_serial_sequence('academics.departments', 'id'), (SELECT max(id) FROM academics.departments), TRUE);
SELECT setval(pg_get_serial_sequence('academics.programs', 'id'), (SELECT max(id) FROM academics.programs), TRUE);
SELECT setval(pg_get_serial_sequence('academics.courses', 'id'), (SELECT max(id) FROM academics.courses), TRUE);
SELECT setval(pg_get_serial_sequence('academics.classes', 'id'), (SELECT max(id) FROM academics.classes), TRUE);
SELECT setval(pg_get_serial_sequence('academics.enrollments', 'id'), (SELECT max(id) FROM academics.enrollments), TRUE);
SELECT setval(pg_get_serial_sequence('academics.class_meetings', 'id'), (SELECT max(id) FROM academics.class_meetings), TRUE);
SELECT setval(pg_get_serial_sequence('admissions.applications', 'id'), (SELECT max(id) FROM admissions.applications), TRUE);
SELECT setval(pg_get_serial_sequence('admissions.documents', 'id'), (SELECT max(id) FROM admissions.documents), TRUE);
SELECT setval(pg_get_serial_sequence('finance.fee_types', 'id'), (SELECT max(id) FROM finance.fee_types), TRUE);
SELECT setval(pg_get_serial_sequence('finance.student_bills', 'id'), (SELECT max(id) FROM finance.student_bills), TRUE);
SELECT setval(pg_get_serial_sequence('finance.bill_items', 'id'), (SELECT max(id) FROM finance.bill_items), TRUE);
SELECT setval(pg_get_serial_sequence('finance.payments', 'id'), (SELECT max(id) FROM finance.payments), TRUE);
SELECT setval(pg_get_serial_sequence('finance.scholarships', 'id'), (SELECT max(id) FROM finance.scholarships), TRUE);
SELECT setval(pg_get_serial_sequence('library.publishers', 'id'), (SELECT max(id) FROM library.publishers), TRUE);
SELECT setval(pg_get_serial_sequence('library.authors', 'id'), (SELECT max(id) FROM library.authors), TRUE);
SELECT setval(pg_get_serial_sequence('library.books', 'id'), (SELECT max(id) FROM library.books), TRUE);
SELECT setval(pg_get_serial_sequence('library.book_copies', 'id'), (SELECT max(id) FROM library.book_copies), TRUE);
SELECT setval(pg_get_serial_sequence('library.loans', 'id'), (SELECT max(id) FROM library.loans), TRUE);
SELECT setval(pg_get_serial_sequence('research.projects', 'id'), (SELECT max(id) FROM research.projects), TRUE);
SELECT setval(pg_get_serial_sequence('research.publications', 'id'), (SELECT max(id) FROM research.publications), TRUE);

DO $$
DECLARE
    target RECORD;
BEGIN
    FOR target IN
        SELECT schemaname, tablename
        FROM pg_tables
        WHERE schemaname IN (
            'campus', 'people', 'academics', 'admissions',
            'finance', 'library', 'research'
        )
    LOOP
        EXECUTE format('ANALYZE %I.%I', target.schemaname, target.tablename);
    END LOOP;
END;
$$;
