# E1 v1 contracts and restricted callers

The canonical executable schema set is [internal/contracts/v1/schemas](../internal/contracts/v1/schemas/), embedded with its [synthetic examples](../internal/contracts/v1/examples/). `v1.schema.json` owns the definitions; named schemas reference those definitions. The three original schema paths and `docs/examples` are symbolic links to the canonical files. There are no independently maintained copies. `v1.Validate` uses the pinned `github.com/santhosh-tekuri/jsonschema/v6 v6.0.3`, asserted formats, precision-preserving numbers and an embedded-only loader. The strict parser, schema evaluation, header cardinality and distinct-token check together form the boundary.

Enabled routes are `GET /api/v1`, `GET /api/v1/workspace`, `GET /api/v1/tasks/:id`, `GET /api/v1/inbox/messages/:id`, `POST /api/v1/inbox/messages` and coordinator-only `POST /api/v1/inbox/messages/:id/resolutions`. macOS additionally enables coordinator-only `POST /api/v1/tasks/:id/materials` and all-role `GET /api/v1/materials/:id`, with `apiVersion: v1`, additive `apiRevision: 2` discovery and the original request/response limits. The saved-state Wails facade binds exactly `GetWorkspaceSnapshot`, `GetTaskDetails`, `GetInboxMessage` and `ReadMaterial`; a separate navigation facade accepts only validated HTTP(S) URLs. A separate DiagnosticFacade enables bounded frontend fault reports, technical status and no-argument `ExportDiagnostics` through trusted native Save, with process-private diagnostics on both targets and private persistent diagnostics on macOS. Windows persistent logging stays disabled pending an anchored adapter and native verification. The native maintenance action is available without React/database readiness. It returns frozen exported/cancelled/failed outcomes, accepts no destination/file-list arguments and exports only a bounded revalidated technical snapshot. Export destination access also remains disabled on Windows pending an anchored adapter/native verification. No diagnostic HTTP route is enabled. Disabled action routes return `not_found` after confinement/authentication, including for a coordinator.

## Caller setup and operation policy

Bootstrap creates three distinct random 64-character lowercase hexadecimal tokens in owner-private `credentials.json`. Existing valid bytes and tokens survive restart. Setup rejects unsafe permissions/ACLs, links, unknown or case-aliased properties, duplicate/escaped duplicate keys, missing/null/invalid/equal tokens, invalid UTF-8, trailing JSON and files over 4096 bytes including whitespace tails. Rejection does not reset or rewrite the file. `discovery.json` contains endpoint/version only. Existing workspace ownership and private-file checks remain in force.

Use one `Authorization: Bearer <token>` header. Never put a token in a URL, example, log or source file. Authentication produces a server-derived principal with `clientId: client-<32 lowercase hex>` and its credential's role. Identity is a namespaced SHA-256-derived opaque credential identifier; it stays stable while that token stays stable. It identifies a local credential, not a verified person or provider. The principal survives the HTTP application context. The authenticated submitter is separate from payload-claimed source/sender; applying a report retains that submitter and separately records the coordinator.

| Operation | reader | worker | coordinator | Enabled now |
|---|---|---|---|---|
| Discovery / workspace.read | yes | yes | yes | yes |
| inbox.read | yes | yes | yes | yes |
| tasks.read | yes | yes | yes | yes |
| materials.read | yes | yes | yes | macOS |
| materials.upload | no | no | yes | macOS |
| inbox.receive | no | yes | yes | yes |
| inbox.resolve.createTask | no | no | yes | yes |
| Any other operation/provider action | no | no | no | no |

Authorization precedes every command workflow and every receipt lookup. The pure operation policy is tested independently of route availability; it does not install future workflows. The trusted native UI uses its query facade rather than an HTTP authentication bypass.

## HTTP envelope and limits

Bind only literal `127.0.0.1`, require the exact bound Host and reject every Origin header, including empty or repeated values. Reject duplicate Authorization, media/version headers, invalid bearer syntax and query-string parameters. There are no CORS permissions. Each attempt gets a fresh backend-generated `X-Request-ID`; caller correlation headers have no authority. Replies are bounded UTF-8 JSON, `no-store` and `nosniff`, with fixed public failure text.

