# Personal Desk desktop

Personal Desk opens a genuine empty SQLite workspace in the adopted React design. An authenticated worker or coordinator can durably receive immutable Inbox messages; the read-only native Inbox and HTTP details use the same committed query. A separate coordinator command creates one durable request task, its timeline event and a confirmed Inbox resolution atomically. Receiving never creates a task. A coordinator can explicitly upload plain text or Markdown and associate it with an existing task; native details and authenticated HTTP return the same confined saved content. There are no provider requests, collectors, agents or production fixtures.

Targets: macOS 15+ arm64 and Windows 11 25H2+ amd64. Windows cross-compilation and CI build coverage are separate from native Windows acceptance, which is pending. No installer or release distribution is included.

## Build and checks

Use Go 1.27.3, Node 24.22.2, and the Wails 3.0.0 CLI (`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0`). Native macOS builds require Xcode command-line tools; Windows requires WebView2. Dependencies are locked in go.sum/package-lock.json. No remote assets are used at runtime.

From the repository root, with these tools on PATH:

```sh
wails3 generate bindings -f '-tags bindings' -ts -i
cd frontend
npm ci
npm run typecheck
npm test
npm run build
cd ..
export MACOSX_DEPLOYMENT_TARGET=15.0 # macOS native deployment target
export CGO_CFLAGS=-mmacosx-version-min=15.0
export CGO_LDFLAGS=-mmacosx-version-min=15.0
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
go build -tags production -trimpath -o bin/personal-desk .
sh scripts/package-mac.sh
```

`wails3 task build` runs binding generation, deterministic frontend checks/build and native compilation. `wails3 task package:mac` creates a local ad hoc signed app bundle. `scripts/check.sh` repeats the full Mac source check. `bindings` is a generation-only build tag that avoids requiring dist before the first frontend build; every normal native binary embeds the built frontend. Keep the generated bindings in source control. On macOS, discovery advertises API revision 2 with `workspace.read`, `tasks.read`, `inbox.read`, `inbox.receive`, `inbox.resolve.createTask`, `materials.read` and `materials.upload`. Existing revision 1 schemas/examples remain unchanged.

## Isolated demo

After building and packaging the normal app, run the integrated E1 demonstration
with Python 3.9+ (standard library only):

```sh
python3 scripts/test_demo_mac.py
python3 scripts/demo-mac.py
python3 scripts/demo-mac.py --inspect
```

These commands run from the repository root; an absolute script path works from any cwd.
The runner locates `bin/Personal Desk.app` beside its source tree and always
creates a fresh private synthetic workspace. One workspace proves empty → worker
receive with zero tasks → separate coordinator creation → saved board/Inbox/
details/Unicode material → clean restart and exact original-response replay.
Live task/workspace revisions remain distinct from the original receipts.

It prints only its retained evidence path/status. The private evidence root
contains an intact `workspace/`, an `exports/` destination and `report.json` with
source/dirty-state/executable identities, automated assertions, response
status/length/digests and canonical SQL/receipt/credential/blob fingerprints.
Bodies and credentials are never logged; raw response bytes stay in memory for
replay comparisons. Workspace credentials and synthetic originals remain private
files, so share the reviewed report rather than the workspace. Failure or Ctrl-C/
SIGTERM reports honest failure/interruption, retains evidence and cleans the
runner's own app/canary. There is no caller-workspace option.

`--inspect` leaves the restarted populated app and GET/HEAD source/image canary
alive until native Quit. Check pending and applied Inbox originals, board card
New/Other and type filters, Content/History/Sources/linked Inbox and Unicode
material, then refresh and repeat at normal/narrow window widths. Use actual
Help → Export diagnostics and Save to a new file in the printed `exports/`
destination. Avoid opening the synthetic URL during passive checks. Native Quit
lets the runner compare all saved facts/receipts/credentials/blobs, verify a clean
exit/discovery removal/listener closure and zero canary fetches. Human visual and
Help/Save observations require a separate review record; automated `passed`
never substitutes for them. The [E1 acceptance index](docs/e1-acceptance.md)
maps all 17 requirements/six Done when conditions to executed, reused or pending
evidence. This is Mac development acceptance; installed/manual replacement,
Windows native verification and distribution signing remain open. No distribution
candidate is created. E2–E4 coordination/preparation/review functions stay future.

