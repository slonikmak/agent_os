# Confined material verification — story 7

Implemented target: macOS 15.7.7 (24G720), arm64; deployment target 15.0. Toolchain: Go 1.27.3, Wails/runtime 3.0.0, Node 24.22.2. Only synthetic isolated workspaces are used. The bundle is locally ad hoc signed and verified by `scripts/package-mac.sh`; it has no distribution signature or release upload.

## Source checks

From the repository root, with the pinned tools on PATH:

```sh
sh scripts/check.sh
go test -json -count=1 ./...
sh scripts/package-mac.sh
```

The source check regenerates the four-query saved-state facade and separate HTTP(S) navigation facade, installs locked frontend dependencies, checks TypeScript, runs frontend tests/build, checks gofmt/vet, runs Go tests and the native race suite, and builds the production executable. The fresh JSON test run avoids relying on a cached test result. Final command results and actual-bundle smoke evidence are recorded below after execution.

## Named behavior matrix

| Case | Evidence |
|---|---|
| Coordinator-only explicit upload; exact retry before current-task/file checks; changed payload/target conflicts | `application/materials` pure tests; SQLite upload/replay/restart tests; real bootstrap HTTP tests; material bundle smoke |
| Genuine task creation required; stale revisions; combined 32-association capacity; safe revision ceilings | Pure workflow tests and real SQLite capacity/concurrency/failure tests |
| Concurrent same-key retries produce one association; different stale commands have one winner | Real SQLite tests use the shared writer gate and count committed publications |
| SQL registry/audit immutable; original creation/provenance/history and receive/create receipts retained | Registry update/delete refusal tests; original association/read tests; schema3 upgrade receipt/fact equality tests |
| Write/sync/publish/directory-sync/precommit failures and cancellation | Filesystem fault seams; SQLite rollback, durable-byte cancellation and precommit panic tests; failed uploads have no registry authority |
| Released schema1/2/3 to schema4; consistent retained backup; SQL1–3 unchanged | Existing independent frozen-v1/v2 upgrade cases, explicit schema3-to4 backup/fact/receipt test, migration manifest/frozen SQL tests; recovery bundle smoke |
| Registry-only lookup; unknown names/IDs cannot discover files | Real HTTP/native/bootstrap tests and material bundle smoke; no reader scans directories or interprets coordinates as paths |
| Ancestor/leaf symlinks; retained handle after directory replacement; hardlinks; Finder aliases; directories/FIFO | `adapters/materials` native Mac tests; all unsafe leaves return no bytes and no FIFO blocking |
| Opened-file size/digest verification; missing/unsupported/too-large results | Filesystem and query tests; material smoke removes a known blob and proves null content while task context remains saved |
| Empty/exact limit/over limit; Unicode; malformed UTF-8/binary controls; unpaired JSON surrogates | Pure text, filesystem/query and additive decoder tests; valid surrogate pairs and literal backslash text remain exact |
| HTTP/native same frozen envelope and committed registry read revision; fixed private errors and 4 MiB budgets | Actual bootstrap HTTP/native parity and restart test; facade schema/size/fault tests; existing shared transport enforcement |
| Reordered material/task/snapshot reads; selection/task switch; close/reopen; invalidation bursts; teardown | Independent deferred material adapter tests, including a task arriving after material and freshness-floor reread |
| Same-selection errors preserve saved content and settle; explicit refresh retries | Frontend adapter tests; material reader preserves saved text with error/code-copy and explicit refresh |
| HTML/Markdown/images remain escaped text; source/image URLs cause no automatic requests | Text-token preservation tests, JSX text nodes, existing task-details and new material bundle canaries |
| Invalid schemes, credentials, paths, controls, malformed hosts/ports and ambiguous numeric hosts stay inert | Shared frontend URL tests and native opener spy tests; native allowlist is enforced immediately before OS browser call |
| Accepted link opens only on deliberate user activation | Native opener spy tests; actual browser activation is a parent CUA acceptance check, separate from passive smoke |
| Normal/narrow reader, unavailable state, keyboard/focus/refresh | Parent CUA acceptance at 1280×720 and approximately 640×610; no builder-side visual acceptance is claimed |

## Provisioning and limits

