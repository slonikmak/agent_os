# Story 2 recovery verification — 2026-10-29

Executed target: macOS 15.7.7 (24G720), arm64; Go 1.27.3; Node
24.22.2; Wails CLI/module/runtime 3.0.0. Storage pins remain Goose 3.29.0,
modernc SQLite 1.61.0, libc 1.79.0 and x/sys 0.49.0. No Windows command or
native Windows check ran for this story.

## Commands and results

From the repository root, the final source passed:

```sh
sh scripts/check.sh
go test -json -count=1 ./... > ${TMPDIR:-/tmp}/personal-desk-story2-reviewed-go-tests.json
sh scripts/package-mac.sh
python3 scripts/smoke-recovery-mac.py
```

`scripts/check.sh` passed binding generation, deterministic frontend dependency
installation, type checking, all four frontend tests, production frontend build,
formatting, Go vet, all Go tests, all Go race tests and the native production
build. The complete log is `${TMPDIR:-/tmp}/personal-desk-story2-reviewed-check.log`. Race coverage
included the real SQLite adapter and bootstrap/maintenance subprocess tests;
bootstrap's final race run passed in 17.533 seconds.

The fresh JSON run passed all 121 named test/subtest events across eight tested
packages, with zero failed or skipped test events. The main package and synthetic
fixture helper have no tests of their own. The retained JSON is an audit aid;
rerun the command to regenerate it.

The final bundle at `bin/Personal Desk.app` passed ad hoc signing and strict
signature verification. The production catalog still contains only migration1;
its released SHA256 remains
`f07d0ee79ec6ed34a588e922cad119eb9d364c68482eb5581c8f0e0ea54c1293`.
No dummy production migration or domain mutation/API was added.

The executable smoke passed populated native reads, offline backup/list/restore,
an installed-but-uncompleted intent replay blocking normal startup, explicit
resume twice, saved facts/revisions, retained backup and unchanged credentials
and materials. Output is `${TMPDIR:-/tmp}/personal-desk-story2-reviewed-recovery-smoke.log`. Its
synthetic workspace was retained at
`${TMPDIR:-/tmp}/personal-desk-recovery-smoke-75992bg8`.
The script printed no credentials. Parent visual verification also passed against
the reviewed bundle in the isolated `personal-desk-recovery-smoke-9yb5ss1o` root.
A replayed matching active/completed intent showed `recovery_required`; refresh,
Copy (changed to “Скопировано”) and Inbox navigation worked. Database/intent
SHA256 remained unchanged and no HTTP discovery appeared. After Cmd+Q, offline
list/resume and repeated resume succeeded. Reopening showed the saved synthetic
task and one Inbox record at revision42; authenticated HTTP agreed. Concurrent
maintenance returned `workspace_owned` without changing backup files. Cmd+Q
ended the process and removed discovery. The reviewed native window check is
complete; no test application was left running.

A second executable smoke (`${TMPDIR:-/tmp}/personal-desk-story2-real-wal-smoke.py`)
started and killed a real WAL writer after committing later facts, then restored
the selected earlier point. It proved the original main/WAL/SHM were quarantined
byte-for-byte, canonical sidecars were absent, the backup was unchanged and the
selected title/revision42 returned. Output is
`${TMPDIR:-/tmp}/personal-desk-story2-reviewed-real-wal-smoke.log`; its isolated root is
`${TMPDIR:-/tmp}/personal-desk-real-wal-a7gi_t75`.

## Acceptance matrix

Every named test/subtest below passed in the fresh JSON run on this Mac.

