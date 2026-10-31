# Story 1 verification — 2026-10-29

Implementation target: macOS 15.7.7 (24G720), arm64; Go 1.27.3; Node 24.22.2; Wails CLI/module/runtime 3.0.0. Locked storage dependencies: Goose 3.29.0, modernc SQLite 1.61.0 with libc 1.79.0, x/sys 0.49.0. The selected Goose/driver require that x/sys version.

## Source and build results

The complete `scripts/check.sh` run passed with pinned Node/Wails on PATH. It executed binding generation, `npm ci`, frontend type checking, frontend tests, production frontend build, an empty `gofmt -l .`, `go vet ./...`, `go test ./...`, `go test -race ./...`, and the native production build. Bindings report one service, one query method, seven DTO models and one invalidation event.

The frontend's four tests passed: subscription before read/delayed response ordering; coalesced invalidations/freshness floors; failed read retaining the last snapshot; teardown rejecting late responses. After the responsive/accessibility correction, frontend production build/type checking and all four tests passed again.

A final fresh `go test -json -count=1 ./...` after review fixes passed all 46 named test/subtest events across eight tested packages, with zero failed or skipped test events. The main package has no tests. The full run output was retained for the parent acceptance audit at `${TMPDIR:-/tmp}/personal-desk-story1-final-go-tests.json`; reproduce that command to produce another audit file.

Independent inspection found two defects, both corrected: SQL preflight now matches the literal reserved `sqlite_` prefix, and POSIX candidate file opens are nonblocking before regular-file validation. `TestLegalSQLitePrefixObjectsAreRefusedWithoutChanges` checks legal unknown table/view/index/trigger names and unchanged database bytes. `TestFIFOStartupRefusalIsBoundedAndReleasesLock` checks five service-file paths in bounded child processes and proves lock reacquisition before child exit. The parent reran the complete check script, Windows cross-compilation and native Mac bundle smoke after these fixes; all passed.

Both commands below passed on this Mac. They establish compilation only:

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags production -ldflags '-H windowsgui' -o ${TMPDIR:-/tmp}/personal-desk-windows.exe .
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/bootstrap -o ${TMPDIR:-/tmp}/personal-desk-windows-bootstrap.test.exe
```

The native Mac build passed with `CGO_CFLAGS=-mmacosx-version-min=15.0`, `CGO_LDFLAGS=-mmacosx-version-min=15.0` and `MACOSX_DEPLOYMENT_TARGET=15.0`. The local `.app` bundle passed ad hoc signing/verification. This is a local build artifact, not a distributed release.

## Frozen edge-case matrix

Every test named below passed on the Mac; none was skipped.

| Matrix row | Executed evidence |
|---|---|
| Fresh private workspace | `TestFreshRestartAndReadOnlyPools`, `TestNativeAndHTTPReadSameWorkspaceRestartAndQuit`, `TestSnapshotCountsShareReadTransaction` prove persisted positive identity/revision, real empty arrays/counts, read-only pooled connections and one consistent transaction. |
| Same workspace restart | `TestFreshRestartAndReadOnlyPools`, `TestNativeAndHTTPReadSameWorkspaceRestartAndQuit` compare the complete saved snapshot and unchanged credentials across restart. |
| Second owner, occupied port, unsafe/symlink/private setup | `TestCompetingOwnerCannotTouchWorkspace` exercises a separate process and unchanged owner files; `TestOccupiedPortIsExplicitAndUnchanged`, `TestUnsafeFilesAndSymlinkSetupRejected`, `TestSymlinkAncestorRejectedBeforeCreatingState`, `TestInvalidCredentialsStayIntact` prove explicit refusal and preservation. |
| Wrong bearer/Host/browser Origin | `TestAuthenticationConfinementAndDiscovery` proves rejection before query and server-generated request IDs. |
| Unknown/newer/unrecognized database | `TestUnsupportedDatabasesStayUnchanged`, `TestUnknownSchemaObjectsAreRefusedWithoutHistoryChanges`, `TestFailedStartupReleasesOwnershipAndDoesNotResetUnknownDB` prove no reset/history mutation, no advertised API, cleared stale discovery and released ownership. |
| Query fault/panic/read failure | `TestFaultPrivacyBoundsAndUnknownRoutes`, `TestFacadeOwnsFaultsAndExposesOnlyQuery`, `TestServiceSafeFailuresAndCancellation`, `TestFallbackIsBoundedAndDropsUntrustedFields`, plus the frontend failed-read test, cover safe typed envelopes/IDs, no raw secret/path/error output and the retained last snapshot. |
| Quit/cancellation | `TestNativeAndHTTPReadSameWorkspaceRestartAndQuit`, `TestCancellationReachesQuery`, `TestServiceSafeFailuresAndCancellation` prove listener/discovery cleanup, reacquisition and propagated cancellation. |

`TestPublishedExamplesMatchImplementedDTOs`, `TestTransportMappingsAgreeAndNullsAreExplicit` and `TestEmbeddedMigrationsMatchFrozenManifest` additionally check the frozen wire examples, transport parity and embedded migration identity.

## Native execution

`python3 scripts/smoke-mac.py` passed against the final bundle: native launch, authorized empty saved reads, unchanged passive rereads, quit removing discovery/stopping its listener, restart preserving identity/revision/credentials, and a second clean quit. The isolated synthetic workspace was retained under the system temporary directory; no token was printed.

The parent agent separately exercised the native 1280×720 board, refresh, filters, Inbox and source view; narrow Inbox/source/compact sidebar; fixed the narrow header overflow and confirmed the fix. Authorized HTTP snapshots before/after native navigation/refresh/filter were identical. Native close/Cmd+Q confirmed process exit, removed discovery and connection refusal. The final native keyboard retest passed: End exposed the waiting/done columns; Home then Right returned to the queue/executing columns. The focused board region showed its outline and refresh remained bounded. The direct key handler acts only when the region itself is the event target, preserving child-control keyboard behavior.

The parent also opened a real unrecognized database in an isolated root. The native shell showed `database_unsupported`, navigation/refresh remained usable and Copy changed to «Скопировано». The database SHA256 stayed identical and neither discovery nor credentials was created.

The CUA app accessor can reopen a closed app using its default workspace; the parent detected and closed that extra window. Further native acceptance should launch the executable explicitly with a resolved isolated root before acquiring the CUA app handle.

## Explicit limits

Windows native UI, ACL/lock/runtime and target acceptance have not run here. The Windows job in `.github/workflows/desktop.yml` is authored but has not executed on GitHub; it is not Windows 11 installed-app acceptance. Installer/release distribution, domain mutations, full upgrade backup/recovery, persistent diagnostic rotation and ZIP export remain later stories. Existing databases requiring migration are refused rather than upgraded without that recovery protocol.
