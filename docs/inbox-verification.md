# Durable Inbox verification

Executed on 2026-10-29 on macOS 15.7.7 (24G720), arm64, Go 1.27.3,
Node 24.22.2, Wails 3.0.0. All test data used isolated synthetic workspaces.
No Windows command, push, release or provider request was executed.

Commands from the repository root, with the pinned tools on PATH:

```sh
sh scripts/check.sh
go test -json -count=1 ./... > ${TMPDIR:-/tmp}/personal-desk-story4-go-tests.json
sh scripts/package-mac.sh
python3 scripts/smoke-mac.py
python3 scripts/smoke-inbox-mac.py
python3 scripts/smoke-recovery-mac.py
```

`check.sh` passed generated binding checks, strict TypeScript, 14 frontend
tests/subtests, production Vite build, gofmt, Go vet, Go tests, race tests and the
production native binary build. The fresh uncached Go run passed 258 tests and
subtests in ten tested packages, with no failures. Local ad hoc signing and
strict signature verification passed. Bindings expose only the two read methods.

| Case | Executed evidence | Result |
|---|---|---|
| Fresh worker/coordinator receive | HTTP integration tests and real bundle Inbox smoke | 201, pending revision1, UTC receipt, full original/provenance, one revision; zero tasks |
| Exact key replay, normalized omission/null, restart | Application/hash, SQLite and HTTP tests; real bundle retry/restart | Original result/status and revision retained; attempt ID is fresh |
| Same identity under new key/client | SQLite/HTTP tests and real bundle | 200, first submitter retained, no counter/revision/event change |
| Both changed-input conflicts | SQLite/HTTP tests and real bundle | Fixed 409 idempotency_conflict / message_identity_conflict |
| Concurrent deliveries | SQLite test with 24 parallel deliveries | One message and one event; 24 successful receipts |
| Rollback, cancellation, panic, exhausted counters/revision, occupied gate | SQLite fault seams and finite cancellation tests | No partial message/payload/receipt/counter/revision/event; next write succeeds |
| Post-commit callback failure | SQLite application publication panic and bootstrap subscription tests | Commit stays successful and replayable |
| Authentication, roles, Host/Origin, headers/media/version/body/fields | HTTP refusal matrix with privacy markers | Refused before effects; fixed errors/attempt IDs; no content in diagnostics |
| Shared committed detail and metadata | HTTP/native mapping tests, bootstrap real listener and actual bundle smoke | Matching detail/revision, metadata excludes bodies, passive reads preserve canonical tables |
| Receipt ordering under clock reversal | SQLite sequence-order regression | Newest transactional receipt first |
| Actual frozen-v1 upgrade | SQLite upgrade test and updated recovery smoke | Verified v1 backup retained; original rows/revisions preserved; no invented payload/provenance |
| Legacy unavailable original / identity reservation | SQLite query/receive tests | service_unavailable detail; conservative message_identity_conflict |
| Migration failure/interruption/recovery | Existing suites now append synthetic steps after real production prefix; native recovery smoke | Last committed prefix, original backup, quarantine and blocking intent preserved |
| Reordered snapshots/details, invalidation bursts, changed selection, failures | 14 frontend tests/subtests and native subscription race tests | Fresh matching selection only; same-selection saved view survives failure; teardown stops late callbacks |
| Native normal/narrow Inbox, live invalidation and passive UI inspection | Parent CUA inspection of the actual signed bundle | Passed at 1280×720 and about 640×610; live commit advanced visible Inbox/revision without refresh, preserved selection; original text/provenance readable; zero tasks |

The matrix audit also checked the actual uncached Go JSON output: each test
below had both `run` and `pass` events, without a skip. Frontend cases ran under
`node --experimental-strip-types --test src/services/*.test.ts`.