Existing focused smokes remain independent checks, each with its own workspace:

```sh
python3 scripts/smoke-mac.py
python3 scripts/smoke-inbox-mac.py
python3 scripts/smoke-create-task-mac.py
python3 scripts/smoke-task-details-mac.py
python3 scripts/smoke-material-mac.py
python3 scripts/smoke-recovery-mac.py
python3 scripts/smoke-diagnostics-mac.py
sh scripts/package-diagnostic-smoke-mac.sh
python3 scripts/smoke-diagnostic-export-mac.py
```

The scripts create private temporary workspaces and launch the actual native bundle. The Inbox smoke receives two synthetic messages through Python's neutral HTTP client, proves exact-key retry, both independent conflicts, first-submitter provenance, passive reads and restart with an empty task board. The create-task smoke submits a separate coordinator decision, races exact retries, checks conflicts and saved task/Inbox facts, and restarts before replay. The task-detail smoke compares saved full facts for all three roles, exercises safe read failures, verifies canonical state across restart and observes zero canary source/image fetches. [Task details verification](docs/task-details-verification.md) records its named matrix and native design evidence. The material smoke uploads exact Unicode text, checks every read role and replay/conflict boundary, restarts, proves known missing content stays explicit, and compares passive canonical hashes with a zero-fetch canary. [Material verification](docs/material-verification.md) records the matrix and limitations. None prints a token; each retains its synthetic workspace for inspection. [Task creation verification](docs/task-creation-verification.md) records its executed matrix. [Inbox verification](docs/inbox-verification.md) records the executed matrix and native visual evidence. For manual board/Inbox/filter/narrow-window checks, launch the bundle executable with an absolute fresh private root:

```sh
PERSONAL_DESK_WORKSPACE="/absolute/private/demo-workspace" ./bin/Personal\ Desk.app/Contents/MacOS/personal-desk
```

The override's parent must already exist. A bad override fails explicitly. The default is `~/Library/Application Support/PersonalDesk` on macOS and `%LOCALAPPDATA%/PersonalDesk` on Windows. App files stay separate from data. The default API is exactly `127.0.0.1:8765`; an occupied endpoint fails. `PERSONAL_DESK_ADDRESS` is an explicit isolated-test override (`127.0.0.1:0` requests a temporary test endpoint). No fallback endpoint is selected implicitly.

`discovery.json` supplies endpoint/version only. Obtain a role token locally from the owner-private `credentials.json` and send `Authorization: Bearer <token>` in a header. Never paste tokens into URLs, logs or source. All three roles can query this slice; only worker/coordinator can POST `/api/v1/inbox/messages`. Only a coordinator can POST `/api/v1/inbox/messages/:id/resolutions` with the [frozen createTask input](internal/contracts/v1/examples/create-task.json). On macOS, only a coordinator can POST `/api/v1/tasks/:id/materials` with [upload input](internal/contracts/v1/examples/upload-material.json). GET `/api/v1/materials/:id` accepts all authenticated roles. Every mutation requires an `Idempotency-Key` header. Host must match the bound address exactly and browser Origin headers are rejected. Read the [frozen E1 wire inventory and enabled surface](docs/contracts.md) for response schemas and examples.

A second process for the same workspace shows `workspace_owned` without touching the owner's database or credentials. Quit stops the API, removes discovery, closes SQLite, drains diagnostics and writes its final clean-session marker before releasing the stable OS lock. A failed native database startup retains diagnostic ownership until its error window quits; domain views/HTTP stay unavailable. Quit that shell before running offline maintenance. A prior incomplete session is reported as an interruption without asserting its cause. Unknown/newer/incomplete database histories produce `database_unsupported`; there is no reset or upgrade bypass. Supported forward upgrades require a verified consistent backup before DDL. Failed steps retain the last committed schema and the original recovery point. An active restore blocks database readiness with `recovery_required`.

The same executable supports offline `maintenance backup`, `list-backups`, `restore <backupId>` and `resume <restoreId>` commands. They accept registered IDs, acquire exclusive workspace ownership and exit without starting the app or reapplying migrations. Follow the [exact recovery procedure](docs/recovery.md). `python3 scripts/smoke-recovery-mac.py` exercises populated backup/restore and interrupted-completion recovery in a new synthetic workspace.

