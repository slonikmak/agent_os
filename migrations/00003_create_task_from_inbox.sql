-- +goose Up
ALTER TABLE workspace_counters ADD COLUMN task_sequence INTEGER NOT NULL DEFAULT 0 CHECK(task_sequence BETWEEN 0 AND 9007199254740991);
ALTER TABLE workspace_counters ADD COLUMN event_sequence INTEGER NOT NULL DEFAULT 0 CHECK(event_sequence BETWEEN 0 AND 9007199254740991);
ALTER TABLE workspace_counters ADD COLUMN resolution_sequence INTEGER NOT NULL DEFAULT 0 CHECK(resolution_sequence BETWEEN 0 AND 9007199254740991);
-- Legacy summaries have no saved creation facts. Keep those facts absent.
CREATE TABLE task_creation (
 task_id TEXT PRIMARY KEY REFERENCES tasks(id),
 created_at TEXT NOT NULL,
 inbox_message_id TEXT NOT NULL REFERENCES inbox_messages(id),
 submitted_client_id TEXT NOT NULL,
 submitted_role TEXT NOT NULL CHECK(submitted_role IN ('worker','coordinator')),
 materials_json TEXT NOT NULL
);
CREATE TABLE task_events (
 id TEXT PRIMARY KEY,
 task_id TEXT NOT NULL REFERENCES tasks(id),
 kind TEXT NOT NULL CHECK(kind = 'task.created'),
 at TEXT NOT NULL,
 reason TEXT NOT NULL,
 inbox_message_id TEXT NOT NULL REFERENCES inbox_messages(id),
 submitted_client_id TEXT NOT NULL,
 submitted_role TEXT NOT NULL CHECK(submitted_role IN ('worker','coordinator')),
 applied_client_id TEXT NOT NULL,
 applied_role TEXT NOT NULL CHECK(applied_role = 'coordinator')
);
CREATE TABLE inbox_resolutions (
 id TEXT PRIMARY KEY,
 message_id TEXT NOT NULL REFERENCES inbox_messages(id),
 action TEXT NOT NULL,
 reason TEXT NOT NULL,
 task_id TEXT REFERENCES tasks(id),
 applied_client_id TEXT NOT NULL,
 applied_role TEXT NOT NULL CHECK(applied_role = 'coordinator'),
 applied_at TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 terminal INTEGER NOT NULL CHECK(terminal IN (0,1)),
 CHECK(action != 'createTask' OR (terminal = 1 AND task_id IS NOT NULL))
);
-- Future nonterminal history may coexist with the one terminal outcome.
CREATE UNIQUE INDEX one_terminal_inbox_resolution ON inbox_resolutions(message_id) WHERE terminal = 1;
-- +goose Down
DROP TABLE inbox_resolutions;
DROP TABLE task_events;
DROP TABLE task_creation;
ALTER TABLE workspace_counters DROP COLUMN resolution_sequence;
ALTER TABLE workspace_counters DROP COLUMN event_sequence;
ALTER TABLE workspace_counters DROP COLUMN task_sequence;
