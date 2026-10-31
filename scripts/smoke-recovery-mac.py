#!/usr/bin/env python3
"""Synthetic offline backup/restore and interrupted-completion smoke on macOS.

Run from the repository root after check.sh and package-mac.sh. Only a new private temporary
workspace is touched. Exact process-kill boundaries are covered by Go tests;
this executable smoke replays the durable installed-but-uncompleted state.
"""
import hashlib
from contextlib import closing
import json
import os
from pathlib import Path
import signal
import sqlite3
import subprocess
import tempfile
import time
import urllib.request

os.umask(0o077)
binary = Path("bin/Personal Desk.app/Contents/MacOS/personal-desk").resolve()
root = Path(tempfile.mkdtemp(prefix="personal-desk-recovery-smoke-")).resolve()
environment = os.environ.copy()
environment.update(PERSONAL_DESK_WORKSPACE=str(root), PERSONAL_DESK_ADDRESS="127.0.0.1:0")
database = root / "workspace.sqlite"
with closing(sqlite3.connect(database)) as connection:
    connection.executescript(Path("internal/testsupport/migrationfixture/frozen-v1.sql").read_text())

def maintenance(*arguments):
    result = subprocess.run([str(binary), "maintenance", *arguments], env=environment,
                            capture_output=True, text=True, timeout=35, check=True)
    outcome = json.loads(result.stdout)
    assert outcome["status"] == "ok", "maintenance did not succeed"
    assert not (root / "discovery.json").exists(), "offline maintenance advertised HTTP"
    return outcome

def launch(ready=True):
    process = subprocess.Popen([str(binary)], env=environment, stdout=subprocess.DEVNULL,
                               stderr=subprocess.DEVNULL)
    if not ready:
        time.sleep(2)
        assert process.poll() is None, "native recovery shell exited"
        assert not (root / "discovery.json").exists(), "recovery shell started HTTP"
        return process
    for _ in range(100):
        if (root / "discovery.json").exists():
            time.sleep(1)
            return process
        if process.poll() is not None:
            raise AssertionError("native app exited before readiness")
        time.sleep(0.1)
    process.terminate()
    process.wait(timeout=8)
    raise AssertionError("native readiness timeout")

def snapshot():
    discovery = json.loads((root / "discovery.json").read_text())
    credentials = json.loads((root / "credentials.json").read_text())
    request = urllib.request.Request(discovery["endpoint"] + "/api/v1/workspace",
                                    headers={"Authorization": "Bearer " + credentials["reader"]})
    with urllib.request.urlopen(request, timeout=3) as response:
        return json.load(response)

def stop(process):
    process.send_signal(signal.SIGTERM)
    process.wait(timeout=8)
    assert not (root / "discovery.json").exists(), "discovery survived quit"

def digest(path):
    return hashlib.sha256(path.read_bytes()).digest()

process = launch()
try:
    original = snapshot()
    assert original["workspaceRevision"] == 42
    assert original["tasks"][0]["title"] == "Saved synthetic task"
    credentials_hash = digest(root / "credentials.json")
    stop(process)
    material = root / "materials"
    material.mkdir(mode=0o700, exist_ok=True)
    (material / "synthetic.txt").write_text("Retain this synthetic material")
    material_hash = digest(material / "synthetic.txt")
    manual_backup = maintenance("backup")
    backup_id = manual_backup["backupId"]
    assert manual_backup["schemaVersion"] == 4
    registered = maintenance("list-backups")["backups"]
    assert backup_id in [item["id"] for item in registered]
    assert any(item["schemaVersion"] == 1 for item in registered), "v1 pre-upgrade recovery point was not retained"
    with closing(sqlite3.connect(database)) as connection:
        assert connection.execute("SELECT version_id FROM goose_db_version ORDER BY id").fetchall() == [(0,), (1,), (2,), (3,), (4,)]
        assert connection.execute("SELECT count(*) FROM inbox_payloads").fetchone()[0] == 0, "legacy original was fabricated"
    image = root / "backups" / (backup_id + ".sqlite")
    backup_hash = digest(image)
    with closing(sqlite3.connect(database)) as connection:
        connection.execute("UPDATE tasks SET title='After backup'")
        connection.execute("UPDATE workspace_meta SET workspace_revision=43")
        connection.commit()
    restore_id = maintenance("restore", backup_id)["restoreId"]
    completed = root / "recovery" / restore_id / "completed.json"
    assert completed.exists()
    # Recreate the real durable boundary after installation and before intent
    # retirement. All paths/bytes come from this executable's completed record.
    intent = root / "restore-intent.json"
    completed.rename(intent)
    descriptor = os.open(root, os.O_RDONLY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)
    before_blocked_start = digest(database)
    process = launch(ready=False)
    stop(process)
    assert digest(database) == before_blocked_start, "blocked startup changed SQLite"
    assert maintenance("list-backups")["restoreId"] == restore_id
    maintenance("resume", restore_id)
    maintenance("resume", restore_id)
    assert digest(image) == backup_hash, "selected recovery point changed"
    assert digest(root / "credentials.json") == credentials_hash
    assert digest(material / "synthetic.txt") == material_hash
    process = launch()
    assert snapshot() == original, "restored saved facts/revisions changed"
    stop(process)
    print("PASS populated native backup/list/restore, blocked interrupted completion, explicit repeatable resume, saved facts and private files")
    print("Isolated workspace retained:", root)
finally:
    if process.poll() is None:
        process.terminate()
        process.wait(timeout=8)