Only an authenticated coordinator's POST `/api/v1/tasks/:id/materials` creates material authority. There is no UI upload command, coordinate import, arbitrary file open, directory discovery, URL fetch, preview, provider action or agent launch. All authenticated roles can read the resulting opaque ID. The original Inbox/task material associations remain saved exactly as creation facts; uploaded associations are appended from the immutable registry in task-revision order.

Text accepts valid UTF-8 with at most 262144 Unicode characters, including empty content, and excludes binary controls except tab/CR/LF. The separate 1 MiB whole-JSON-request limit still applies; a maximum-character payload consisting entirely of four-byte characters may exceed that request budget once JSON overhead is included and is refused explicitly. Reads are bounded to 4×262144+1 bytes and never truncate. Missing/corrupt/unsupported content leaves saved task context and association/receipt history intact.

A failed or interrupted upload can leave an unreachable staging/hash blob. Reads require a committed registry ID; they do not scan or infer association from blob names. Automatic orphan cleanup is outside this story. Published blobs are retained on rollback because another association or retained backup may reference them. A publication interrupted while both hardlink names exist fails closed on the link-count check; cleanup/recovery of abandoned content requires a separate explicit maintenance feature.

Windows material upload/read is currently disabled, beyond being unverified. Existing workspace reads remain usable there and no material capability is advertised. Concrete follow-up: implement Windows anchored directory/file access with verified ACLs, reparse/hardlink/shortcut-alias and replacement-race refusal, durable no-replace publication, bounded cancellation, and then run the entire Windows native material/navigation matrix before enabling discovery. No Windows check or push was performed in this story.

Diagnostics remain the existing bounded process-private allowlisted events. No text, file path, URL or upload payload is added to technical diagnostics. Persistent diagnostics/export remain later work.

## Executed command evidence

Passed on macOS 15.7.7 arm64 against the final locally signed production bundle:

- `sh scripts/check.sh`: binding generation, pinned Node 24.22.2 dependency/type/test/build checks, 34 frontend passing test/subtest records, gofmt, vet, Go tests, Go race suite and production native build.
- `go test -json -count=1 ./...`: 394 passing test/subtest records, zero failures; 13 tested packages pass and five packages have no test files.
- `sh scripts/package-mac.sh` and `codesign --verify --strict 'bin/Personal Desk.app'`: pass.
- `python3 scripts/smoke-mac.py`: pass; genuine empty state, private credentials, clean quit and stable restart.
- `python3 scripts/smoke-recovery-mac.py`: pass; populated schema1-to4 upgrade/backup/restore, interrupted completion/resume, original revisions, credentials and saved material bytes retained.
- `python3 scripts/smoke-inbox-mac.py`: pass; exact retry, identity duplicate, both conflicts, role refusal, passive reads and restart with zero tasks.
- `python3 scripts/smoke-create-task-mac.py`: pass; atomic create, concurrent exact replay, normalized replay, terminal/key conflicts, role refusal and saved restart.
- `python3 scripts/smoke-task-details-mac.py`: pass; all-role facts, strict safe reads, passive hash/restart and zero source/image canary fetches. Canonical SHA-256: `bb8daa6ca5cba9c556e2d3f657f8211492a5e8c79d9fabdb550adf2ed9e644eb`.
- `python3 scripts/smoke-material-mac.py`: pass; coordinator upload, exact Unicode/all-role reads, retry/conflicts, registry-only authority, passive hash/restart, explicit known-missing state and zero automatic canary fetches. Canonical SHA-256: `02ccf09173d1472a4097f415a0fba0480d583c7fa5babd3d3ba4af950935b166`.
- `git diff --check`: pass. Published v1 schemas/examples and SQL1–3 have no changed bytes. All six smoke processes exited; a process-list check found zero active Personal Desk bundle executables.

Raw command logs are retained locally at `${TMPDIR:-/tmp}/personal-desk-story7-reviewed-check.log`, `${TMPDIR:-/tmp}/personal-desk-story7-reviewed-tests.json`, `${TMPDIR:-/tmp}/personal-desk-story7-reviewed-package.log` and `${TMPDIR:-/tmp}/personal-desk-story7-reviewed-smokes.log`. Synthetic smoke workspaces are retained and their locations are in the smoke log; no token is printed. The material smoke intentionally removes its blob at the end to verify unavailable content, while preserving its association/task/receipt facts.