| Plan state/input | Executed evidence |
|---|---|
| Fresh/current/recognized older prefix | `TestFreshRestartAndReadOnlyPools` and `TestNativeAndHTTPReadSameWorkspaceRestartAndQuit` preserve identity, content revision and empty state. `TestFrozenV1UpgradeSkippedReleaseAndCurrentNoBackup` upgrades an independently populated frozen-v1 fixture through two private future steps, preserves task revision 7/workspace revision 42, verifies history 3 and proves current startup does not call the backup hook. `TestFailedFirstInitializationRetriesWithPreservedBaseline` retries real SQLite failure/cancellation after Goose baseline0 without resetting that row; `TestBaselineOnlyHistoryRequiresExactSchemaAndBaseline` rejects altered baseline states. |
| Pending upgrade including committed WAL | `TestBackupIncludesRealCommittedWAL` holds a live WAL writer with autocheckpoint disabled, proves nonempty WAL and verifies the committed updated task in the resulting online backup. `TestBackupFailurePrecedesAnyDDLAndCancelledStepRollsBack` proves backup refusal leaves original database bytes unchanged. `TestUpgradeRetainsOriginalRecoveryPointAcrossPartialFailure` proves bootstrap registers a v1 point before DDL and retains exactly that point across two failed retries after step2 commits. `TestCompletedUpgradeReturnAndNewUpgradeRetainNewFacts` completes an upgrade, returns to v1, saves new facts and upgrades again; the new point contains those facts while the original point remains unchanged. |
| Invalid catalog or unknown/newer/non-prefix history/schema | `TestEmbeddedCatalogAndDocumentedManifestAgree`, `TestEmbeddedMigrationsMatchFrozenManifest` and `TestInvalidCatalogsRefused` cover released identity, manifest agreement, ordering/version drift, nontransactional directives, spaced transaction escapes, filesystem SQL and malformed manifests. `TestHistoryAndSchemaDriftRefusedBeforeBackup` covers missing baseline, newer, down, duplicate, non-prefix and changed-column histories without backup or changed database bytes. Existing unsupported/unknown/literal-`sqlite_` tests remain passing. `TestIncompatibleBackupAndMalformedRegistryStopBeforeReplacement` blocks a pending upgrade on malformed recovery metadata. |
| Failed/cancelled/interrupted migration | `TestMigrationStepFailureRetryKeepsHonestHistoryAndBackup` proves earlier step2 commits, defective step3 DDL rolls back, retries do not skip it and the source-v1 backup/facts remain intact. `TestBackupFailurePrecedesAnyDDLAndCancelledStepRollsBack` checks deadline cancellation. `TestKilledMigrationKeepsEarlierCommitAndRollsBackInterruptedStep` actually kills a bounded child while step3 runs after ledger2 is visible, then verifies ledger2, absent interrupted DDL and preserved facts. |
| Explicit compatible registered restore | `TestMaintenanceBackupRestoreResumePreservesFactsAndPrivateFiles` verifies original task/content revisions, retained selected image, quarantined main/WAL/SHM, preserved credentials/materials and repeated explicit resume. `TestIncompatibleBackupAndMalformedRegistryStopBeforeReplacement` rejects a source-v1 point when target2 would require migration. The native executable smoke exercises registered commands without HTTP or migration startup. |
| Restore interruptions and normal-startup gate | `TestRestoreEveryDurableBoundaryResumesWithoutFreshDatabase` injects failure at intent publication; each main/WAL/SHM move and durable quarantine boundary; install move; installed verification; and completion-record publication. Each case proves `recovery_required`, unchanged or still-absent main file during normal startup, explicit successful resume and preserved facts. `TestInterruptedRestoreProcessesAreKilledAndResume` actually kills separate processes at those ten boundaries plus completed-intent retirement, then explicitly resumes each. `TestMalformedIntentBlocksBeforeSQLiteCreationAndReleasesLock` proves malformed intent never creates a database and releases ownership. |
| Corrupt/incompatible image, malformed intent, unsafe ID/file, collision or sync failure | `TestRecoveryUnsafeIDCorruptBackupAndCollisionPreserveEvidence` checks traversal/path/oversized/flag IDs with bounded safe output, corrupt image refusal, unchanged current bytes, a no-replace quarantine collision and a backup-directory symlink. `TestRecoverySyncFailuresAndCancellationStopSafely` injects directory-sync failure after the durable intent, proves the blocking marker remains, cancels a resume and then completes explicitly. `TestLateCompletionSyncFailuresRetainGateAndReconcilePair` covers each active final directory sync; failures keep or recreate the startup gate and matching records resume without overwriting evidence. `TestRepeatedCompletionSyncFailuresPreserveLaterSavedWork` saves later task/content revisions after a completed restore, injects each repeated-completion sync failure, proves no obsolete intent is recreated and later facts stay readable, then successfully retries the required syncs. `TestBackupCancellationAndInvalidDestination` checks backup cancellation and invalid destination; existing private/symlink/FIFO tests remain passing. |
| Competing startup/maintenance and resource release | `TestCompetingOwnerCannotTouchWorkspace` exercises a second process; `TestCompetingMaintenanceLeavesOwnerFilesUntouched` checks all four maintenance operations against an active owner. `TestMalformedIntentBlocksBeforeSQLiteCreationAndReleasesLock` and existing failed-startup/quit tests prove lock reacquisition. Every maintenance path defers lock release; validation connections close before moves. |

## Findings corrected during implementation

The interrupted-installed test initially failed because read-only validation of a
WAL-mode image could create new main-file sidecars. Closed recovery images now
use immutable read-only validation; live sources still use WAL-aware inspection.
The same killed-process test now passes at that boundary. Catalog checks also
reject spaced transaction-control statements, keeping Goose's per-step
transaction/ledger ownership intact.

Parent checks also corrected baseline-only initialization after a failed first
migration, stale upgrade-point reuse after a completed upgrade/return/new-work
cycle, and loss of the startup gate at the last completion sync. Their named
regressions above ran and passed in the final 121-event JSON run.

Independent Quick review found two further issues, both corrected: a repeated
completed resume could revive an obsolete intent after later saved work; the new
regression proves sync failures preserve those facts without recreating that
intent. The Windows helper opened a read-only handle for flushing; it now opens
a write-capable directory handle with no-reparse checks and propagates flush/close
errors, following the [FlushFileBuffers access requirement](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-flushfilebuffers).
This is an inspected correction, not Windows runtime validation. No review
finding was deferred; platform/visual evidence limits remain as stated here.

## Limits

Windows native ACL/locking/durability/runtime/UI acceptance remains pending.
Its platform helper is authored but has no new execution evidence here. Mac
subprocess kills exercise process interruption, not physical power failure or
every storage-device behavior. File and directory sync errors fail explicitly;
recovery files are retained. Recovery-point retention is manual and no deletion
policy is implemented. A deterministic defective migration still needs a
separately reviewed recovery procedure/release. Distribution signing/notarization and release
distribution remain outside this local verification.