## Ownership and extension

`internal/application/intake` owns the receive and coordinator create-task workflows and their separate typed transaction ports. `internal/application/tasks` constructs a new request task with pure rules. One bounded writer gate serializes both workflows. Creation commits task snapshot/creation facts, immutable event, terminal resolution, message revision, persistent counters, workspace revision and the complete original success receipt in one transaction. Its versioned canonical hash includes normalized client input and the target, excluding authenticated/server fields. Exact retries replay the original status/result; matching source/scope/messageId under a new key returns 200 and retains the first authenticated submitter without changing the domain revision. Committed changes publish only a revision after commit; callback failures cannot reverse success.

`internal/application/queries` owns small storage ports and transport-independent models. `internal/adapters/sqlite` implements one saved-state read transaction with a bounded writer pool and separate genuinely read-only reader pool. Its per-connection pragmas enforce foreign keys, busy timeouts and query-only behavior. Embedded transactional Goose SQL owns schema history. Persisted workspace identity/revision are independent of that history.

`internal/bootstrap` alone wires storage and HTTP, owns private file/ACL checks and the workspace lock, gates readiness, and closes resources from the native shutdown hook. POSIX uses no-follow regular owner-only files and flock; Windows uses protected user ACLs and LockFileEx. `internal/adapters/httpapi` and `wailsread` sanitize faults at their own boundary. The saved-state Wails service exposes exactly `GetWorkspaceSnapshot`, `GetTaskDetails`, `GetInboxMessage` and `ReadMaterial` and returns typed envelopes so framework outer errors cannot leak raw panic/error values. No SQL or lifecycle API is bound to JavaScript.

The frontend uses the aligned design CSS and v1 system-font tokens. Its adapter subscribes before initial read, coalesces invalidations, discards stale snapshots/details and preserves same-selection saved state on errors. Selection generations prevent a delayed result from appearing under another task or message. Board cards and confirmed Inbox links open the adopted task drawer with Content, History and Sources tabs; related-message navigation selects the exact saved Inbox ID under the all filter. The drawer traps/returns focus and supports Escape and keyboard tabs. Original bodies and materials are escaped text, including Markdown syntax. Valid HTTP(S) URLs have explicit user-activated links through a separate native navigation allowlist; paths, credentials, controls and unsupported or ambiguous URLs remain inert. No image, preview or provider request is created. Uploaded materials are selected on demand and have independent generation/revision checks; same-selection failures preserve saved text with explicit retry. Refresh reads saved state. A root rendering boundary gives a copyable correlated fallback; global runtime/promise hooks and bridge failures use a bounded fixed-metadata reporter. A separate DiagnosticFacade exposes bounded frontend reports, technical status and no-argument `ExportDiagnostics`, with no domain or caller-path authority. The single allowlisted diagnostic pipeline retains a 128-record private ring and drains a 128-record queue through one owned writer. Logs live outside SQLite in private `logs/`, with a 5 MiB active file and four fixed backups. Events are limited to 16 KiB; frontend reports to 4 KiB, 20/minute and one matching code/component per 60 seconds. Sink errors/panics and queue overflow mark diagnostics degraded and cannot change committed results. The framework logger drops free-form messages/attributes. [Diagnostic verification](docs/diagnostics-verification.md) records ownership, privacy and failure evidence. The native Help → Export diagnostics action remains available during React/database failure and uses a native Save dialog. It creates a new owner-private ZIP outside configured/default workspace and application folders, refuses replacement/aliases, and reports cancellation normally. The archive contains exactly `manifest.json`, revalidated/serialized `logs/diagnostics.jsonl` and blank `reproduction-steps.txt`, with a 30 MiB total uncompressed budget. Export uses cached schema facts and never opens SQLite, materials, credentials or original files. [Export verification](docs/diagnostic-export-verification.md) records coverage/lifetime/destination checks.

