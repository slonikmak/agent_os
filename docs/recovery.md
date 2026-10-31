# Offline schema recovery

The application embeds its ordered SQL catalog. Goose history is the sole schema
version authority; content and entity revisions are separate. Every SQL step and
its history entry commit together. A later failed step leaves earlier commits
intact and stops normal queries and HTTP. Production contains the immutable
migration catalog 1–4: the original workspace, Inbox originals, task creation and
material attachment registry. Recognized v1–v3 prefixes receive a verified
pre-upgrade backup before their pending steps run. A current v4 database has no
pending upgrade. Future chains are also exercised with private test catalogs.

Before any upgrade of a supported existing database, startup creates a verified
SQLite online backup, including committed WAL pages, and durably registers its
manifest. Backup failure stops the upgrade before DDL. A durable upgrade intent
associates the original registered point with one unfinished attempt. Retries
validate history and reuse that point. Successful migration or explicit restore
retires the attempt; a later upgrade after returning to an older schema captures
a new point, including work saved after that return. Every point remains retained.
Current-schema startup creates no backup.
Unknown, newer, missing or non-prefix history and schema drift are refused.

Quit Personal Desk before maintenance. Use the executable belonging to the
application version whose exact schema the selected backup contains. On macOS:

```sh
app="./bin/Personal Desk.app/Contents/MacOS/personal-desk"
export PERSONAL_DESK_WORKSPACE="/absolute/private/isolated-workspace"
"$app" maintenance backup
"$app" maintenance list-backups
"$app" maintenance restore <backupId>
"$app" maintenance resume <restoreId>
```

Replace the angle-bracket examples with a registered 32-character lowercase hex
ID returned by the commands. Paths are never accepted as backup/restore inputs.
Omit the workspace override to use the normal per-user root. The root must exist
for maintenance; its parent/root permissions and service files must pass the same
private-file checks as startup. The commands acquire the same exclusive lock and
exit without starting Wails, HTTP, queries or migrations. A competing owner gets
`workspace_owned` and its files remain untouched. Successful results are bounded
JSON with `status: "ok"`; failures expose a fixed code without paths, SQL or data.
List output is capped at 1024 entries and reports `restoreId` while a valid intent
is pending. Failure to list a corrupt/incompatible registry is explicit.

`restore` validates the selected registered image, hash, integrity and exact
supported schema before replacement. It makes and verifies a private staging
copy, records the original main/WAL/SHM inventory in a durable intent, quarantines
that complete set without replacing existing files, and installs the staged
image. Validation connections close before moves. Image validation uses immutable
read-only connections so it creates no stale WAL/SHM sidecars. Credentials and
materials stay in place. After verification, a completion record is published
without replacing evidence while the durable startup gate remains in place.
Only after required directory syncs does recovery retire that gate. During active
completion, a late sync failure retains or reestablishes the intent; explicit
resume reconciles matching intent/completion records and finishes the syncs.
`completed.json` remains in the retained recovery directory.

If interrupted, normal startup shows `recovery_required` before creating/opening
SQLite and cannot expose normal queries or HTTP. Run `list-backups` to obtain the
active restore ID, then explicitly run `resume` with that ID. Resume validates
the selected image, inventory and each observed file location; it completes each
move idempotently. Repeating resume after completion validates the target schema
and finishes required directory syncs. It reports a sync failure without reviving
an obsolete replacement intent, preserving any later saved work. It never
reapplies migrations or starts the app. Start the desired
compatible application separately after successful recovery.

Keep these files until recovery has been independently verified:

- `backups/<backupId>.sqlite` and its `.json` manifest: immutable registered image
  and source schema, chain prefix, integrity digest and upgrade-target identity.
- `restore-intent.json`: active replacement inventory and registered IDs.
- `upgrade-intent.json`: the recovery point for an unfinished upgrade attempt;
  completed/returned attempts retain their records under `recovery/upgrade-*.json`.
- `recovery/<restoreId>/`: staging image, original main/WAL/SHM quarantine and the
  completed intent. Interrupted preparation can leave an unregistered image or
  staging directory; it is retained for inspection and never selected implicitly.

No automatic retention deletion, Down, reset, force, history edit or arbitrary
backup-path option exists. A deterministically defective pending SQL script
continues to fail on retry. Preserve the workspace and recovery points for an
explicitly reviewed recovery release/procedure; adding a later migration cannot
skip the defective step. A corrupt image, malformed intent, unsafe private file,
collision or sync failure produces a fixed failure and retains available evidence.

From the repository root, reproduce the isolated Mac checks with:

```sh
sh scripts/check.sh
go test -json -count=1 ./... > ${TMPDIR:-/tmp}/personal-desk-story5-go-tests.json
sh scripts/package-mac.sh
python3 scripts/smoke-recovery-mac.py
```

The smoke uses synthetic populated v1 data in a newly created private temporary
root, invokes the built executable, and replays the installed-but-uncompleted
intent boundary from a real completed record. Go subprocess tests actually kill
the recovery process at every durable move/restore boundary. See
[the verification record](recovery-verification.md) for commands, test names and
target limits. Windows native verification remains pending.