The major version is `/api/v1`. Optional `X-API-Version` must be a single `v1`; a different major `/api/v…` path or invalid/duplicate version header returns `unsupported_api_version`. GET has no body or Idempotency-Key; a nonempty body returns 400, and a body over 1 MiB returns 413. If supplied on a read, Content-Type must be a single supported JSON media type. Mutations require exactly one bounded Idempotency-Key and one Content-Type (`application/json` or `application/json; charset=utf-8`). All mutation routes use the shared mutation-header validator before decoding or entering the workflow. Missing version header is valid because the path already selects v1.

| Budget | Limit |
|---|---|
| Request / metadata response | 1,048,576 / 4,194,304 bytes |
| HTTP server configured header budget | 16,384 bytes |
| Bearer token / Idempotency-Key | 64 lowercase hex / 1–128 ASCII `[A-Za-z0-9][A-Za-z0-9._:-]*` |
| Credential file / JSON nesting | 4096 bytes including all tails / depth 32 |
| IDs / source / scope / claimed messageId | 128 / 64 / 128 / 256 characters |
| Title / summary / nextAction / nextActor / sender | 256 / 4096 / 2048 / 256 / 256 characters |
| Message text / reason | 65,536 / 4096 characters |
| URL / inert coordinate / artifact label | 2048 / 1024 / 256 characters |
| Workspace tasks / Inbox / source-health rows | 10,000 / 10,000 / 256 |
| Artifacts or task materials / task timeline | 32 / 1000 |
| Material text | 262,144 characters, still subject to the response byte cap |

Scalar limits are Unicode character limits; byte limits include the entire encoded input. Numeric tokens use decimal integer syntax, without fraction/exponent forms, and at most 17 characters before schema checks. All revisions are positive safe JSON integers, at most 9007199254740991; counts are nonnegative safe integers. Times are valid UTC RFC3339 instants with literal `Z` and at most nine fractional-second digits. Unknown/case-aliased fields, duplicate keys at every depth, invalid UTF-8, trailing JSON, oversized whitespace tails, invalid enums/times/revisions and excessive nesting fail before typed inputs escape. Decode failures return a zero typed value and fixed errors, without reflected input. Saved workspace responses are also schema-checked at both transport boundaries; invalid saved representations become safe internal faults.

## Frozen field inventory

All query optionals are present with `null`; empty collections serialize as `[]`. Required values reject null. Optional technical event fields and optional error metadata are omitted when unavailable, as explicitly stated below.

| Schema / application model | Fields |
|---|---|
| WorkspaceSnapshot | workspaceId, workspaceRevision, createdAt, tasks, inbox, sources, counts |
| TaskSummary | id, kind, status, title, summary?, nextAction?, nextActor?, revision, updatedAt |
| InboxMetadata | id, source, scope, kind, status, sender?, occurredAt?, receivedAt, revision |
| SourceHealth | source, scope, status, lastAttemptAt?, lastSuccessAt? |
| Counts | tasks, inbox, unprocessedInbox, needsAttention |
| TaskDetails | workspaceRevision, task; task extends TaskSummary with createdAt, provenance?, timeline, materials |
| InboxDetails | workspaceRevision, message; message extends InboxMetadata with messageId, text, sourceUrl?, artifacts, submittedBy, proposedTaskRef?, resolution? |
| TaskRef / Principal | taskId, revision / clientId, role |
| Provenance | messageId (local Inbox ID), submittedBy (authenticated principal) |
| CreateTask resolution | id, action, reason, taskId (confirmed link), appliedBy (coordinator principal), appliedAt |
| Task event | id, kind=`task.created`, at, reason, provenance, appliedBy |
| Artifact coordinate | kind (`file`, `url`, `reference`), coordinate, label? |
| Material association | coordinate, materialId?, availability, unavailableReason? |
| Material result | workspaceRevision, material: id, availability, mediaType?, text?, unavailableReason? |
| Receive result / createTask result | workspaceRevision, message / workspaceRevision, message, task |

