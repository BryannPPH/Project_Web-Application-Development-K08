\set ON_ERROR_STOP on
\timing on

BEGIN;
SET LOCAL client_min_messages = warning;

\ir 00_reset.sql
\ir 01_schema.sql
\ir 02_reference_data.sql
\ir 03_generated_data.sql
\ir 04_views.sql
\ir 05_analyze.sql

COMMIT;

SELECT 'university seed completed' AS status;
