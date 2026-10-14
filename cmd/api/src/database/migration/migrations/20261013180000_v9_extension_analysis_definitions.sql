-- +goose Up
-- Existing extensions remain usable. Re-uploading their schema supplies the
-- complete declaration required for associated analysis files.
ALTER TABLE schema_extensions ADD COLUMN definition jsonb;

-- +goose Down
ALTER TABLE schema_extensions DROP COLUMN definition;