Question marks in this table mean explicit nullable query fields. Task kind `task_analysis` maps to board category `development`, `code_review` to `review`, and `request` to `other`. Kind/category are separate from status (`new`, `preparing`, `executing`, `review`, `waiting`, `done`). Message kinds are `user_request`, `external_signal`, `agent_report`; statuses are `pending`, `needs_attention`, `applied`, `ignored`. Source-health statuses preserve migration1's `ready`, `error`, `unavailable`, `not_configured` vocabulary and represent saved reported health.

`proposedTaskRef` carries no confirmation; only a saved resolution's `taskId` is an applied link. Its applying principal must be a coordinator. Receipt time is independent of claimed occurredAt. Results describe local saved state/commit only, never independently verified provider success.

Coordinates are inert metadata, even if their text resembles a private path. They never authorize a file open. Receive artifacts cannot supply material IDs. Associations receive server-owned opaque material IDs; `not_imported` has null ID, while an unavailable known material may retain its ID. Available associations have an ID and null reason; unavailable associations have a reason (`not_imported`, `missing`, `unsupported`, `too_large`). Material reads use an opaque ID, with available text/plain or text/markdown content, or explicit unavailable state (`missing`, `unsupported`, `too_large`) and null content/media type. macOS material access uses only the server-owned registry and confined blob handles. It excludes database, credentials, backups, logs and diagnostic service paths and rejects traversal, symlinks, hardlinks, aliases and nonregular files. Only validated HTTP(S) URLs may become user-activated links; no implicit fetch, preview or remote image load is authorized.

## Creation inputs and presence

| Input | Required | Optional and normalization |
|---|---|---|
| Receive | source, scope, messageId, kind, nonblank text | sender, occurredAt, sourceUrl, taskRef: omitted or null → nil; artifacts: omitted → empty array, null rejected |
| CreateTask | expectedMessageRevision, action=`createTask`, nonblank reason, task | task requires kind=`request`, nonblank title; summary, nextAction, nextActor: omitted or null → nil |

`Presence[T]` retains omitted/null/value until the explicit creation-to-application mapping. It round-trips omitted and explicit null fields separately. Creation normalization does not invent actor/action/summary values. Later updates must retain omitted=preserve and may interpret null=clear only for documented nullable fields. Authenticated principal fields are supplied by the server mapping and cannot be injected into a payload.

The createTask workflow requires a pending user_request/external_signal without a proposed task association. Agent reports and proposed associations return `unsupported_operation`; missing messages return `not_found`. A different-key terminal request returns `terminal_resolution` before revision checking. Stale pending input returns `revision_conflict` with currentRevision. Idempotency is scoped by authenticated clientId + operation + key. The versioned canonical hash includes the target and typed normalized payload. Authorization precedes replay, and a committed replay precedes current entity reads/revision validation. Receive and create operations have independent key scopes.

Creation commits the request/new task at revision1, genuine UTC creation/update times, `task.created` reason/provenance, copied inert associations, confirmed resolution, applied message revision+1, workspace revision+1 and complete original success receipt together. Optional task values stay null. Only a fresh commit publishes the revision. Exact retries retain the original 201/result across later changes and restart; callback failures cannot reverse success. A receive retry still returns its original pending result, while identical source-identity redelivery under a new key returns the current applied message/resolution and original submitter.

## Public errors and native equivalence

Every API error has `{error:{code,message,retryable,requestId}}`. Only `internal_error` includes generated `errorId`. Only `revision_conflict` includes a valid currentRevision. Expected validation/auth/conflict outcomes are not diagnostic faults. Unknown application errors or invalid conflict metadata become captured internal faults. Public mapping ignores arbitrary underlying message strings and uses the fixed catalog. HTTP and Wails preserve the same semantic fields and response budget; neither leaks SQL, stack, paths, payloads or credentials.

| HTTP | Codes |
|---|---|
| 400 | invalid_request, unsupported_api_version, unsupported_operation |
| 401 / 403 / 404 | unauthorized / forbidden / not_found |
| 409 | revision_conflict, idempotency_conflict, message_identity_conflict, terminal_resolution |
| 413 / 415 | payload_too_large / unsupported_media_type |
| 500 | internal_error |
| 503 | service_unavailable, response_too_large |

