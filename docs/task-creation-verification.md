# Atomic task creation verification — macOS

Story5 adds an external coordinator command to the existing local receive/query
slice. All inputs and workspaces below are synthetic and isolated. It preserves
the frozen v1 schemas, the two native business query methods and released SQL1/2.
The full Task is returned in the create receipt; the task-detail route/UI remains
story6 work. No provider, material or evidence URL is opened by these checks.

## Environment and commands

Executed on macOS 15.7.7 (24G720), arm64, Go 1.27.3, with the pinned Go/Node/Wails
dependencies on PATH. Run these commands from the repository root:

```sh
sh scripts/check.sh
go test -json -count=1 ./... > ${TMPDIR:-/tmp}/personal-desk-story5-final-go-tests.json
sh scripts/package-mac.sh
python3 scripts/smoke-mac.py
python3 scripts/smoke-inbox-mac.py
python3 scripts/smoke-recovery-mac.py
python3 scripts/smoke-create-task-mac.py
```

The source check covers binding generation, npm clean install/typecheck/tests/
production frontend build, gofmt, Go vet, ordinary Go tests, race tests and the
production native build. A previous run exposed an old migration cancellation
assertion that allowed only v1/latest: additive v3 introduces a legitimate v2
committed prefix. The corrected test allows recognized committed prefixes and
explicitly checks that the cancelled migration left no table.

Final source check, fresh JSON tests and package commands passed. The fresh JSON
run records **317 passing tests/subtests, zero failures and 11 passing test packages**.
Logs are `${TMPDIR:-/tmp}/personal-desk-story5-final-check.log`,
`${TMPDIR:-/tmp}/personal-desk-story5-final-go-tests.json` and
`${TMPDIR:-/tmp}/personal-desk-story5-final-package.log`.

All four packaged smoke commands passed. Their logs are
`${TMPDIR:-/tmp}/personal-desk-story5-native-smoke.log`,
`${TMPDIR:-/tmp}/personal-desk-story5-inbox-smoke.log`,
`${TMPDIR:-/tmp}/personal-desk-story5-recovery-smoke.log` and
`${TMPDIR:-/tmp}/personal-desk-story5-create-smoke.log`. Each command stopped its app and
retained its private synthetic workspace. Workspace names under the macOS
temporary directory are `personal-desk-smoke-lnzz3407`,
`personal-desk-inbox-smoke-hup8o9jm`,
`personal-desk-recovery-smoke-6ksbrija` and
`personal-desk-create-task-smoke-ig_rza97`; the logs hold their exact local paths.
Native visual inspection passed in the parent verification below; it is not
inferred from these automated checks.

The parent's independent actual-bundle neutral-client pass also succeeded. It
checked role refusal, currentRevision/terminal/target/payload conflicts,
provenance/materials, operation-key independence, six same-key and six
different-key concurrent commands, recovery after discarding a response,
receive replay compatibility, passive reads and restart. Its retained workspace
is `personal-desk-story5-parent-_pa5_tv8` in the same macOS temporary directory,
initially with five tasks/seven messages and zero observed canary URL fetches;
the native pass below added one further message/task.

## Named matrix

