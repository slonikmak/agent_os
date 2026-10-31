# Story 11 refactor verification — 2026-10-30

Target: macOS 15.7.7 arm64 (24G720), Go 1.27.3, Node 24.22.2,
Wails CLI/module/runtime 3.0.0. Baseline:
`1cc2801f8b872f815aca235f2ef3e9011631cb27`. Checks use isolated synthetic
workspaces. Story 10 installation/packaging acceptance remains explicitly
deferred by the owner; this cleanup does not establish Windows or full epic
acceptance. No personal workspace, push, publication or release is involved.

## Inventory dispositions

| Inspected item | Disposition |
|---|---|
| Numeric retained-event delivery values | Replaced with private `deliveryState` values `deliveryPrivate`, `deliveryQueued`, `deliveryPersisted`, `deliveryAmbiguous`, retaining zero/1/2/3. Ambiguous append documentation explains possible written bytes and omission accounting. Capture, eviction, queue admission, writer completion, merging, mutexes and close/drain ordering are unchanged. |
| Four native response-validation copies | One private `validateResponse` owns marshal → encoded byte budget → named schema validation. Each actual facade method still owns its reader/deadline, mapping, expected error handling, operation IDs and panic/fault boundary. No bound method or generic query framework was added. |
| Unread startup request storage | Removed the facade field, constructor argument and main call-site plumbing; updated both constructor tests. Bootstrap `StartupError.RequestID` and its diagnostic event remain unchanged. Each failed-start workspace read still creates a fresh attempt ID and retains the original error ID/code. |
| Stale capability/recovery descriptions | Current contract text now describes implemented Mac native Save/ZIP export, bounded sanitized entries, delivery coverage and disabled Windows destination/persistence. Recovery describes immutable migrations 1–4 and supported prefixes 1–3. Dated prior evidence is unchanged. |
| Existing frontend generations/API dispatch, filesystem/ACL checks, maintenance lifetime and canonical persistence | No cleanup needed or performed. Their existing regression tests and Mac smokes remain the verification guards. |
| Packaging/updater, Windows persistence/export and orphan cleanup | Separate deferred features; no implementation added and no acceptance claimed. |

## Matrix audit

All names below refer to executed Go tests or the eight Mac smoke scripts listed
in the command record. Assertions exercise the public facade methods; no test
calls the new response helper directly.