503 outcomes are retryable; all other catalog outcomes are not. Native query results remain `{data: Snapshot|null,error: Failure|null}` with exactly one non-null field. Native errorId/currentRevision are explicit null for unavailable metadata, with equivalent generated attempt/fault IDs. Startup-only native codes include `workspace_unsafe`, `workspace_owned`, `database_unavailable`, `database_unsupported`, `credentials_unsafe`, `endpoint_invalid`, `endpoint_occupied`, `discovery_unsafe`, `catalog_invalid`, `recovery_required`, and `service_unavailable`. They gate normal reads/API while preserving the native maintenance shell.

## Diagnostic, frontend and archive contracts

Schema version 1 preserves the fallback's `at`, `code`, `operation` and optional generated requestId/errorId, adding required schemaVersion/level/component. Optional sessionId, durationMs, appVersion, buildVersion, errorClass and sanitized frames are allowlisted. The active pipeline retains 128 process-private events and uses the frozen technical catalog; Wails faults retain their own component. On macOS, one owned writer drains a bounded 128-record queue to private rotating logs outside SQLite, with a final session marker and drain before workspace unlock. Framework free-form messages/attributes are discarded. Unknown values and arbitrary messages/paths are rejected. Rotation, persistent capture, the narrow frontend report/status facade and explicit private ZIP export are implemented on macOS. Help → Export diagnostics… and the no-argument DiagnosticFacade method use native Save independently of React/database readiness. Windows retains the bounded process-private fallback and report/status service; persistent logging and export destination access stay disabled pending anchored adapters and native verification.

The full catalog reserves codes `app.start`, `app.stop`, `db.init`, `migration.step`, `api.operation`, `query.fault`, `framework.event`, `ui.render_fault`, `ui.runtime_fault`, `ui.promise_fault`. Components and operations are fixed schema enums. Frontend reports contain only schemaVersion, one ui fault code, a component (`root`, `desk`, `board`, `inbox`, `task_drawer`, `read_adapter`) and sanitized frontend frames; they carry no authoritative IDs or arbitrary exception values.

| Frame location | Known source/function family |
|---|---|
| bootstrap.start | internal/bootstrap/workspace.go, Start/startCatalog |
| queries.workspace | internal/application/queries/workspace.go, GetWorkspaceSnapshot |
| httpapi.serve | internal/adapters/httpapi/handler.go, ServeHTTP |
| wailsread.workspace | internal/adapters/wailsread/facade.go, GetWorkspaceSnapshot |
| sqlite.open / sqlite.workspace | internal/adapters/sqlite/migrate.go / store.go, OpenCatalog / LoadWorkspace |
| frontend.render / frontend.runtime / frontend.promise | frontend/src/main.tsx, controlled rendering/runtime/promise boundaries |

A frame carries a catalog location plus line 1–1,000,000; callers cannot provide a raw path/function/stack. Only frontend locations are accepted in frontend reports. The capture adapter maps known application source locations to this catalog; unknown frontend source locations remain empty. Export revalidates these records against the same frozen allowlist.

| Technical budget | Frozen policy |
|---|---|
| Complete event / frontend report | 16 KiB / 4096 bytes |
| Frames / frontend rate / repeat window | 16 / 20 reports per minute / 60 seconds |
| Active log / retained backups | 5 MiB / four (roughly 25 MiB after cleanup) |
| Archive uncompressed / source log files | 30 MiB / five |
| Fixed ZIP entries | manifest.json, logs/diagnostics.jsonl, reproduction-steps.txt |

The technical manifest includes schemaVersion, createdAt, bounded app/build/OS/architecture/runtime versions, cached schema status/version, selected session/error IDs, shared diagnostic availability and coverage. Unknown values stay null/`unknown`; it requires no database query. Coverage contains complete, acceptedRecords, omittedRecords, unavailableFiles and truncatedRecords; complete cannot be true with omissions/unavailable/truncated records. Export result is exported/cancelled/failed with explicit nullable coverage/errorId; cancellation has neither. No result accepts a destination path. The exporter reparses complete records through this versioned allowlist, omits invalid/unknown/oversized records with honest coverage, uses only the three fixed regular private entries, and never scans arbitrary workspace files. A new owner-private destination must be outside configured/default workspace and application folders; replacement, links and aliases are refused. Delivery identities prevent duplicating persisted events from private memory; failed appends are ambiguous and count as omitted. All shared service writes and accepted snapshot/drain work retain existing workspace-lock ownership rules. See [diagnostic export verification](diagnostic-export-verification.md) for executed Mac evidence and limits.