The adopted decisions and design are in the parent planning workspace's `initiative-personal-desk-desktop` architecture/design/quality documents. This repository implements the approved desktop foundation, schema recovery, shared E1 contracts, durable Inbox receipt/inspection and atomic coordinator task creation. The canonical embedded schemas and typed inventories cover planned reads/mutations/materials/diagnostic formats, with strict decoding, authenticated principals and a tested role-operation policy. Saved task details use one committed read transaction and preserve genuine creation/provenance/history/material coordinates through restart. Legacy summaries without saved creation facts remain on the board and return `service_unavailable` for details. Text material uploads/reads and explicit safe browser navigation are implemented on macOS. Private native diagnostic ZIP export is implemented on macOS. Migrations2/3/4 are additive; original v1 rows/revisions survive a verified pre-upgrade backup. Because v1 never stored message bodies or submitter provenance, their original details remain explicitly unavailable and source-identity redelivery conservatively conflicts. [Contract verification](docs/contract-verification.md) records executable schema/security and Mac evidence. [Recovery verification](docs/recovery-verification.md) records Mac evidence; Windows native acceptance remains pending.

Explicit material provisioning is API-only: no UI upload/write command, directory discovery or coordinate-to-file conversion exists. Migration 4 adds an immutable attachment registry. The workflow checks authorization, exact replay, genuine task facts, expected revision, safe revision ceilings and the combined 32-association limit before writing bytes. The materials adapter opens every ancestor without following links, retains a private directory handle, writes/fsyncs private staging content, publishes a SHA-256 blob without replacement, syncs the directory and only then commits the registry/task/workspace revision and receipt. A failure can leave an unreachable staging/blob file; no read scans for it, and automatic cleanup is outside this slice. Reuse checks the opened regular file, link count, size and digest. Retained backups may still reference blobs, so rollback never deletes published content.

Materials accept `text/plain` and `text/markdown`, exact valid UTF-8 and up to 262144 Unicode characters, subject also to the existing 1 MiB JSON request limit. Empty text is valid. NUL/binary controls are rejected except tab/CR/LF. Missing, unsupported/digest-mismatched and excessive content return explicit unavailable states with null text/media type. Arbitrary paths, symlinks, hardlinks, Finder aliases and nonregular files are never readable materials. The confined adapter is implemented and verified on macOS; unsupported platforms keep existing workspace reads and disable material provisioning/capabilities. Windows material upload/read is currently disabled, and Windows native behavior remains unverified. A follow-up must implement confined Windows directory/file handles with reparse, hardlink, shortcut/alias, ACL and replacement-race checks, then run the full Windows material/native-navigation matrix before advertising the capabilities. The package is locally ad hoc signed, with no distribution signing or push.

## Diagnostic fault fixture

The separate native React fixture reuses the production reporter, notices and render boundary. Its fault buttons and synthetic secret canaries belong to a separate asset entry point and are absent from the production asset graph. It adds no production fault API or environment switch:

```sh
sh scripts/package-diagnostic-smoke-mac.sh
PERSONAL_DESK_WORKSPACE="/absolute/private/synthetic-workspace" PERSONAL_DESK_ADDRESS="127.0.0.1:0" ./bin/Personal\ Desk\ Diagnostic\ Fixture.app/Contents/MacOS/personal-desk
```

Use its Runtime fault, Promise fault and Render fault buttons, then locate the displayed opaque ID in `logs/diagnostics.jsonl`. Reports contain no thrown value, browser stack, props or business text. Unknown source frames remain empty. Normal build/package commands continue to produce `Personal Desk.app` independently. Diagnostic capture cannot recover native crashes, fatal runtime failures or power loss; shutdown still depends on the OS completing filesystem operations. Windows persistent diagnostics are disabled until an anchored adapter and native verification are available; bounded process-private capture, frontend reports and technical status remain available. Windows native acceptance remains unverified. Windows export destination access also fails closed until an anchored adapter and native verification are available.


The export smoke uses the separate diagnostic fixture bundle with a build-tag-only picker result injection and automatic React failure. Those environment switches and fault controls are absent from the production binary/assets. It validates every exported manifest/log record against the unchanged frozen schemas, inspects the three entries/budgets/private modes, scans synthetic excluded markers/tokens and compares all canonical SQL facts, receipts and credential bytes. Real native Help/Save interaction is checked separately from the fixture injection. Export history is bounded; omissions, unreadable files, interrupted records and unknown prior loss set incomplete coverage. Quit cancels new file work, ignores late Save results and drains accepted snapshots/writes before releasing ownership.