| Frozen plan row | Named executed coverage |
|---|---|
| 1 · fresh receive | `sqlite.TestReceiveImmutableReplayIdentityDuplicateAndRestart`, `httpapi.TestReceiveHTTPContractsReplayNormalizedPresenceAndDetail` |
| 2 · original key replay | Same SQLite/HTTP tests plus `intake.TestCanonicalReceiveHashPreservesFactsAndExcludesSubmitter`; bundle retry after restart |
| 3 · independent source identity | Same SQLite/HTTP tests and `bootstrap.TestActualHTTPReceiveNativeDetailsInvalidationAndRestart` |
| 4 · concurrent / aborted transaction | `sqlite.TestConcurrentDeliveriesAndReceiptSequenceOrder`, `sqlite.TestRollbackFaultCancellationExhaustionAndBoundedGate` (fault, cancel, panic, counter, revision and occupied gate) |
| 5 · boundary refusal / privacy | `httpapi.TestReceiveRejectsUntrustedInputsBeforeEffectsAndNeverLogsContent`, `httpapi.TestDuplicateAndBadHeadersFailBeforeQueries`, `intake.TestReceiveAuthorizationAndKeyPrecedeTransaction` |
| 6 · shared detail / ordering / passive reads | `bootstrap.TestActualHTTPReceiveNativeDetailsInvalidationAndRestart`, `wailsread.TestInboxNativeReadMappingFaultPrivacyAndResponseBudget`, `sqlite.TestConcurrentDeliveriesAndReceiptSequenceOrder`; bundle canonical table comparison |
| 7 · committed publication | `sqlite.TestPostCommitPublicationPanicLeavesReplayableSuccess`, `bootstrap.TestActualHTTPReceiveNativeDetailsInvalidationAndRestart`, `bootstrap.TestSubscriptionStopWaitsForCallbackAndClosePreventsLatePublications`; rollback/concurrency event assertions |
| 8 · real v1 upgrade | `sqlite.TestActualV1UpgradePreservesUnknownOriginalAndReservesIdentity`, existing migration/recovery suites and native recovery smoke |
| 9 · frontend freshness / saved error state | Frontend cases “subscribes before reading and discards delayed older requests”, “coalesces invalidations, rejects snapshots below freshness floor and rereads”, “selection changes discard late details and never reuse another selection on failure”, “detail invalidations coalesce and require the latest committed revision”, “same-selection detail failure preserves saved text and teardown ignores late detail results”, “a superseded same-selection detail cannot overwrite a newer read”, “a newer snapshot and failed selected detail settle without repeated reads” (snapshot-first and detail-first) |
| 10 · actual native receipt/read/restart | Actual bundle Inbox smoke and parent native inspection recorded below |

The actual bundle Inbox smoke retains a populated workspace at
`${TMPDIR:-/tmp}/personal-desk-inbox-smoke-b8obdus3`.
Its two API-created messages include original escaped text, a proposed task link,
source URL and inert artifact coordinates. Both remain pending; its board has no
tasks. The process is stopped and discovery removed before visual handoff.

Parent independently exercised the packaged API with exact-key and cross-client
normalized retries, both conflicts, six concurrent same-key requests and six
new-key identity retries, restart and original-response replay. Its isolated
workspace is retained at
`${TMPDIR:-/tmp}/personal-desk-story4-api-z_g5ly8u`.

In the actual native window, a fourth live HTTP delivery appeared without manual
refresh (Inbox 4 / workspace revision 5), preserving the previously selected agent
report. Selection, refresh, original multiline text, claimed source/sender,
first authenticated client/role, MSK event/receipt times and an unconfirmed task
proposal were inspected. Normal and narrow layouts preserved the adopted design;
at about 640×610 the reader scroll exposed the original, inert coordinates and
provenance. Literal Markdown-image and script text stayed text. The board remained
empty. A local source/image canary received zero requests. The complete canonical
SQL dump and credential hashes matched their post-delivery values after passive
UI actions and quit; the app exited 0 and removed discovery.

Parent reran the full source check and fresh uncached Go JSON suite after
strengthening the rollback regression to check unchanged counters/revision before
any test reset. Both passed; the suite count remains 258, frontend count 14.

Independent Quick review found a stale-detail retry loop when the snapshot
advanced while selected-detail reads failed. The adapter now requests only the
needed detail and preserves a settled detail error. A bounded regression covers
both response orders, retains revision2 text with a revision3 snapshot/error and
asserts no extra reads. Parent reran the full source check (14 frontend tests and
subtests), fresh 258 Go tests/subtests, packaging/signature verification and all
three actual-bundle smokes after this correction; all passed.

The final corrected bundle also passed a repeated native control check:
1280×720 and about 640×610, selected original text/provenance, manual refresh and
scrolling. A fifth HTTP delivery appeared automatically at workspace revision 6
while the original message remained selected. After passive inspection and native
Quit, the canonical SQL dump and credentials matched their post-delivery hashes;
there were five pending messages, zero tasks, zero source/image canary requests,
exit code 0 and no remaining discovery file. No review finding remains open.

The scope remains receipt and inspection. Task creation/resolution, task/material
read methods, link opening/fetching and diagnostic export are disabled. Windows
native acceptance is unverified and was excluded from this story's verification.