## Persistence and evidence

Released migration1 remains immutable and the Goose ledger remains the only schema authority. The implemented story2 protocol supports recognized forward prefixes with a verified SQLite-consistent pre-upgrade backup, retains recovery points and committed steps on failure, and blocks unsafe/interrupted replacement until controlled offline recovery. There is no reset, downgrade, skip or force bypass. See [recovery](recovery.md) and [contract verification](contract-verification.md) for actual target evidence. Windows native acceptance remains unverified.


## Durable Inbox persistence

`intake.ReceiveService` owns transaction orchestration; its consuming ports are implemented only by the SQLite adapter. One writer gate and 2.5-second shared gate/transaction deadline bound receive work. Caller/workspace cancellation rolls back unfinished work. A fresh message stores immutable original fields, credential provenance, transactional receipt sequence, workspace revision and the complete original result/status receipt atomically. The versioned operation/target/input hash normalizes omitted nullable creation fields and an omitted artifacts collection, preserves text/time/order, and excludes submitter/server fields.

Exact client/operation/key retries return the original committed result/status, including its original workspace revision, after restart. Changed input conflicts. An identical source/scope/messageId delivered under another key/client returns 200 with the current committed message and first submitter; it can add a receipt but changes no domain counter/revision/event. A changed original conflicts. Only a fresh committed message publishes `workspace.changed` with its revision; rollback/replay/identity duplicates do not publish. Publication faults cannot reverse commit success.

Inbox metadata omits original bodies and coordinates and orders fresh receipts by transactional receipt sequence, independent of clock reversal/ties. Detail queries use a single committed read transaction and include that snapshot's revision. Passive reads write no receipt or domain fact. Migration2 preserves released migration1 and all legacy rows/revisions; legacy bodies/provenance were never stored, so detail returns `service_unavailable` and legacy source-identity redelivery conservatively returns `message_identity_conflict`.

## Atomic coordinator creation persistence

Migration3 leaves released SQL1/2 unchanged. It adds bounded transactional task/event/resolution counters, separate saved task-creation facts, task events and append-only resolution history. A partial unique index allows one terminal resolution per message while permitting multiple future nonterminal history rows. Legacy task summaries receive no invented creation/history/provenance; legacy Inbox originals remain unavailable. Existing v1 and independent released-v2 upgrade fixtures prove verified backups and preservation of originals, entity/workspace revisions and receive receipts. Task-detail queries read the revision, snapshot, creation facts, materials and deterministically ordered events (`at`, then `id`) in one read transaction. Missing tasks return `not_found`; legacy summaries without creation facts return `service_unavailable` and remain in board snapshots. Invalid saved or oversized representations fail safely at both transport boundaries. See [task creation verification](task-creation-verification.md) and [task details verification](task-details-verification.md).

## Revision 2 material addition (macOS)

The immutable v1 schema set above is preserved byte-for-byte. Additive schemas are [discovery revision 2](../internal/contracts/v1/schemas/discovery-revision2.schema.json), [upload request](../internal/contracts/v1/schemas/upload-material.schema.json), and [upload result](../internal/contracts/v1/schemas/upload-material-result.schema.json), each with an example beside the existing examples. Revision 2 references the original Limits definitions and advertises materials.read/materials.upload only when the confined adapter and upload workflow are wired.

Coordinator POST /api/v1/tasks/:id/materials accepts expectedTaskRevision, mediaType, exact text and optional nullable label. All authenticated roles GET /api/v1/materials/:id using the unchanged material.schema.json envelope. Upload is a separate idempotent operation: exact committed replay precedes current task/file checks. The original receive/create receipts, task_creation materials and timeline are never rewritten. Task details append registry associations by resulting task revision; coordinates are metadata, never file authority. Material unavailable responses preserve the committed lookup revision and return null text/mediaType.
