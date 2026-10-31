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