An initial recovery smoke exposed an obsolete fixture assumption that `materials/` did not exist. The fixture now permits bootstrap's owned directory. An upload decoder regression also closes Go's lossy unpaired UTF-16 escape normalization; valid paired escapes, U+FFFD and literal backslash text stay valid. The full source check, fresh test run and all six bundle smokes above ran after those corrections.

Parent native/API/CUA verification is recorded separately by the parent build workflow. Normal/narrow visual layout, keyboard/focus, and actual deliberate OS-browser activation remain parent acceptance checks; the builder does not claim those interactions from these API smokes.

## Parent acceptance and matrix audit

The parent independently reran the full source/race/production check, fresh Go
JSON tests and package signing after the two native focus corrections and the review URL-token correction. Accepted
logs: `${TMPDIR:-/tmp}/personal-desk-story7-reviewed-check.log`,
`${TMPDIR:-/tmp}/personal-desk-story7-reviewed-tests.json`,
`${TMPDIR:-/tmp}/personal-desk-story7-reviewed-package.log` and
`${TMPDIR:-/tmp}/personal-desk-story7-reviewed-smokes.log`. All 394 Go test/subtest records across
13 tested packages and 34 frontend records passed; none failed or skipped. All
six final bundle smokes passed again. Baseline byte comparison independently
confirmed 23 released schema/SQL files unchanged.

| Plan matrix row | Executed passing coverage |
|---|---|
| Upload/roles/replay/conflicts/capacity | `TestAuthorizationReplayNormalizationAndRefusalBeforeBytes`, `TestMaterialUploadRegistryReceiptsOriginalFactsAndRestart`, `TestMaterialRollbackOrphansAndConcurrentKeys`, `TestMaterialCapacityAndPassiveMissingAssociation` |
| Publication failures and supported upgrades | `TestPublicationFaultsNeverInventReadableContent`, `TestMaterialCancellationAndWriteFaultsNeverGrantRegistryAuthority`, `TestFrozenV1UpgradeSkippedReleaseAndCurrentNoBackup`, `TestIndependentReleasedV2UpgradePreservesOriginalReceiptAndLegacySummaries`, `TestReleasedSchema3UpgradePreservesFactsAndBackup` |
| Authorized same HTTP/native content/revision/restart | `TestActualMaterialHTTPNativeParityRestartAndRoleBounds`; final material bundle smoke |
| Invalid/unknown IDs and no coordinate authority | `TestActualMaterialHTTPNativeParityRestartAndRoleBounds`; final material smoke and independent parent API cases |
| Known unavailable content and nulls | `TestMaterialContentValidationUsesCommittedRegistryRevision`; material smoke and parent private-file substitutions |
| Traversal/aliases/private files/nonregular/races | `TestDurableBoundsAndUnsafeLeaves`, `TestAnchoredAncestorsAndReplacement`, `TestFinderAliasIsInert`; parent actual symlink/hardlink/FIFO/directory substitutions |
| Text/size/UTF-8/cancellation/response budgets | `TestTextAndInputBoundaries`, `TestUploadRejectsLossySurrogateEscapesAndPreservesLiteralText`, `TestMaterialNativeBudgetsFaultsAndExpectedFailures`; parent exact262144-character and whole-request-byte limit cases |
| Selection/reordering/close/refresh/invalidation/teardown | All five named cases in `material-read-adapter.test.ts` passed, including same-ID close/reopen, stable error/retry and independent task/material freshness floors |
| Untrusted text and safe explicit navigation | All four cases in `safe-links.test.ts`, `TestSafeNavigationOnlyCallsOpenerForAcceptedUserTargets`; actual native/browser checks below |
| Passive state and zero automatic requests | Final material/task-detail smokes, independent parent canonical/restart checks, native observation below |
| Normal/narrow and keyboard/error states | Parent native tests below at 1280×720 and approximately 640×610 logical pixels |