| Required behavior | Actual test evidence |
|---|---|
| Pure Tasks rules; new/request, revision1, nullable values and copied inert associations | `TestNewTaskRulesRejectInvalidOrUnsupportedValues`, `TestConstructRetainsNullableValuesAndInertAssociations`, `TestCreateWorkflowPureConstructionAndReplayBeforeEntityRead` run without SQLite/Wails. |
| Coordinator authorization and conflict precedence | `TestCreateWorkflowPureAuthorizationAndConflictPrecedence` proves reader/worker refusal before transaction entry, terminal before stale revision, pending-only eligibility, agent-report/proposed-association refusal and no writes. |
| Complete atomic saved result and provenance | `TestCreateAtomicSavedFactsReceiveRedeliveryAndRestartReplay` checks genuine UTC times, task snapshot, immutable original submitter, separate coordinator, event reason/provenance, stored material metadata, applied message/resolution, saved card/counts and original full receipt after restart/later workspace changes. |
| Target/payload/client/operation key scopes; original receive results after resolution | The same SQLite test checks changed target and independent caller scope, exact original pending receive replay and current applied identity redelivery. `TestCreateStateRefusalsAndOperationScope` proves one caller may reuse the same key independently for receive/create. `TestCreateHTTPFullResultNormalizedReplayAndSavedReads` proves omission/null normalization, changed payload conflict and distinct attempt IDs with identical replay bytes. |
| Concurrent same-key/different-key callers | `TestCreateConcurrentSameAndDifferentKeysHaveOneOutcome` runs 20 simultaneous callers per case: all same-key results succeed identically, or one different-key winner and 19 terminal losers; exactly one task/creation/event/resolution/counter/revision change and publication. The packaged create smoke also races eight exact retries. |
| Fault/panic/cancel/gate/exhaustion rollback | `TestCreateRollbackFaultPanicCancellationExhaustionAndSharedGate` compares every row in all nine canonical tables before fixture resets. It covers precommit fault, panic, cancellation, each creation counter, message revision, workspace revision and occupied shared gate; no partial original/receipt/counter/revision/history survives and the gate is reusable. Existing receive tests retain message-counter exhaustion coverage. |
| Missing/stale/unsupported saved state | `TestCreateStateRefusalsAndOperationScope` checks not_found, revision_conflict with currentRevision, agent_report, proposed association and non-pending needs_attention, with complete unchanged canonical facts. |
| Constraints and future history | `TestCreatePublicationFailureAndTerminalConstraintPermitNonterminalHistory` checks one terminal row, multiple nonterminal rows, task FK, coordinator event role and bounded counters. It also proves notification panic leaves the 201 result replayable. |
| HTTP rejection before workflow/replay | `TestCreateHTTPRejectsBeforeWorkflowAndReplay` uses a counting command port to prove zero workflow calls for roles/auth/Host/Origin, invalid/duplicate headers, version/query/media/key failures, unknown/duplicate JSON, invalid revisions/kind/action, null task, UTF-8/trailing/body bounds. Every error is schema-valid with a generated attempt ID; private markers enter neither errors nor diagnostics. |
| HTTP/native saved view agreement and lifecycle | `TestActualHTTPCreateNativeSavedViewsInvalidationAndRestart` uses the actual listener, compares HTTP/Wails saved Inbox facts, checks card/counts, revisions2/3 published once, exact retry and restart. Existing subscription tests cover unsubscribe/close and panic isolation. |
| Supported upgrades and immutable released SQL | `TestActualV1UpgradePreservesUnknownOriginalAndReservesIdentity` retains v1 summaries/revisions without inventing originals. `TestIndependentReleasedV2UpgradePreservesOriginalReceiptAndLegacySummaries` uses a separately frozen released-v2 image, validates the v2 backup, preserves original/receive receipt/revisions/legacy summaries and creates after upgrade. `TestReleasedV2SQLIsImmutable` freezes SQL2's released digest; existing manifest tests freeze SQL1 and the packaged ordered catalog. Existing recovery tests cover backup failure, cancelled/interrupted steps and controlled restore. |
| Passive UI and saved queries | Automated native queries and packaged smokes prove passive reads preserve canonical data. The existing read-adapter frontend suite covers stale/reordered reads, generations, refresh, invalidation and preserved state on failure. Normal/narrow native filtering/selection/refresh passed in the parent CUA check below. |

## Parent native inspection

The same packaged binary was inspected with CUA at 1280×720 and about 640×610
against the retained parent workspace. The actual API-created cards showed
request/other, new status, real saved titles and accurate counts; absent optional
fields were not invented. Opaque IDs fit the cards, with full values retained in
the accessible text/title. Type filters showed both matching cards and a truthful
empty state. The applied Inbox filter showed confirmed task IDs, coordinator
reason/time/client and the original worker provenance separately. Original text,
inert source/artifact coordinates, narrow scrolling and refresh were readable.

While the board was open, one further receive/create cycle appeared automatically:
six tasks, eight messages, two unprocessed and workspace revision 15. After passive
selection/filter/navigation/refresh and native Quit, the complete canonical SQL
dump and credential hashes matched their post-delivery values. The source/image
canary saw zero requests; process exit was 0 and discovery was removed. All ten
frozen plan rows have executed coverage; every named Go test above had run/pass
events in the fresh JSON output, without test skips or failures. The frontend
suite passed 14 tests/subtests.

## Review correction and final verification

Independent Quick review found one malformed route: a resolution POST without a
message-ID segment could reach the command as ID “resolutions”. The parser now
requires both complete path delimiters and a valid middle ID. The regression
failed before the correction and now passes: missing IDs, doubled slashes, extra
segments and trailing slashes return404 without workflow calls, including reuse
of an actual committed create key.
`TestCreateHTTPMalformedResolutionPathsDoNotInvokeWorkflow` and the updated
`TestCreateHTTPFullResultNormalizedReplayAndSavedReads` both ran and passed in the
final JSON output. No review finding remains open or deferred.

After this HTTP-only correction, parent repeated the full source check, fresh
317 Go tests/subtests, 14 frontend tests/subtests, race/vet/typecheck/build,
packaging/signature, all four bundled smokes and the independent client scenario.
All passed. The repeated client retained
`personal-desk-story5-parent-6dnbp_hs` with five tasks/seven messages, stable
restart/replay, passive canonical state and zero canary fetches. The correction
does not change the read/view code exercised by the native inspection above.

## Limits

Windows was neither built nor exercised for this story and remains unverified.
Signing for distribution, installation, full task details, material reads,
provider actions and persistent diagnostic export are outside this increment.
The packaged app uses the existing local ad hoc Mac signing procedure.
