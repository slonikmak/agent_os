# Saved task details verification

Story6 adds the frozen task-detail read to the existing board/Inbox design. It
changes no v1 schema, command receipt or SQL1–3 migration. The business facade
now exposes exactly GetWorkspaceSnapshot, GetTaskDetails and GetInboxMessage.
Material coordinates remain inert; no material reader or link opener is added.

## Named source matrix

| Case | Evidence |
|---|---|
| Real create → complete detail → restart; passive canonical facts | `TestTaskDetailsRealCreationPassiveConsistencyAndRestart` compares every task field, real timestamps, optional values, original submitter, coordinator/event reason and material associations with the create result. It compares all canonical tables before/after repeated reads and restart. `TestActualHTTPCreateNativeSavedViewsInvalidationAndRestart` compares native details with the real HTTP create result across restart. |
| One committed read revision | `TestTaskDetailsRevisionAndFactsShareOneCommittedRead` concurrently commits 97 revision/task-fact updates while reading 150 complete details and verifies each title belongs to its returned workspace revision. |
| Invalid/missing/legacy/cancelled/corrupt | `TestTaskQueryValidationSafeFailuresAndCancellation` rejects invalid IDs before storage and checks cancelled/error/envelope handling. `TestTaskDetailsMissingLegacyCancellationCorruptionAndDeterministicHistory` proves missing=`not_found`, legacy detail=`service_unavailable` with its board summary retained, caller cancellation, corrupt saved materials and deterministic tied-event ordering. |
| All authenticated roles and same saved facts | `TestCreateHTTPFullResultNormalizedReplayAndSavedReads` reads the actual created task as reader, worker and coordinator and compares the frozen Task and workspace revision. |
| HTTP confinement, request bounds, privacy | `TestTaskHTTPStrictReadConfinementAndRoutes` covers exact path shape, missing/invalid IDs, Host, Origin, auth/duplicate auth, version/duplicate version, query, Idempotency-Key, media/duplicate media, nonempty/oversized body and fresh server attempt IDs. `TestTaskHTTPFaultBudgetAndCorruptSavedResults` checks sanitized faults/panics, captured IDs, unavailable and explicit byte-budget failures. |
| Native mapping, budgets, privacy, lifetime | `TestTaskNativeMappingFaultPrivacyBudgetAndValidation` compares native/HTTP representations and covers malformed results, raw faults/panics, unknown errors, deadlines and response budgets. `TestTaskReadLifetimeCancellation` proves an in-flight native read cancels with workspace lifetime; the integrated create/restart test rejects reads after close. |
| Selection, close/reopen, overlapping refresh | `task-read-adapter.test.ts`: “task selection change, close and reopen discard every previous generation” and “superseded task refresh cannot replace a newer saved detail”. New selection has no previous selection's data. |
| Invalidation bursts and monotonic details | Frontend cases “task invalidation bursts coalesce and stale task rereads do not reread the workspace”, “a task detail below its previous revision is discarded even without invalidation”, and “task response identity and invalid revisions fail safely without automatic retry”. Existing snapshot/Inbox deferred-promise tests remain passing. |
| Stable errors in either completion order | Frontend case “a newer snapshot and a task failure settle in both completion orders, then explicit refresh retries” checks saved-detail preservation and bounded request counts. “concurrent Inbox stale completion cannot clear or retry a settled task error” proves the independent query schedules. |
| Bridge failure/teardown | Frontend case “task bridge exceptions preserve saved details and shutdown ignores all late results” proves safe fixed error text and no late publication after unsubscribe. |
| Frozen frontend mapping | `contracts.test.ts`: “task mapping preserves saved creation, original submitter, coordinator history, nulls and inert materials” compares the canonical example and rejects an unsupported applying role. |

## Commands and actual target

macOS 15.7.7 (24G720) arm64, Go 1.27.3 / Node 24.22.2 / Wails 3.0.0. The commands
ran from the repository root with the pinned tools on PATH.

`sh scripts/check.sh` passed binding generation (one service, three methods),
frontend type checking, 25 frontend tests, production frontend build, formatting,
Go vet, Go tests, Go race tests and the production native build. The parent reran
the entire check after the Tab-cycle/focus-return, Russian material labels, bounded
long-action footer and focus-preserving Refresh corrections; final log: `${TMPDIR:-/tmp}/personal-desk-story6-accepted-check.log`.

`go test -json -count=1 ./...` then passed 353 test/subtest cases across 11 tested
packages with no failures or skipped cases; final JSON:
`${TMPDIR:-/tmp}/personal-desk-story6-accepted-go-tests.json`.
`sh scripts/package-mac.sh` completed ad hoc signing and strict verification of
the final native rebuild; log: `${TMPDIR:-/tmp}/personal-desk-story6-accepted-package.log`.
`git diff --check` passed. SQL1–3 and the frozen v1 schemas have no diff.

All five actual-bundle scripts passed serially, each with an isolated synthetic
workspace and clean shutdown:

| Script | Final retained workspace suffix |
|---|---|
| `python3 scripts/smoke-mac.py` | `personal-desk-smoke-4efgq4sx` |
| `python3 scripts/smoke-recovery-mac.py` | `personal-desk-recovery-smoke-upvzqsvn` |
| `python3 scripts/smoke-inbox-mac.py` | `personal-desk-inbox-smoke-zvr5on27` |
| `python3 scripts/smoke-create-task-mac.py` | `personal-desk-create-task-smoke-p03pivih` |
| `python3 scripts/smoke-task-details-mac.py` | `personal-desk-task-details-smoke-k2aa8zi5` |