| Plan input/state | Executed guards and observable assertions |
|---|---|
| Valid workspace/Inbox/task/material native reads | `TestTransportMappingsAgreeAndNullsAreExplicit`, `TestPopulatedAndDetailMappingsShareSchemaFactsWithoutBindingNewMethods`, saved cases of `TestInboxNativeReadMappingFaultPrivacyAndResponseBudget`, `TestTaskNativeMappingFaultPrivacyBudgetAndValidation`, and `TestMaterialNativeBudgetsFaultsAndExpectedFailures`: typed native saved facts match HTTP mapping, explicit nulls/empty collections and unchanged four-method facade. HTTP receive/create/read tests and Inbox/task/material Mac smokes exercise committed saved facts and receipts. The material saved case now exercises the actual `ReadMaterial` method. |
| Expected errors, cancellation/deadline | Existing native expected-error cases plus all four methods' canceled/deadline cases in `TestFaultsUseTheirActualBoundaryAndExpectedCancellationHasNoFault`: bounded contexts, safe `service_unavailable`, fresh correlated operation ID, null fault ID and zero fault events. `TestHTTPAndWailsErrorsHaveEquivalentSafeApplicationFacts`, HTTP `TestCancellationReachesQuery` and bootstrap read-lifetime tests retain transport/lifetime coverage. |
| Invalid or oversized saved native DTO | Workspace invalid/budget checks and existing Inbox/task/material invalid/budget cases now assert the actual operation, matching request/error IDs and one internal fault for invalid data. Oversized results retain retryable `response_too_large`, null error ID and zero fault events; byte-budget validation still precedes schema validation. Existing HTTP fault/budget/corrupt-result tests also run. |
| Panic in each query | All four method panic cases in `TestFaultsUseTheirActualBoundaryAndExpectedCancellationHasNoFault`: method-owned recovery, `internal_error`, exactly one fault with the original operation and `panic` class, correlated IDs and no synthetic thrown/private text. Existing per-method privacy tests remain. |
| Repeated reads after failed startup | `TestFailedStartupReadsKeepFreshAttemptCorrelation`: two fresh attempt IDs, same original code/error ID, original bootstrap request retained and operation milestones match the responses. `TestFailedNativeStartupRetainsOnlyDiagnosticOwnership` additionally checks retained diagnostic ownership and no API discovery/readiness; failed-database Mac smokes retain the original bytes. |
| Private/queued/persisted/ambiguous delivery and overflow | Unchanged `TestSnapshotDrainsEarlierCaptureUsesIdentityAndReportsLoss` proves distinct identical captures, barrier ordering, persisted deduplication, ambiguous append omission and private ring loss; `TestArchiveBudgetAndQueueOverflowRemainBounded` and `TestUnknownLossNeverClaimsCompleteCoverage` prove bounded overflow/budget and honest incomplete coverage. No diagnostics test was added solely to mirror renamed constants. |
| Close, concurrency and ownership through drain | Unchanged `TestSlowSinkHasBoundedQueueAndCloseDisablesLateSharedCallbacks`, `TestSinkFailurePanicAndLateCaptureAreIsolated`, `TestCaptureRotationExportAndCloseDrainBeforeUnlock`, bootstrap late-callback/ownership tests and the full race suite exercise accepted writer/snapshot drain and shared ownership. Export smoke compares canonical SQL/receipts/credentials and validates sanitized archives. |

## Command record

Run from the repository root, with the pinned tools:

```sh
sh scripts/check.sh > ${TMPDIR:-/tmp}/personal-desk-story11-check.log 2>&1
go test -json -count=1 ./... > ${TMPDIR:-/tmp}/personal-desk-story11-go-tests.jsonl
sh scripts/package-mac.sh > ${TMPDIR:-/tmp}/personal-desk-story11-package.log 2>&1
sh scripts/package-diagnostic-smoke-mac.sh > ${TMPDIR:-/tmp}/personal-desk-story11-fixture.log 2>&1
python3 scripts/smoke-mac.py
python3 scripts/smoke-inbox-mac.py
python3 scripts/smoke-create-task-mac.py
python3 scripts/smoke-task-details-mac.py
python3 scripts/smoke-material-mac.py
python3 scripts/smoke-recovery-mac.py
python3 scripts/smoke-diagnostics-mac.py
python3 scripts/smoke-diagnostic-export-mac.py
```

The full check regenerates bindings, runs clean frontend install/typecheck/tests/
build, verifies gofmt, runs vet plus normal/race Go suites and builds the normal
production binary. The first check found an additional bootstrap constructor
test still using the removed argument; that call was corrected before rerunning
the complete check.

Results:

- `sh scripts/check.sh` passed: binding generation, locked frontend install,
  typecheck, all 42 frontend tests, production frontend build, clean gofmt, vet,
  normal and race-enabled Go suites, and production binary build.
- Fresh `go test -json -count=1 ./...` passed 466 test/subtest events across
  14 tested packages, with zero failures/skips. The raw evidence is
  `${TMPDIR:-/tmp}/personal-desk-story11-go-tests.jsonl`; parsed counts/package inventory
  are in `${TMPDIR:-/tmp}/personal-desk-story11-go-summary.json`.
- Normal and diagnostic-fixture packaging passed local ad hoc signing and strict
  signature verification. Logs are `${TMPDIR:-/tmp}/personal-desk-story11-package.log` and
  `${TMPDIR:-/tmp}/personal-desk-story11-fixture.log`.
