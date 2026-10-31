# Story 9 diagnostic export verification — 2026-10-30

Target: macOS 15.7.7 arm64, Go 1.27.3, Node 24.22.2, Wails CLI/module/runtime 3.0.0. Baseline: `bcca1138f7d4af8438f4451038bb84a61556e7e4`. Business inputs and exported archives use isolated synthetic workspaces. Windows remains unverified; Windows persistent diagnostics and destination access remain disabled. No push or release is performed.

## Implemented behavior

Help → Export diagnostics… uses trusted native Save and native safe feedback independently of React and database readiness. The operational `DiagnosticFacade.ExportDiagnostics()` takes no arguments and returns the frozen typed outcome. Cancellation has null coverage/errorId and creates no file/fault. Admission permits one export and refuses repeats without another picker or bug event; a native callback guard also bounds repeated menu actions through feedback completion.

The existing capture writer owns export snapshot barriers as well as appends/rotation/close. Earlier queued deliveries settle before a fixed-inventory snapshot reads pinned handles; capture retains its nonblocking bounded queue. Internal retained-event identities track delivery independently of request/error IDs. Successfully delivered events are not duplicated from private memory. Ambiguous failed deliveries are counted as omitted instead of being appended again. Ring/queue loss, unknown prior loss, repairs, unavailable readers, malformed/oversized/unknown records, partial records and archive exhaustion set incomplete coverage. No snapshot lock/handle is held while Save waits.

Export decodes every complete source record with the frozen diagnostic schema and serializes only the typed allowlist. The ZIP contains exactly regular private entries `manifest.json`, `logs/diagnostics.jsonl`, and empty `reproduction-steps.txt`. Record size is at most 16 KiB, total uncompressed content at most 30 MiB. The manifest contains typed runtime values and startup-cached ready/unavailable/unsupported/unknown schema facts; unknown values stay null. Export reads no SQLite, materials, credentials, original files, environment dump, screenshot or directory-discovered file list, and performs no upload or domain mutation.

Destination traversal opens every ancestor no-follow, checks aliases and protected directory identities, and rechecks the selected parent before publication. Configured/default workspaces and the application subtree are protected, including case variants and renamed directory identities. A new 0700 staging directory and 0600 archive are held by handles; macOS inherited ACLs are removed through those descriptors before creating children or writing bytes. Atomic hard-link publication refuses an existing leaf. Source/published inode checks and identity-checked cleanup prevent directory/staging-name replacement from redirecting publication or deleting independent replacements. Unsupported filesystems/destinations fail closed. Write/sync/publish errors and panics remove only owned staging/publication entries; an early descriptor-allocation failure also cleans the known empty stage. If its identity cannot be established or its name was replaced, cleanup preserves that unknown entry.

Maintenance has a lifetime separate from `stopDomain`/Halt. Shutdown rejects new work, cancels/drains accepted file work, ignores late Save completion and then closes diagnostics before unlocking. Business reads/API remain gated on normal readiness. Diagnostics cannot change a committed receipt/result.

## Named matrix evidence

| Frozen plan row | Evidence |
|---|---|
| Healthy new export; fixed ZIP and unchanged domain state | `TestMaintenanceExportCachedSchemaSurvivesHaltAndUnavailableDB`; export bundle smoke ready case; schema validator |
| Cancel/repeated invocation, no file/bug, one picker | `TestMaintenanceCancelBusyLatePickerAndFailureDoNotTouchSharedFiles`; fixture cancel case; parent native Help/Save checks |
| React/DB failure and Halt; cached facts, no reopen | Same cached-schema/Halt test; export bundle smoke automatic real React failure and invalid DB shell |
| No ownership/shared unavailable | Same cancel/private test; competing-owner export smoke compares owner logs and canonical SQL |
| Concurrent capture/rotation/export/quit and identity | `TestSnapshotDrainsEarlierCaptureUsesIdentityAndReportsLoss`, `TestArchiveBudgetAndQueueOverflowRemainBounded`, `TestCaptureRotationExportAndCloseDrainBeforeUnlock`; race detector |
| Invalid/unknown/oversized/partial and secret-marked input | `TestExportRevalidatesReserializesAndCountsHostileRecords`; hostile fixed-backup smoke; frozen-schema validator |
| Ring/queue loss, unreadable/unknown availability, budget | Snapshot/loss and budget tests; `TestExportPinnedInventoryAndUnreadableSharedFallback`, `TestUnknownLossNeverClaimsCompleteCoverage`; explicit incomplete manifests |
| Existing/protected paths, case/ancestor/leaf aliases | `TestSafeDiagnosticDestinationAliasesProtectedIdentityAndIOFailures`, `TestDiagnosticPublicationRejectsStagingAndParentReplacement`; bundle overwrite/protected refusal |
| Write/sync/publish failure, panic, late Save; shutdown and original results | Safe-destination injected write/closed-fd/racing-leaf/panic cases; `TestPublishedDiagnosticSyncFailureAndPanicCleanOnlyOwnArchive`; cancel/late-picker and capture/close tests; committed-result/receipt regressions and passive bundle SQL/credentials comparison |

## Commands and results

The following final commands ran successfully. Parent real native Help/Save/layout acceptance is recorded separately and is not inferred from fixture picker injection.

