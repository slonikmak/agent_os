-- +goose Up
CREATE TABLE task_materials (
 id TEXT PRIMARY KEY,
 task_id TEXT NOT NULL REFERENCES tasks(id),
 content_sha256 TEXT NOT NULL CHECK(length(content_sha256)=64 AND content_sha256 NOT GLOB '*[^0-9a-f]*'),
 byte_count INTEGER NOT NULL CHECK(byte_count BETWEEN 0 AND 1048576),
 media_type TEXT NOT NULL CHECK(media_type IN ('text/plain','text/markdown')),
 label TEXT,
 uploaded_client_id TEXT NOT NULL,
 uploaded_role TEXT NOT NULL CHECK(uploaded_role='coordinator'),
 uploaded_at TEXT NOT NULL,
 task_revision INTEGER NOT NULL CHECK(task_revision BETWEEN 2 AND 9007199254740991),
 UNIQUE(task_id,task_revision)
);
-- +goose StatementBegin
CREATE TRIGGER task_materials_immutable_update BEFORE UPDATE ON task_materials BEGIN SELECT RAISE(ABORT,'immutable material'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER task_materials_immutable_delete BEFORE DELETE ON task_materials BEGIN SELECT RAISE(ABORT,'immutable material'); END;
-- +goose StatementEnd
-- +goose Down
DROP TRIGGER task_materials_immutable_delete;
DROP TRIGGER task_materials_immutable_update;
DROP TABLE task_materials;
