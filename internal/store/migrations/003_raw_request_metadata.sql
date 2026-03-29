ALTER TABLE raw_documents ADD COLUMN request_path TEXT NOT NULL DEFAULT '';

ALTER TABLE raw_documents ADD COLUMN request_query TEXT NOT NULL DEFAULT '';

ALTER TABLE raw_documents ADD COLUMN request_start TEXT NOT NULL DEFAULT '';

ALTER TABLE raw_documents ADD COLUMN request_end TEXT NOT NULL DEFAULT '';
