-- internal/repository/postgres/migrations/010_make_template_instance_id_optional.sql

ALTER TABLE templates ALTER COLUMN instance_id DROP NOT NULL;
