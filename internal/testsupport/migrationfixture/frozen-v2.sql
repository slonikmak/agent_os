-- Independent released v2 image; never built from the current catalog at test time.
-- Frozen released v1 fixture; never regenerated from the current catalog.
PRAGMA journal_mode=WAL;
CREATE TABLE goose_db_version (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 version_id INTEGER NOT NULL,
 is_applied INTEGER NOT NULL,
 tstamp TIMESTAMP DEFAULT (datetime('now'))
);
INSERT INTO goose_db_version(version_id,is_applied,tstamp) VALUES(0,1,'2026-10-29 00:00:00'),(1,1,'2026-10-29 00:00:00');
CREATE TABLE workspace_meta (
 singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
 workspace_id TEXT NOT NULL UNIQUE,
 workspace_revision INTEGER NOT NULL CHECK (workspace_revision BETWEEN 1 AND 9007199254740991),
 created_at TEXT NOT NULL
);
INSERT INTO workspace_meta VALUES (1, lower(hex(randomblob(16))), 1, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'));
CREATE TABLE tasks (
 id TEXT PRIMARY KEY,
 kind TEXT NOT NULL CHECK(kind IN ('task_analysis','code_review','request')),
 status TEXT NOT NULL CHECK(status IN ('new','preparing','executing','review','waiting','done')),
 title TEXT NOT NULL,
 summary TEXT,
 next_action TEXT,
 next_actor TEXT,
 revision INTEGER NOT NULL CHECK(revision BETWEEN 1 AND 9007199254740991),
 updated_at TEXT NOT NULL
);
CREATE TABLE inbox_messages (
 id TEXT PRIMARY KEY,
 source TEXT NOT NULL,
 scope TEXT NOT NULL,
 message_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('user_request','external_signal','agent_report')),
 status TEXT NOT NULL CHECK(status IN ('pending','needs_attention','applied','ignored')),
 sender TEXT,
 occurred_at TEXT,
 received_at TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision BETWEEN 1 AND 9007199254740991),
 UNIQUE(source, scope, message_id)
);
CREATE TABLE source_health (
 source TEXT NOT NULL,
 scope TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('ready','error','unavailable','not_configured')),
 last_attempt_at TEXT,
 last_success_at TEXT,
 PRIMARY KEY(source, scope)
);

UPDATE workspace_meta SET workspace_id='frozen-workspace',workspace_revision=42,created_at='2026-10-29T00:00:00Z';
INSERT INTO tasks VALUES('T-frozen','request','waiting','Saved synthetic task','Keep this fact','Review recovery',NULL,7,'2026-10-29T00:00:00Z');
INSERT INTO inbox_messages VALUES('I-frozen','manual','isolated','fixture-1','user_request','pending','Synthetic requester',NULL,'2026-10-29T00:00:00Z',3);
INSERT INTO source_health VALUES('manual','isolated','ready',NULL,NULL);
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

INSERT INTO goose_db_version(version_id,is_applied,tstamp) VALUES(2,1,'2026-10-29 01:00:00');
UPDATE workspace_meta SET workspace_revision=43;
UPDATE workspace_counters SET message_sequence=1;
INSERT INTO inbox_messages VALUES('I-released-v2','manual','synthetic','released-v2','external_signal','pending',NULL,NULL,'2026-10-29T01:00:00Z',1);
INSERT INTO inbox_payloads VALUES('I-released-v2','1cd7485da50f06fee757131f7166fdec5fe12ab8ae1c1aed3bb8568c5e8e837f','Saved released-v2 original',NULL,'[]',NULL,'client-11111111111111111111111111111111','worker',1);
INSERT INTO mutation_receipts VALUES('client-11111111111111111111111111111111','inbox.receive','released-v2','1cd7485da50f06fee757131f7166fdec5fe12ab8ae1c1aed3bb8568c5e8e837f',201,'{"WorkspaceRevision":43,"Message":{"ID":"I-released-v2","Source":"manual","Scope":"synthetic","Kind":"external_signal","Status":"pending","Sender":null,"OccurredAt":null,"ReceivedAt":"2026-10-29T01:00:00Z","Revision":1,"MessageID":"released-v2","Text":"Saved released-v2 original","SourceURL":null,"Artifacts":[],"SubmittedBy":{"ClientID":"client-11111111111111111111111111111111","Role":"worker"},"ProposedTaskRef":null,"Resolution":null}}');