The new detail smoke creates a real task with saved optional values and inert
URL associations, compares its complete facts as all three roles, checks unique
attempt IDs and strict safe failures, and verifies identical complete details
after restart. All canonical tables/receipts retain SHA-256
`6a22029a51b0ebec08f9d6614fec4a103aa7868f689077d53af2796503d52121`
through passive reads and restart. Credentials retain the same hash, and the
local source/image canary observes zero fetches. The Go real-creation tests and
existing updated create-task smoke additionally prove truthful absent optionals.

## Design and native interaction evidence

The implementation retains the adopted CSS/token/theme order, 36px titlebar,
510px aligned drawer, six statuses and kind/category mapping. Cards are keyboard
buttons; confirmed Inbox resolutions open the saved task ID. Drawer Content,
History and Sources use saved facts only. Tabs have ARIA ownership, roving focus,
Arrow/Home/End activation, a modal focus trap, Escape/close/backdrop dismissal and
focus return. Related-message navigation sets the all filter before selecting the
exact saved Inbox ID; sidebar Inbox navigation still resets to unprocessed.
Optional task summary/action/actor are shown only when saved. Dates remain МСК.

The parent independently exercised the signed bundle in a synthetic workspace
`personal-desk-story6-parent-c8tah4sd`: all-role complete task equality, nullable
combinations, 24 concurrent reads, strict boundaries, unchanged original command
receipts across restart, and legacy-detail 503 with its board summary retained.

Actual CUA checks of the final bundle passed at 1280×720 and approximately
640×610 logical pixels, with the existing 36px chrome and 510px drawer:

- `native-drawer-keyboard`: full forward Tab cycle Close → active tab → visible
  panel → Refresh → Close, reverse traversal, inactive tabs skipped, Escape
  returns to the actual opening card. The next card is Tab-reachable and opens
  with Enter. Close-button and backdrop dismissal also restore the opening card;
  Escape from the confirmed Inbox link restores that link.
- `native-drawer-tabs-and-fields`: ArrowRight, Home, End and wrapping ArrowLeft
  select the correct panels. Real creation/history/reason, submitting worker and
  applying coordinator are distinct. Summary/action/actor disappear when absent;
  an actor alone does not invent an action. Narrow history/material content
  scrolls above the fixed footer; IDs and Russian availability labels are legible.
- `native-related-inbox`: Sources opens the exact applied Inbox ID under “Все
  сообщения” despite two unrelated pending records. The confirmed resolution
  opens the same task. Sidebar Inbox resets “Необработанные”; proposed taskRef
  remains labelled as a proposal with zero saved resolutions and no task link.
- `native-live-and-failure`: while the original task remains selected, explicit
  synthetic API receive/create commands advance workspace 10→12. The same task
  displays revision12 and closing reveals the newly created fifth board task.
  Legacy missing-creation details show stable `service_unavailable` after an
  explicit retry, without previous task content; closing preserves the board.
- `native-passive-and-shutdown`: the full SQL dump matches its baseline through
  initial navigation. After the two deliberate API writes, subsequent reads,
  task/screen refreshes and navigation retain canonical SHA-256
  `2343dfabfa5dd6a4a7288470f1b946e55f5d26ad507e75bc8fec5bc780e59690`.
  Private credentials retain their hash, source/image canary fetches=0, normal
  quit exits0 and removes discovery. Observation:
  `${TMPDIR:-/tmp}/personal-desk-story6-final-native-observation.json`.

All nine plan matrix rows have executed passing automated or native manual
evidence above. No provider, image or file coordinate was opened. Windows
remains unverified in this Mac-only story.

## Review corrections and final boundary recheck

Quick inspection found that a valid 2048-character nextAction could expand the
fixed footer beyond the narrow viewport. The parent reproduced this with an
actual API-created task before the correction. The footer now caps its height at
35% and scrolls, wraps long text and keeps controls from shrinking; the body
retains scrollable space. Native CUA on the accepted bundle confirmed visible
tabs/details at approximately 640×610, scrolling to the full action ending and
Refresh, and Tab bringing Refresh into view. The original short footer at
1280×720 retains its existing layout.

The same native recheck exposed disabled Refresh losing keyboard focus to outer
HTML during loading. It now uses aria-disabled and rejects activation while
loading. Final CUA confirmed Enter on Refresh retains focus on that button and
Escape immediately closes to the exact opening card, for both long and short
actions. All full checks, 353 fresh Go cases, 25 frontend cases and five bundle
smokes above ran again after both corrections. No findings were deferred.

The additional isolated task advanced workspace 12→14 only through explicit
receive/create API calls. The accepted native view/refresh/quit check retained
full-SQL SHA-256
`d65d5f99b88891c2d33a380d32a7dbd4b1f937e66ea95615df4d4a9d1a2fcb94`,
unchanged credentials, zero canary fetches and clean exit 0/discovery removal. Observation:
`${TMPDIR:-/tmp}/personal-desk-story6-long-action-observation.json`.
