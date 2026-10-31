# Story 8 diagnostic verification — 2026-10-30

Verified target: macOS 15.7.7 (24G720), arm64; Go 1.27.3, Node 24.22.2 and Wails CLI/module/runtime 3.0.0. Only isolated synthetic workspaces were used. The baseline is `6d74d5117e052da118cf389a6c0e744f1e8a7029`. No Windows check, push or release was performed. ZIP export/native Save belongs to story 9.

## Implemented boundaries

`diagnostics.Buffer` validates the frozen event catalog before retaining a record. It owns a 128-record private ring, a 128-record writer queue and one sink worker. Sink errors/panics, queue overflow and rotation failure mark diagnostics degraded; they cannot alter an already committed result. The worker drains and closes before workspace ownership is released. Before ownership and after close, capture uses private memory only. Framework messages/attributes, raw errors/panic values, browser stacks/props, paths, tokens, SQL parameters and business text are excluded. Correlation IDs use crypto/rand with an opaque diagnostic-only fallback; credentials separately require crypto/rand success.

Bootstrap starts the pipeline after acquiring the workspace OS lock and before opening SQLite. Native failed-start shells retain diagnostic ownership while normal reads/API remain gated. Shutdown cancels/drains domain resources, queues app.stop, disables late shared capture, drains the writer and records clean shutdown before unlocking. The canonical small marker contains only sessionId/clean; an incomplete prior session signals interruption without asserting its cause.

Mac persistence uses a no-follow ancestor walk and retained directory handles. Fixed child names use openat/renameat/unlinkat with owner/mode/link/Finder-alias checks. Replacement cannot redirect an opened sink. The active file is bounded to 5 MiB with four backups; startup repairs invalid/incomplete active records and drops oversized retained files with degraded availability. Event size is at most 16 KiB.

The separate DiagnosticFacade exposes only `GetDiagnosticStatus` and `ReportFrontendFault`. It has no domain/path/provider authority and no HTTP route. Reports use the frozen 4 KiB schema, 20/minute limit and 60-second code/component repetition suppression. Frontend reports contain fixed metadata, not thrown values; unknown source frames remain empty. Reports settle within one second, cancel timed-out calls and retain at most four unresolved calls. Technical health polling uses the existing status method every two seconds, with a one-second deadline, one underlying request and explicit teardown. It detects an asynchronous sink failure even while business reads succeed.

Command recovery carries a private, fixed panic classification through the existing failure type. An actual receive/create/upload panic produces a sanitized internal_error with class=panic and gates further mutations until restart. Ordinary rollback errors, expected validation/auth/conflict/cancellation and postcommit notification/diagnostic failures retain their existing behavior. The marker is absent from public DTOs and released schemas.

## Executed regression matrix

| Behavior | Passing evidence |
|---|---|
| Session/catalog/duration/frame bounds and copied snapshots | `TestEventCatalogAndEveryBound`, `TestRetainedEventsDoNotAliasCaptureOrSnapshotPointers`, existing fallback privacy tests |
| Sink error/panic, bounded queue and late callbacks | `TestSinkFailurePanicAndLateCaptureAreIsolated`, `TestSlowSinkHasBoundedQueueAndCloseDisablesLateSharedCallbacks` |
| Frontend validation/rate/repetition/correlation | `TestFrontendValidationRateRepeatAndFaultCorrelation`, `diagnostics.test.ts` |
| Private rotation, complete records, aliases and directory replacement | `TestDiagnosticRotationRepairsPartialRecordsAndRejectsAliases`, `TestDiagnosticDirectoryReplacementCannotRedirectOwnedWrites` |
| Interruption, competing ownership and failed-start shell | `TestInterruptedMarkerAndPostCloseCallbacksNeverWriteSharedFiles`, `TestFailedNativeStartupRetainsOnlyDiagnosticOwnership`, diagnostic bundle smoke and parent native lock checks |
| Unsafe sink fallback without blocking readiness | `TestUnsafeDiagnosticDirectoryDegradesWithoutBlockingReadiness`, diagnostic smoke and independent parent API cases |
| Committed receive/create/upload and immutable receipts under failing diagnostics | `TestCommittedHTTPResultsReceiptsAndStateSurviveDiagnosticFailure`, `TestPostcommitNotificationsAndDiagnosticsDoNotBecomeCommandPanics`, parent actual-bundle exact replay |
| Actual command panic versus rollback/expected/cancellation | All 12 subcases in `TestActualCommandRecoveryClassifiesPanicsAndGatesUnsafeMutations`; `TestRecoveredPanicClassificationSurvivesInternalSanitization` |
| Correct native operation/fault and attempt IDs | `TestFaultsUseTheirActualBoundaryAndExpectedCancellationHasNoFault`, `TestFailedStartupReadsKeepFreshAttemptCorrelation` |
| Migration observer isolation and unsafe-invariant gate | `TestMigrationObserverIsTypedAndCannotChangeMigrationResult`, `TestUnsafeInvariantHaltGatesDomainUntilRestartAndKeepsDiagnosticOwnership` |
| Live health without failed business reads, bounded/hung requests and teardown | All three `diagnostic-health.test.ts` cases and parent dynamic-health CUA check |
| Saved-view ordering, bridge failures, React fallback/copy/reopen and layout | Existing saved-read suites, delayed-report teardown test and parent native checks below |

