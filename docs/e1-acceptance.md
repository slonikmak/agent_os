# E1 Mac development acceptance — 2026-10-30

This index covers the 17 E1 accountable requirement portions and six Done when
conditions in the adopted desktop-intake epic. The accepted scope is a Mac
development demonstration with the existing normal `Personal Desk.app`.
Story 10 installation/manual replacement, Windows native acceptance and the
distribution signing decision remain deferred. This document cannot close the
installed or cross-platform epic gates.

## Reproduce and inspect

Build/package the normal app using the [README prerequisites](../README.md#build-and-checks).
Python 3.9+ and the standard library are sufficient for the neutral client.
From the repository root:

```sh
python3 scripts/test_demo_mac.py
python3 scripts/demo-mac.py
python3 scripts/demo-mac.py --inspect
```

The two demo commands also work from another cwd when passed the absolute script
path. They resolve the normal bundle relative to the script. There is no workspace
argument: every run creates a private synthetic root, `workspace/`, `exports/`
and `report.json`, then retains them. No production fixtures, fault hooks, provider
credentials, agent launch, installer, DMG or dependency changes are involved.

The report records the full source revision/dirty state, normal executable
path/size/SHA-256, runtime, byte audit, named successful checkpoints, response
status/length/SHA-256, canonical table/receipt hashes, credential hash, blob
inventory, both process lifetimes and final GET/HEAD canary counts. Credentials
and bodies are never logged. Original raw status/body bytes stay in memory for
exact replay comparisons. The retained workspace itself contains the synthetic
originals and owner-private credentials; do not publish or attach its contents.

`--inspect` reaches `awaiting-native-quit` only after the second launch has passed
restart/replay/read checks. Navigate the populated app and use native Quit.
The runner then compares immutable read-only SQL/receipt hashes, credentials and
blobs with their pre-inspection fingerprint, verifies discovery removal/listener
closure/clean session/exit 0, and closes its canary. Native observations stay
separate from automated assertions: `humanObservations.status=not-recorded` is
intentional even when the automated report passes. Save human observations in
the review evidence below. An interrupted/failed runner retains its evidence,
cleans only its own processes/listener and reports interruption/failure.

## Requirement evidence index

“Mac demonstrated” means only the named Mac development paths. “Partial” means
an accountable target, installation or platform portion is still pending.
Historical suites below are reused evidence, not newly executed test results.

| E1 portion | Status | Exact proof and remaining gate |
|---|---|---|
| R1 native launch and equivalent saved views | Partial | `demo-mac.py` `fresh-empty`, restart and all-role saved-view assertions; inspect Inbox/card/details checklist below. Installed Mac and Windows 11 25H2+ amd64 remain pending. |
| R2 statuses and kind/category mapping | Mac demonstrated | Runner checks a separately created `request`/`new` task and saved board summary. Native New/Other display uses the adopted mapping; [task-details evidence](task-details-verification.md) and current inspect checklist cover display. The unchanged frontend test `schema-evaluated populated facts map into native view with explicit nulls` asserts request→other; native inspection shows the six columns. |
| R3 compact cards, type filters, counts | Mac demonstrated | Runner asserts one task/two Inbox/one unprocessed/no attention and compares board facts to full task facts for each role. Native compact card/filter/narrow checks below. Candidate/preparation behavior remains E3. |
| R4 basic details and optional facts | Mac demonstrated | `read_views` checks saved context/provenance, one immutable creation event and associations across all roles/restart; create response checks null next action/actor. Native Content/History/Sources/material below; [task-details verification](task-details-verification.md). E3/E4 projections remain future. |
| R5 pending/applied Inbox originals | Mac demonstrated | `worker-receive-pending-zero-tasks`, `separate-create-material-pending-and-applied`, all-role `read_views`: exact original text/sender/source/times/submitter/artifacts retained, applied resolution links the created task. Native default unprocessed/all/linked-message checks below. Other resolutions remain E2. |
| R7 durable immutable receive/dedup | Mac demonstrated | Original worker receive is revision 2 with zero tasks. Exact original bytes replay before/after restart; changed key/content and identity/content return their distinct 409 codes. New-key identity redelivery returns current applied view without business changes. [Intake tests](../internal/adapters/sqlite/intake_test.go): `TestReceiveImmutableReplayIdentityDuplicateAndRestart`, `TestConcurrentDeliveriesAndReceiptSequenceOrder`. |
| R9 separate atomic coordinator resolution | Mac demonstrated | Runner separately creates revision 3/task revision 1, one task/event/resolution, denies other roles and terminal rewrite, and replays original bytes after live task revision 2/workspace revision 5. Transaction rollback/fault/concurrency are [create tests](../internal/adapters/sqlite/create_task_test.go): `TestCreateAtomicSavedFactsReceiveRedeliveryAndRestartReplay`, `TestCreateConcurrentSameAndDifferentKeysHaveOneOutcome`, `TestCreateRollbackFaultPanicCancellationExhaustionAndSharedGate`. |
| R11 coordinator-only creation and conventions | Mac demonstrated | Runner rejects reader/worker create, checks genuine first-submit provenance, null optional values, revisions and stale-message refusal. [HTTP create tests](../internal/adapters/httpapi/create_task_test.go) and [pure workflow tests](../internal/application/intake/create_task_test.go) cover normalized presence and conflict precedence. General update/link/move is E2. |
| R17 neutral authenticated versioned API | Mac demonstrated | Stdlib client discovers exact revision 2 capability inventory, uses all read roles, rejects bad authentication/Host/Origin/roles and representative identity/key/terminal/revision conflicts, and records safe status/code without credentials. Full bounds/schema/route/header cases: [contracts](contract-verification.md), `TestAuthenticationConfinementAndDiscovery`, `TestDuplicateAndBadHeadersFailBeforeQueries`, `TestReceiveRejectsUntrustedInputsBeforeEffectsAndNeverLogsContent`, `TestCreateHTTPRejectsBeforeWorkflowAndReplay`. |
| R18 passive committed views | Mac demonstrated | Runner compares explicit all-table read transactions, receipts, credentials and blobs across repeated all-role reads, exact retries, two lifetimes and inspect period. GET/HEAD canary remains zero. [Frontend ordering evidence](task-details-verification.md), SQLite `TestTaskDetailsRevisionAndFactsShareOneCommittedRead` and `TestSnapshotCountsShareReadTransaction` cover broader ordering. |
| R19 confined local materials | Partial | Runner uploads/reads exact Unicode Markdown with saved association, denies reader/worker upload, private/unknown material IDs and stale revision, preserves blob through restart/inspection. [Material evidence](material-verification.md) covers anchored handles, unavailable/missing/corrupt/unsafe inputs. This retained demo never deletes its blob. Windows confined material capability remains disabled/pending. |
| R21 serialized atomic state/history/receipts | Mac demonstrated | Live/restart exact raw replay and canonical fingerprints in runner. Fault/rollback/response-loss/publication cases remain attributed to named SQLite/create/material suites and [recovery evidence](recovery-verification.md), rather than injected into production. |
| R22 neutral native cycle/no mandatory agent | Partial | Runner uses only Python stdlib and loopback API/canary against the actual Mac bundle, and requires no agent or provider credentials. Zero canary fetches demonstrate only the synthetic source/image coordinates. Broader zero-provider boundary is supported by unchanged application ownership/imports/capabilities and [material/source tests](material-verification.md); the canary does not observe all network traffic. Windows cycle remains pending. |
| R23 genuine empty/no Python records/restart | Mac demonstrated | `fresh-empty` checks revision 1 with empty tasks/Inbox/sources and all counts zero before explicit API writes; both process lifetimes reuse that one workspace and saved state. No fixture seeding/import takes place. |
| R24 separation/migration/recovery/manual replacement | Partial | Normal bundle path and freshly allocated workspace are separate; credentials/materials/receipts remain intact. Embedded Goose migration/recovery named suites and [recovery evidence](recovery-verification.md) cover backup, interrupted steps, unsupported schemas and explicit recovery. Story 10 installed manual replacement and Windows remain pending. |
| R25 adopted native design and states | Mac demonstrated | Current inspect checklist plus [Inbox](inbox-verification.md), [task details](task-details-verification.md), [material](material-verification.md) and [story11 native evidence](refactor-verification.md#parent-native-acceptance) show saved native views, responsive layout, real empty/loading/error behavior. No UI business mutation is added. |
| R26 private bounded diagnostic/export foundation | Partial | Actual native Help/Save in inspect below; unchanged [diagnostic](diagnostics-verification.md) and [export](diagnostic-export-verification.md) suites cover cross-boundary safe IDs, log-sink isolation, rotations, private ZIPs and DB/React failure exports. Runner independently proves passive canonical/receipt/credential/blob state after export. Windows persistence/destination export are disabled/pending. |

## Done when index

| Epic condition | Status | Evidence and gate |
|---|---|---|
| 1. Installed empty→authenticated receive→separate creation on both targets | Partial | Same-workspace Mac normal-bundle runner proves development cycle. Installed Mac and native Windows remain deferred. |
| 2. Adopted saved Inbox/card/details, passive view/refresh | Mac demonstrated | Runner reads/fingerprints and native inspect checklist; related design/ordering/error evidence in the unchanged story records. |
| 3. Dedup, changed-content conflict, atomic creation and restart with neutral client | Mac demonstrated | Runner named checkpoints/replay comparisons plus SQLite intake/create rollback/concurrency suites. |
| 4. Private-release artifacts, separate data and safe manual replacement for both | Deferred | No distribution candidate is produced. Story 10 and Windows are open; signing/notarization remain the separate release decision. |
| 5. Own-schema fresh/current/upgrade/failure/interruption/newer refusal, backup preserved | Mac demonstrated | `TestFreshRestartAndReadOnlyPools`, `TestFrozenV1UpgradeSkippedReleaseAndCurrentNoBackup`, `TestMigrationStepFailureRetryKeepsHonestHistoryAndBackup`, `TestKilledMigrationKeepsEarlierCommitAndRollsBackInterruptedStep`, `TestHistoryAndSchemaDriftRefusedBeforeBackup`; bootstrap registered restore/kill tests in [recovery verification](recovery-verification.md). Mac process interruption does not establish physical power-loss behavior. |
| 6. Safe correlated diagnostics/rotation/export without DB, sink isolation/privacy on both | Partial | [Diagnostic/export matrices](diagnostic-export-verification.md#named-matrix-evidence), retained fixture results and actual native Help/Save; Windows acceptance remains pending. The new normal-bundle demo introduces no fault hook. |

## Matrix audit

Every frozen row has executed assertions or helper regressions. Test names refer
to `python3 scripts/test_demo_mac.py` (20 tests); named checkpoints are recorded
only after their assertions pass.

| Frozen state/input | Executed guard |
|---|---|
| Fresh actual bundle/missing executable/readiness | `fresh-empty`; `test_missing_executable_has_no_child`, `test_readiness_timeout_reaps_its_owned_child`, `test_early_exit_is_reaped_and_never_ready`. |
| Worker receive before separate coordinator creation, pending/applied | `worker-receive-pending-zero-tasks`, `separate-create-material-pending-and-applied`, linked original/resolution/history assertions and exact one-task/event/resolution counts. |
| Unicode upload, associations, all-role reads and original/live revision distinction | `read_views` for every role before/after restart; live workspace 5/task 2 compared separately with original receive/create/upload revisions 2/3/4 and exact raw receipt bytes. |
| Auth/role/Host/Origin/key/identity/terminal/revision refusal, no effects/leaks | `auth-role-host-origin-identity-revision-refusals`, create/upload refusal fingerprints, response credential-reflection guard. HTTP helper tests preserve status/error bytes in memory and refuse unexpected success. |
| Exact receive/create/upload retry before/after restart | `first-lifetime-clean-passive-replay`, `second-lifetime-restart-original-byte-replay`; status/raw equality, fresh attempt IDs and unchanged all-table/receipt hashes. |
| Passive reads/inspection, hashes, GET/HEAD zero, two clean lifetimes | `final-clean-quit-passive-fingerprints-zero-canary`; `test_canary_observes_get_and_head_and_closes` proves the observer detects both methods. `test_explicit_read_transaction_is_consistent_across_concurrent_commit` proves one coherent SQL snapshot; `test_fingerprint_closes_wal_connection_without_gc_on_success_and_failure` proves deterministic connection closure and release of shared WAL locks for successful and failed reads while a strong reference prevents collection. |
| Missing DB, fingerprint/HTTP failure, interruption and retained evidence | `test_missing_database_never_creates_files`, `test_database_disappearing_after_existence_check_is_not_recreated`, `test_corrupt_database_fails_without_replacing_it`, `test_closed_readonly_fingerprint_does_not_create_journals`, `test_receipt_credential_and_blob_drift_are_independently_detected`, `test_blob_symlink_is_refused`, HTTP error/malformed/budget/reflection tests, `test_failed_or_interrupted_runner_retains_honest_report_and_cleans`, `test_sigterm_becomes_interrupted_report_with_cleanup`, `test_app_cleanup_failure_still_closes_canary_and_never_passes`; import guard proves no import-time app/workspace/network/thread/umask actions. |

## Reused unchanged suites

The runner byte-audited all 232 tracked application/test/native-config/schema/SQL/
binding/pin paths (excluding scripts and docs) against source baseline
`5eb273e420c788dfa7e1a04ad6631ccc1af2a804`. It also refuses newly added production
inputs. The parent independently compared all 282 pre-existing tracked desktop paths except the intentionally edited README to that baseline, including prior smoke scripts and evidence: identical. Audit: `${TMPDIR:-/tmp}/personal-desk-story12-parent-byte-audit.json`.
That report used the nested checkout's baseline. The root-layout runner now uses
the equivalent app-only filtered-history baseline `ea84d96b3e2820796a2e4ce50fac5946c6685c81`.
Only the new runner/helper tests and documentation changed. Therefore the
[story11 full check](refactor-verification.md#command-record), Go test JSON
(466 passing events/14 packages/zero failed or skipped events), 42 frontend
tests, race checks and eight native smoke runs are reused unchanged evidence.
They have not been relabeled as new executions for this story.

Important named evidence beyond the happy-path demo:

- SQLite intake/create/material tests cover atomic rollback, cancellation, shared
  writer gate, competing keys, revision exhaustion, receipt ordering, response
  loss and publication failure without production fault injection.
- Bootstrap `TestActualHTTPCreateNativeSavedViewsInvalidationAndRestart` and
  `TestActualMaterialHTTPNativeParityRestartAndRoleBounds` compare actual HTTP
  with actual native query facades and committed invalidation facts.
- Frontend tests cover stale/reordered replies, selection generations,
  invalidations, loading/errors, saved-value preservation and retry. These are
  broader ordering/error evidence than a human navigating one successful demo.
- Recovery tests actually kill bounded children during migration/restore and
  prove rollback, retained backups, explicit resume and workspace ownership.
- Diagnostic/export tests cover bounded faults, sink failure, rotations, hostile
  records, DB/React failure, safe IDs, archive privacy, native destination and
  lifecycle/refusal boundaries. The separate fault fixture remains separate.

## Current execution record

Target: macOS 15.7.7 arm64 (Darwin 24.6.0). Python 3.9.6 neutral client.
Source baseline/revision is `5eb273e420c788dfa7e1a04ad6631ccc1af2a804`;
each report records its actual dirty state. Normal executable size is 25,454,624
bytes, SHA-256 `db5ed34ecd3cf4c8652829fc4d4d2d9552d5f2293e8a1fdb496182e5baf5705a`.

- `python3 scripts/test_demo_mac.py`: 20 tests passed after the review correction,
  raw result at `${TMPDIR:-/tmp}/personal-desk-story12-reviewed-helpers.log`.
- `python3 scripts/demo-mac.py` from the repository root:
  passed before the connection-close review correction, 80 responses checked, two exit-0 lifetimes, discovery/listeners closed,
  clean session markers and GET=0/HEAD=0. Command output is
  `${TMPDIR:-/tmp}/personal-desk-story12-demo.log`. Retained report:
  `${TMPDIR:-/tmp}/personal-desk-e1-demo-iqo7xgux/report.json`.
- The pre-review inspect run used the runner with `--inspect` and retains
  `${TMPDIR:-/tmp}/personal-desk-e1-demo-25t3k28m/report.json`.
  Passed after parent native Cmd+Q: both lifetimes exited 0, discovery/listeners closed, session markers clean, all saved fingerprints unchanged and GET=0/HEAD=0. Separate human evidence is `native-observations.json` beside that report.

The review correction explicitly closes the fingerprint SQLite connection on both
success and exception while retaining its coherent `BEGIN`/rollback read
transaction. The new WAL lifecycle regression first failed both paths against
the original helper, then passed with deterministic closure: after the writer closes, a new connection
acquires exclusive WAL ownership and checkpoints successfully while the test still
holds the inspection connection; immutable reopen sees the committed receipt. All 20 helper tests passed; the parent owns the
post-correction full automated/native verification.

## Native checklist and observations

In inspect mode, verify default unprocessed Inbox and its exact pending original;
switch to all and open the applied original/confirmed task link. Open the board
card and verify New/Other, saved summary, absent next action/actor, Content,
History, Sources, related Inbox and exact Unicode material. Exercise type filters
and refresh at normal and narrow window sizes. Use actual Help → Export
diagnostics and native Save with a new destination in the retained `exports/`
folder, then inspect its frozen schema/private entries/content exclusions.
Use native Quit; observe exit by the owned process rather than a tool that could
relaunch the ordinary app. Never click the synthetic source/image URL while
measuring passive navigation.

The parent independently observed the 1280×720 board with all six columns,
selected type Other (`Другое`) and retained New=1/total=1. Normal and narrow
Content/material readers were legible with exact Unicode, escaped script and
Markdown and the long line; Refresh succeeded and Escape restored card focus.
Sources showed the original worker and distinct coordinator, an unavailable file
coordinate and the saved material. Inbox opened with one pending of two records;
All showed the applied original, one coordinator history event and its confirmed
task link. Following that link reopened the same task revision 2/workspace 5.

Actual Help → Export diagnostics → native Save produced
`exports/personal-desk-diagnostics.zip` in the retained inspect root; the parent
acknowledged success before one native Cmd+Q and made no further app observations.
The parent ran `go run scripts/validate-diagnostic-export.go` with the absolute
retained ZIP path and passed one archive. It contained exactly three expected entries, mode0600, blank reproduction
steps and 104 validated log records matching `coverage.acceptedRecords`. The
parent's in-memory scan excluded credentials, workspace path, original/pending
text, task summary, script marker, source/image canary and file coordinate.
Archive SHA-256 is
`d7fdf5ab05ba4b001887744dd76ac3cd05da705e1ebaf2ba7c2330325bdb89f0`.
Independent immutable read-only all-table counts/hashes, credential and blob
hashes matched the final report. Inspect SQL SHA-256 stayed
`d0facb044dcc0211e779e472e397e9cd1ecf2d9e517178dc44e789e08f745e68`.
The separate `native-observations.json` beside the report records the human checks;
`report.json` keeps its automated `humanObservations.status=not-recorded` contract.

Temporary artifacts remain local and may be removed by OS cleanup; repository
documentation records the commands and limitations.


## Parent post-review verification

The single quick-review finding was deterministic SQLite connection ownership in
the new fingerprint helper. It was corrected with explicit closure; no application
source, design, schema or dependency changed. The parent independently reran:

- `python3 scripts/test_demo_mac.py`: 20/20 passed, including both success/failure
  WAL closure subcases; `${TMPDIR:-/tmp}/personal-desk-story12-parent-helpers-postreview.log`.
- The automated demo command from the repository root: all seven checkpoints and
  80 responses passed, with two clean exits and GET=0/HEAD=0; output at
  `${TMPDIR:-/tmp}/personal-desk-story12-parent-demo-postreview.log`, retained report at
  `${TMPDIR:-/tmp}/personal-desk-e1-demo-b5eqna0a/report.json`.
- The demo command with `--inspect`: all seven checkpoints/80 responses
  passed after native Cmd+Q, two clean exits/discovery/listener/session markers,
  unchanged SQL/receipts/credentials/blobs and GET=0/HEAD=0; report and separate
  `native-observations.json` at
  `${TMPDIR:-/tmp}/personal-desk-e1-demo-ohuyr9v6/`.

The parent repeated normal six-column board/card/details, exact Unicode material
and refresh, actual Help/Save success and native Quit on the corrected runner.
The earlier Inbox/History/Sources/filter/narrow-layout observations remain valid
for the unchanged application. The new ZIP passed the same Go frozen-schema/budget
validator and independent in-memory credential/content exclusions: three entries,
mode0600, blank reproduction, 101 records matching accepted coverage; SHA-256
`2608a4f6b200da22294cb9dbcab0031bb111bc5e1e29cee300bc1e0bb6514de3`.
Independent closed read-only table/receipt/credential/blob comparisons matched its
report; SQL SHA-256 `65941d27a1af9a69bb6ce43769821a4bdc2d1ace80613dcf63881a76f54d0e99`.
This closes the scoped Mac development verification and the single review finding;
the installation, signing and Windows gates above remain open.