- All eight Mac smokes passed against the rebuilt bundles. The sequential runner
  recorded each exact script, exit status, duration and per-script log in
  `${TMPDIR:-/tmp}/personal-desk-story11-smokes-summary.json`; logs follow
  `${TMPDIR:-/tmp}/personal-desk-story11-smoke-*.log`. The export smoke validated four ZIPs
  against frozen schemas/budgets and passed ready/React-failure/DB-failure/private
  owner, cancellation, replacement/protected destination refusal, excluded-marker
  scans and passive SQL/receipt/credential comparisons. Task and material smokes
  passed passive canonical hashes and zero-fetch canaries. Every smoke process
  exited; a process inventory confirmed no native bundle process remained before
  parent native acceptance started.
- `git diff --check` passed. No historical verification record was edited.

The baseline byte audit at `${TMPDIR:-/tmp}/personal-desk-story11-byte-audit.json` compares
Git blob bytes (including symlink targets) to the working tree: all 73 released
schema/example paths, five SQL/catalog paths, 12 generated binding paths and four
dependency manifest/lock paths are identical. Binding generation is included in
the full check; no released schema, SQL, generated binding or pin was changed.

## Parent native acceptance

The parent independently launched the rebuilt normal bundle with a fresh private
workspace and created pending/applied Inbox messages, a task/history and uploaded
Unicode material through the neutral HTTP API. Native workspace, task, material
and Inbox reads showed the saved revision 5/task revision 2 facts. The existing
drawer/content/history and material reader remained usable at 1280×720 and about
640×610; the pending Inbox reader showed the exact saved message.

Actual Help → Export diagnostics and the system Save panel produced a private ZIP
whose three entries passed the frozen schema/budget validator. Its coverage was
complete with 13 accepted records and zero omissions/unavailable/truncated records;
business/material/credential/path markers were absent. Canonical SQL/receipts
SHA-256 stayed `4cfaa019a8258d1fd95b6237c984910cb1538fdd84975aa3d81984559ca7af08`;
credentials and material blob inventories stayed identical. Quit returned 0,
discovery was removed and the session marker was clean.

A second isolated launch used deliberately invalid database bytes. Normal and
narrow native views retained `database_unsupported` and errorId
`3638efbf9c091dba716cf4736fff9dff`, including after Refresh. That ID matched the
single original startup fault; repeated workspace reads had distinct request IDs,
all different from its original bootstrap request. No API discovery appeared,
the database SHA-256 stayed `ac0ce2231c1057da80c351635b5cf09a49a834477cd7bb769929ad886f09df2f`,
and shutdown returned 0 with a clean marker. Exit was observed by process only.

Parent evidence: `${TMPDIR:-/tmp}/personal-desk-story11-parent.py`,
`${TMPDIR:-/tmp}/personal-desk-story11-parent-normal-result.json` and
`${TMPDIR:-/tmp}/personal-desk-story11-parent-failed-result.json`. Earlier unchanged UI
keyboard/failure evidence remains in the prior dated verification records.

## Review

The independent quick review read the approved plan, adopted architecture/quality
contracts and complete baseline diff, then inspected the surrounding code. It
reported no findings. The parent separately audited the six frozen matrix rows
against 30 named passing guards and native evidence. No review finding or cleanup
item was deferred; story10 and Windows remain the previously deferred work.

## Limits

Tests and smokes prove only their executed paths. Fatal/native process failures
can still lose recent diagnostics; bounded history is not canonical business
history, ambiguous appends remain incomplete, and accepted filesystem calls
have no hard kernel completion deadline. Installation, distribution signing,
Windows native behavior and full epic acceptance remain pending. Native visual
and actual Help/Save checks are separate parent evidence, reusing unchanged
prior UI evidence where appropriate; exit is observed through the launched
process to avoid an inspection tool relaunching the app.