`${TMPDIR:-/tmp}/personal-desk-story7-upgrade-smoke.py` exercised a real previous-bundle
schema3 workspace copy, preserving every original fact and receipt byte plus
credentials, verifying the consistent schema3 backup/integrity, then uploading
and reading after upgrade. SQLite-consistent static backups were opened with
`mode=ro&immutable=1` in this independent Python check. Retained workspace:
`personal-desk-story7-upgrade-qrx_a17c`.

`${TMPDIR:-/tmp}/personal-desk-story7-parent-smoke.py` used the actual signed bundle and
API-created tasks, never test-only registry insertion. It passed all-role exact
text/nulls, original Inbox/create receipts and creation history, later-revision
replay, normalized omitted/null labels, target/content conflicts, request/Unicode
bounds, private aliases/hardlinks/FIFO/binary/oversized substitutions, one winning
concurrent revision, 32-association capacity, passive full-SQL/credential hashes
and restart. Source/image canary requests remained zero. Retained workspace:
`personal-desk-story7-parent-gnt2b8v7`.

Parent native tests:

- `native-material-content`: actual saved Markdown/Unicode, plain/empty/missing
  content, unchanged context, opaque IDs and source coordinates; raw HTML and
  unsafe file/javascript syntax remain text. Exact 262144-character content wraps
  within the narrow drawer, while the footer remains accessible.
- `native-material-selection`: switching tasks and close/reopen clear old
  material selection. A deliberate API upload during a selected plain-material
  view advances workspace47→48 and task4→5, appends the new choice and preserves
  the selected material and its text with a fresh revision.
- `native-material-keyboard`: in-modal Tab traversal includes choices, links and
  Refresh. Enter on material Refresh preserves focus. Sources→Read Material now
  focuses the visible Content tab; Escape immediately closes to the originating
  task card. Confirmed at normal and narrow widths.
- `native-safe-navigation`: Sources opens the exact saved Inbox record with the
  all filter. Tab now reaches the source link in Inbox. Explicit click and,
  after the correction, keyboard Enter open the synthetic loopback URL in Google
  Chrome; its actual address/title/content were observed. Only the test tab was
  closed afterward. The private file coordinate remains inert.
- `native-passive-and-quit`: no canary request before explicit activation; only
  the activated `/explicit-navigation` and browser favicon were requested after
  it. Full-SQL canonical SHA-256 after the deliberate upload remains
  `aa5e4afc37f25cf852e2e25d7e088753b414de6e43fa305eba74e4e2fff3ed33`.
  Credentials remain identical; normal quit exits 0 and removes discovery.
  The later review regression below replaced the observation file after its own pass.

Initial CUA found and the same implementation agent corrected two defects before
review: hiding the focused Sources button lost Escape handling; implicit native
Tab traversal skipped the Inbox SafeLink. The accepted bundle was rebuilt and all
checks/smokes reran after the corrections; final CUA confirmed both fixes. Windows
material behavior remains disabled with the follow-up above; no Windows check
or release/push is claimed.

## Quick review correction and final native regression

The independent Quick review found one medium defect: unconditional terminal
punctuation trimming changed a saved balanced-parenthesis URL and disabled a bare
IPv6 URL. The same implementation agent removed the destructive trimming and
handles an enclosing Markdown parenthesis with a linear balanced scan. Four
focused link tests pass, including exact text reconstruction, IPv6, balanced
parentheses, Markdown wrappers and legitimate terminal URL punctuation. No review
finding was deferred.

After the full checks and all six signed-bundle smokes above passed again, the
parent uploaded a synthetic plain-text material through the coordinator API
(task5→6, workspace48→49). The native reader showed both complete links ending
in `/explicit-navigation/a(b)` and an active `https://[::1]` link. Clicking one
loopback link opened Google Chrome with the exact parenthesized address and the
expected fixture title/content. Only that test tab was closed. IPv6 activation
was not attempted; no fixture server listens there.

The final watcher recorded zero requests before explicit activation, then only
`/explicit-navigation/a(b)` and `/favicon.ico`, both after the activation marker.
Passive full-SQL SHA-256 remained
`6025c20e0997acae1302eb26d1a8207cc3ffb6553cc32b70a515611478250202`;
credentials were unchanged, normal quit exited 0 and discovery was removed.
`${TMPDIR:-/tmp}/personal-desk-story7-native-observation.json` records this final pass.
