# Personal Desk

**A local desktop workspace for turning incoming requests into traceable tasks.** Built with Go, Wails, React, TypeScript, and SQLite through an AI-assisted development process.

Personal Desk starts with a genuinely empty workspace. An authenticated local client can submit an Inbox message; a coordinator can then make a separate, explicit decision to create a task from that message. The desktop app shows the saved board, Inbox, task history, source links, and text materials. It reads committed local data rather than generating example records or asking an AI service for content.

> **Current scope:** This is a development-stage portfolio project. The desktop interface is read-only; writes use the local API. The complete native demonstration and material support are on macOS. Windows builds are configured, but native Windows acceptance, an installer, and a signed release are still open.

## Start here

- [Run the isolated Mac demo](#run-the-mac-demo) to see the complete request-to-task flow without touching a personal workspace.
- [Explore the architecture](#architecture) to see how the UI, application workflows, and storage fit together.
- [Read the API contract](docs/contracts.md) for roles, requests, responses, and error semantics.
- [Review the acceptance evidence](docs/e1-acceptance.md) for what was exercised and what remains open.

## What the application does

1. **Receive a message.** A worker or coordinator submits a request to the loopback HTTP API. The original message, source identity, submitter, and receipt are saved together. Receiving a message does not create a task.
2. **Decide what to do with it.** A coordinator can resolve an eligible pending message by creating one request task. The task, creation event, Inbox resolution, revisions, and response receipt commit in one SQLite transaction.
3. **Inspect saved work.** The Russian-language desktop UI shows a board grouped by status, filters by task type, and opens a task drawer with Content, History, and Sources tabs. The Inbox shows the original message, its provenance, and any confirmed task link. A Sources view shows saved source information.
4. **Attach text deliberately.** On macOS, a coordinator can upload plain text or Markdown to an existing task through the API. The UI reads the confined saved copy on demand. A URL or file coordinate by itself is only metadata; it is never fetched automatically.
5. **Recover and diagnose.** Offline maintenance commands manage verified database backups and explicit restore/resume. Bounded, privacy-conscious diagnostics can be exported from the native Help menu on macOS.

The app has no provider integration, background collector, agent execution, automatic task creation, or automatic source fetching. The UI does not currently offer task editing or material upload.

## Principles behind the design

| Principle | How it appears in this project |
| --- | --- |
| **Saved state is authoritative** | The UI reads committed SQLite snapshots. It does not infer success from a click, synthesize task details, or silently change Inbox status while reading. |
| **Intake and decision are separate** | Message receipt and coordinator task creation are different authorized commands with different transactions. |
| **Retries have defined results** | Mutations require an idempotency key. Exact retries replay the original committed response; changed input or stale revisions produce explicit conflicts. |
| **One owner, private local data** | A workspace lock prevents two app processes from writing the same workspace. Credentials, database files, materials, and logs are kept in private local storage. |
| **Untrusted content stays inert** | Message bodies and materials render as text. Links require a user action and pass a native URL allowlist; coordinates are not treated as file access authority. |
| **Failure remains inspectable** | Startup faults have bounded public error codes, unsupported database histories are refused, and an interrupted restore requires an explicit resume. Diagnostics exclude original content and credentials. |

## Architecture

```text
React / TypeScript desktop UI (read-only board, Inbox, task details)
                |
                | Wails typed read facade + workspace.changed revisions
                v
Go bootstrap and application services
    |           |                    |
    |           |                    +-- Queries: committed snapshots/details
    |           +-- Intake: receive message / create task
    +-- Materials: explicit text upload
                |
                v
SQLite adapter (transactions, receipts, schema migrations)
    + private material blobs + backup/recovery files

Authenticated loopback HTTP API ----> the same application services and queries
```

`internal/bootstrap` owns startup, the workspace lock, HTTP listener, readiness, and shutdown. `internal/application` contains transport-independent workflows and query interfaces. `internal/adapters` implements SQLite persistence, the HTTP API, native Wails reads, material confinement, and diagnostics. `internal/contracts/v1` holds versioned schemas and examples; `migrations` contains the embedded SQL catalog. `frontend/src` contains the React UI and its native read adapter.

Reads use a consistent saved-state transaction. Writes publish a workspace revision only after commit, so the frontend can refresh without treating an event as the data itself. The native Wails domain facade exposes four read methods; business mutations remain behind the authenticated HTTP API. [The contract inventory](docs/contracts.md) describes the enabled operations and their exact payloads.

**Stack:** Go 1.27.3 · Wails 3.0.0 · React 18 · TypeScript · Vite · SQLite (modernc driver) · Goose migrations. Dependencies are pinned in `go.mod`, `go.sum`, and `frontend/package-lock.json`.

## Build and checks

The native target is macOS 15+ on Apple silicon. Install Go 1.27.3, Node.js 24.22.2, Xcode command-line tools, and the Wails CLI, then run from the repository root:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0
wails3 task package:mac
open "bin/Personal Desk.app"
```

The bundle is locally ad hoc signed for development. It is not a distribution release. A fresh launch creates an empty workspace at `~/Library/Application Support/PersonalDesk`. For a separate workspace, set `PERSONAL_DESK_WORKSPACE` to an absolute path whose parent already exists. The local API normally binds to `127.0.0.1:8765`; an occupied endpoint causes an explicit startup failure.

The API endpoint and version are written to the workspace's `discovery.json`. Role credentials are stored separately in its private `credentials.json`; clients send a bearer token in the `Authorization` header. Reader, worker, and coordinator roles can query saved state. Worker and coordinator can receive messages; coordinator alone can create tasks and, on macOS, upload text materials. See [contracts and examples](docs/contracts.md) before writing a client.

## Run the Mac demo

After packaging the app, Python 3.9+ and its standard library are enough to run the isolated demonstration:

```sh
python3 scripts/test_demo_mac.py
python3 scripts/demo-mac.py
python3 scripts/demo-mac.py --inspect
```

Each run creates a new private synthetic workspace. It proves the empty state, worker message receipt with no task, separate coordinator task creation, a saved Unicode material, read parity, clean restart, and exact original-response replay. `--inspect` keeps the populated native app open for a visual walkthrough; quit it normally to complete the runner's final checks. The runner retains a report and workspace for local inspection without printing credentials. [The E1 acceptance index](docs/e1-acceptance.md) maps the demonstrated behavior to evidence and remaining gates.

For source checks, run `sh scripts/check.sh`. It regenerates bindings, installs locked frontend dependencies, checks types and tests, builds the frontend, then runs Go formatting, vet, tests, race tests, and a production binary build. CI also builds Mac and Windows binaries; a configured or passing CI job does not establish interactive Windows 11 acceptance.

## Authorship and current limits

Created by Anton Mashkov as an AI-assisted desktop application project. AI agents were used as implementation and review tools; the repository retains contracts, tests, verification records, and known limits so the result can be examined beyond a demo. The application itself does not use an AI model at runtime.

This repository demonstrates a local, durable intake-to-task foundation. General task updates, other Inbox resolutions, provider or agent integrations, automatic collection, Windows material support, native Windows verification, and release distribution are outside the implemented scope. The [verification records](docs/e1-acceptance.md), [recovery procedure](docs/recovery.md), and [diagnostic export evidence](docs/diagnostic-export-verification.md) document the tested boundaries in detail.
