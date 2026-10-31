-- +goose Up
CREATE TABLE workspace_counters (
 singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
 message_sequence INTEGER NOT NULL CHECK(message_sequence BETWEEN 0 AND 9007199254740991)
);
INSERT INTO workspace_counters VALUES (1, 0);
-- Legacy metadata remains intact. Absence of a payload means the original was
-- never saved by v1; the reader cannot reconstruct its body or submitter.
CREATE TABLE inbox_payloads (
 message_id TEXT PRIMARY KEY REFERENCES inbox_messages(id),
 identity_hash TEXT NOT NULL,
 original_text TEXT NOT NULL,
 source_url TEXT,
 artifacts_json TEXT NOT NULL,
 proposed_task_json TEXT,
 submitted_client_id TEXT NOT NULL,
 submitted_role TEXT NOT NULL CHECK(submitted_role IN ('worker','coordinator')),
 receipt_sequence INTEGER NOT NULL UNIQUE CHECK(receipt_sequence BETWEEN 1 AND 9007199254740991)
);
CREATE TABLE mutation_receipts (
 client_id TEXT NOT NULL,
 operation TEXT NOT NULL,
 idempotency_key TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 response_status INTEGER NOT NULL CHECK(response_status IN (200,201)),
 response_json TEXT NOT NULL,
 PRIMARY KEY(client_id, operation, idempotency_key)
);
-- +goose Down
DROP TABLE mutation_receipts;
DROP TABLE inbox_payloads;
DROP TABLE workspace_counters;