Every named case ran and passed. All ten rows of the approved plan matrix are covered by these tests, the seven actual-bundle smokes and parent API/CUA acceptance.

## Final command results

From the repository root, with the pinned tools on PATH:

- `sh scripts/check.sh`: pass, including binding generation (3 services/7 methods), frontend typecheck/build, all 42 frontend test events, Go formatting/vet/tests/race and production native build.
- `go test -json -count=1 ./...`: 426 passing test/subtest events across 14 tested packages; zero failed or skipped test events.
- `sh scripts/package-mac.sh`: pass, locally ad hoc signed/strictly verified normal bundle.
- All seven Mac smokes: pass — lifecycle, recovery, Inbox, create-task, task-details, materials and diagnostics.
- `sh scripts/package-diagnostic-smoke-mac.sh`: pass, separately signed test bundle and asset graph. Production assets exclude fixture controls/canaries.
- Baseline byte audit: all 52 released schema/example/SQL files unchanged. `git diff --check`: pass. All own native processes exited after verification.

Accepted logs are `${TMPDIR:-/tmp}/personal-desk-story8-reviewed-{check,go-tests,package,smokes,fixture}.log`. Earlier implementation/acceptance logs retain intermediate runs; the counts above describe the final tree after review corrections.

## Parent native/API acceptance

The parent independently exercised real receive/create/upload through the signed bundle, exact replay with an unsafe log directory, expected forbidden/conflict results, private marker exclusion, interruption/restart and clean shutdown. Full-SQL canonical SHA-256 remained `96edeee63d0463f2679cbfcfc6c2be45ce36640fd89975b704e31d22dc2b7400`; credentials remained identical.

At 1280×720 and approximately 640×610, runtime/promise errors preserve the saved board and use the existing warm notice surface without covering its heading. Tab reaches Copy; Enter copies actual IDs into an offline local test field. Render failure provides readable Copy/Open again controls, and keyboard Open again restores the same saved board/revision 4. The temporary clipboard tab was closed; other user tabs were untouched.

After review corrections, the final fixture matched these visible IDs to safe records: runtime `63955afe9678036fe0228d0291354973`, promise `c725c5edf5f1c7b9674a8194b770fd4c`, render `c883d3c9f95b17757a5300c92f39e93e`. Unknown frames were omitted; synthetic private/business/path markers were absent from diagnostic and process output. SQL/credentials stayed unchanged; quit 0, clean marker and discovery removal passed.

The normal failed-start shell showed/copy-confirmed errorId `58a9cd621c8a8cff216e853687855f6c`, matching the startup record. Tab/Shift+Tab reach Copy at both widths. Original invalid database bytes remained identical; no API was advertised, the OS lock stayed held through the living shell and could be reacquired after normal drain/quit.

The final dynamic-health check started with a healthy saved board, then changed the owned log file to mode0400 and performed a successful GET/workspace read. The native notice changed to “Локальная диагностика ограничена. Сохранённые результаты доступны.” without any business read error. Refresh still succeeded; the narrow notice remained readable. Restoring mode0600 and quitting retained full SQL/credentials, a clean marker and exit 0.

Parent local evidence: `${TMPDIR:-/tmp}/personal-desk-story8-{parent-smoke.py,parent-session.json,startup-watch.py,startup-session.json,health-watch.py,fixture-watch.py,fixture-observation.json}`.

## Review and limits

Quick review found three defects: hidden command-panic classification, stale health observation and Windows pathname reopening. The parent also corrected stale enabled-surface documentation. All four findings were fixed; no review finding was deferred. Earlier native acceptance corrected Tab reachability, per-attempt startup request IDs and notice placement before review.

Windows persistent diagnostics are deliberately disabled: its adapter opens no diagnostic pathname. The bounded private ring/report/status pipeline remains, and Windows persistence requires an anchored adapter plus native ACL/reparse/replacement tests before enabling it. No Windows behavior is claimed verified here.

The ring/queue and rotated history provide bounded diagnostic availability, not business audit history. Overflow, permissions/IO failures and repair/cleanup can omit records. Controlled recovery cannot cover native/fatal runtime crashes, OOM, process kills or power loss. Drain depends on OS filesystem calls completing; there is no hard kernel I/O deadline. ZIP export/native Save remains story 9.