- `sh scripts/check.sh`: pass, including three-service/eight-method binding generation, all 42 frontend tests, typecheck/build, formatting/vet, Go tests/race and production native build.
- `go test -json -count=1 ./...`: pass, 453 test/subtest pass events across 14 tested packages, zero failed/skipped test events.
- `sh scripts/package-mac.sh` and `sh scripts/package-diagnostic-smoke-mac.sh`: pass, separate locally ad hoc signed and strictly verified normal/fixture bundles.
- Seven existing Mac smokes plus `python3 scripts/smoke-diagnostic-export-mac.py`: all pass. Four exported ZIPs passed frozen-schema/budget validation; ready/React-failure/DB-failure/private-owner exports, cancellation, overwrite/protected refusal, excluded markers and canonical SQL/receipts/credential comparisons passed. All smoke native processes exited.
- Released schema/example/SQL byte comparison to baseline: all 52 files byte-identical. `git diff --check`: pass.

Final post-review evidence logs: `${TMPDIR:-/tmp}/personal-desk-story9-reviewed-{check,package,fixture,smokes}.log`, `${TMPDIR:-/tmp}/personal-desk-story9-reviewed-go-tests.jsonl` and `${TMPDIR:-/tmp}/personal-desk-story9-reviewed-summary.json`. All eight smokes ran again against the rebuilt bundles. The independent byte audit additionally checked the unchanged migration catalog manifest (53 files total). Commands used the pinned Wails and Node tools on PATH. Earlier `${TMPDIR:-/tmp}/personal-desk-story9-*.log` files retain intermediate evidence.

The test fixture's `PERSONAL_DESK_EXPORT_FIXTURE_DESTINATION` injects a picker result only in the `diagnostic_smoke` build; `cancel` injects cancellation. `PERSONAL_DESK_EXPORT_FIXTURE_MODE=render` starts the fixture at its real React rendering fault and waits for the bounded production reporter before export. Without these switches, the fixture still uses the actual native Save dialog. None of these switches or fault controls compiles into normal production binaries/assets. The smoke validates archive content through `go run scripts/validate-diagnostic-export.go` using the unchanged Go schema validator.

## Parent native acceptance

The parent independently used the actual Help menu and system Save panel, with fixture picker switches unset. Cancel returned to the same saved view; repeated invocation remained usable. A new archive exported successfully. Even after macOS Replace confirmation, an existing synthetic file remained byte-identical and the app returned a safe failure with errorId. A new destination inside the workspace was refused. Native refusal feedback was readable with the main window at 1280×720 and approximately 640×600.

The normal failed-database shell exported through actual Save with schemaStatus=unsupported and a null applied version. The original invalid database bytes remained identical; no discovery endpoint appeared, shutdown returned 0 and the session marker was clean. The fixture's real Render fault produced errorId `6523967e8901c1670f66eb394a4c2a6d`; native Save cancellation and export remained usable while its React fallback stayed visible. That exact ID appeared in the validated ZIP. Shutdown returned 0 with unchanged canonical SQL/receipts and credentials, clean marker and discovery removal.

Parent ZIP scans excluded synthetic business/database/private-path markers; frozen-schema validation passed. Normal canonical SQL SHA-256 stayed `771991d8f1b74a31d1d253c867202b3eac95cb3d1e963e2d165244a9b20106ff`; fixture SHA-256 stayed `d5245fcdfc25dc244d0c6efed3eed5e6af220227b3d2bd7539cd4681666891cd`. The normal post-exit Python SQLite mode=ro probe required immutable=1 after journal teardown; the independent immutable read confirmed the same facts. One post-exit CUA observation automatically relaunched the ordinary app without the synthetic environment; it was immediately closed without commands or export. Subsequent exit checks observed the test process only. Parent harness/results are `${TMPDIR:-/tmp}/personal-desk-story9-parent.py`, `-failed-result.json` and `-fixture-result.json`.

After review corrections, the parent used actual Help/Save again with the final normal bundle and a synthetic destination inheriting an everyone allow ACL. The resulting ZIP had mode0600 and no ACL entries; the selected parent retained its original ACL. Frozen-schema/privacy checks passed, no staging entry remained and canonical SQL SHA-256 stayed `e680360792115d892ea26d3e74639fd38f7d56ca0b9baf2dd16489d330fb1aa3`. Credentials and the existing sentinel file remained identical; quit returned 0, discovery was removed and the marker was clean. Evidence: `${TMPDIR:-/tmp}/personal-desk-story9-parent-acl-result.json` and `${TMPDIR:-/tmp}/personal-desk-story9-parent-archive-validation.log`.

## Review

Quick review found inherited macOS ACL exposure and an early-stage descriptor-failure cleanup gap. Both were fixed with narrow descriptor/ownership changes. `TestDiagnosticExportStripsInheritedMacAllowACLsBeforeWriting` and both cases of `TestDiagnosticEarlyStageOpenFailureCleansOnlyCreatedEmptyDirectory` ran in the final full and race suites. No finding was deferred. Native ACL enforcement requires cgo on macOS; no-cgo destination access fails closed.

## Limits

Rotated history/private fallback are bounded diagnostic availability, not business audit history. Unknown loss remains incomplete even when no exact omitted count exists. The exporter cannot run after fatal/native process failure. Kernel filesystem calls have no hard completion deadline; shutdown waits for accepted I/O to settle before ownership release. Actual native dialog behavior and design/layout are separate acceptance evidence. Windows native behavior, persistence and safe destination access are unverified and disabled.
