#!/usr/bin/env python3
"""Actual Mac fixture bundle export/privacy/passive-state smoke.

The separate diagnostic_smoke build injects only the trusted picker result;
parent CUA acceptance separately exercises the real native Help/Save dialogs.
All roots, credentials, business data and outputs are isolated/synthetic.
"""
import hashlib
import json
import os
from pathlib import Path
import signal
import sqlite3
import subprocess
import tempfile
import time
import urllib.request
import zipfile

os.umask(0o077)
binary = Path("bin/Personal Desk.app/Contents/MacOS/personal-desk").resolve()
fixture = Path("bin/Personal Desk Diagnostic Fixture.app/Contents/MacOS/personal-desk").resolve()
parent = Path(tempfile.mkdtemp(prefix="personal-desk-export-")).resolve()
exports = parent / "exports"
exports.mkdir(mode=0o700)
active = []
archives = []

def fresh(name):
    root = parent / name
    root.mkdir(mode=0o700)
    return root

def launch(root, destination=None, mode=""):
    env = dict(os.environ, PERSONAL_DESK_WORKSPACE=str(root), PERSONAL_DESK_ADDRESS="127.0.0.1:0")
    if destination is not None:
        env.update(PERSONAL_DESK_EXPORT_FIXTURE_DESTINATION=str(destination), PERSONAL_DESK_EXPORT_FIXTURE_MODE=mode)
    output = parent / (root.name + "-" + str(len(active)) + ".output")
    stream = output.open("wb")
    process = subprocess.Popen([str(fixture if destination is not None else binary)], env=env, stdout=stream, stderr=subprocess.DEVNULL)
    stream.close()
    active.append(process)
    for _ in range(150):
        if destination is not None:
            for line in output.read_bytes().splitlines():
                try:
                    result = json.loads(line)
                except (json.JSONDecodeError, UnicodeDecodeError):
                    continue
                if isinstance(result, dict) and result.get("status") in ("exported", "cancelled", "failed"):
                    return process, result
        elif (root / "discovery.json").exists():
            time.sleep(1)
            return process, None
        if process.poll() is not None:
            raise AssertionError("native process exited before export/readiness")
        time.sleep(0.1)
    raise AssertionError("native export/readiness timeout")

def stop(process):
    if process.poll() is None:
        process.send_signal(signal.SIGTERM)
        assert process.wait(timeout=8) == 0

def request(root, path, body, key):
    endpoint = json.loads((root / "discovery.json").read_text())["endpoint"]
    token = json.loads((root / "credentials.json").read_text())["coordinator"]
    req = urllib.request.Request(endpoint + path, data=json.dumps(body).encode(), headers={"Authorization": "Bearer " + token, "Content-Type": "application/json", "Idempotency-Key": key})
    with urllib.request.urlopen(req, timeout=4) as response:
        assert response.status == 201
        return json.load(response)

def canonical(root):
    with sqlite3.connect("file:" + str(root / "workspace.sqlite") + "?mode=ro", uri=True) as db:
        names = [r[0] for r in db.execute("SELECT name FROM sqlite_schema WHERE type='table' ORDER BY name")]
        return [(name, db.execute('SELECT * FROM "' + name + '" ORDER BY 1,2').fetchall()) for name in names]

def inspect(path, forbidden=()):
    archives.append(path)
    with zipfile.ZipFile(path) as archive:
        assert archive.namelist() == ["manifest.json", "logs/diagnostics.jsonl", "reproduction-steps.txt"]
        assert sum(item.file_size for item in archive.infolist()) <= 30 << 20
        assert all(item.external_attr >> 16 & 0o777 == 0o600 for item in archive.infolist())
        assert archive.read("reproduction-steps.txt") == b""
        combined = b"\n".join(archive.read(name) for name in archive.namelist())
        for marker in [b"SYNTHETIC_SECRET", b"BUSINESS_CANARY", str(parent).encode(), b"/private/diagnostic-fixture", *forbidden]:
            assert marker not in combined, "excluded content in archive"
        logs = archive.read("logs/diagnostics.jsonl")
        assert not logs or logs.endswith(b"\n")
        events = [json.loads(line) for line in logs.splitlines()]
        manifest = json.loads(archive.read("manifest.json"))
        assert manifest["coverage"]["acceptedRecords"] == len(events)
        assert all(len(line) + 1 <= 16 << 10 for line in logs.splitlines())
        assert path.stat().st_mode & 0o777 == 0o600
        return manifest, events

try:
    root = fresh("saved")
    process, _ = launch(root)
    pending = request(root, "/api/v1/inbox/messages", {"source": "manual", "scope": "export-fixture", "messageId": "one", "kind": "user_request", "text": "SYNTHETIC_SECRET BUSINESS_CANARY"}, "receive")
    request(root, "/api/v1/inbox/messages/" + pending["message"]["id"] + "/resolutions", {"expectedMessageRevision": 1, "action": "createTask", "reason": "BUSINESS_CANARY", "task": {"kind": "request", "title": "SYNTHETIC_SECRET BUSINESS_CANARY"}}, "create")
    before = canonical(root)
    credentials = (root / "credentials.json").read_bytes()
    tokens = [value.encode() for value in json.loads(credentials).values()]
    stop(process)
    (root / "original.txt").write_bytes(b"SYNTHETIC_SECRET original file")
    # Backups are untrusted input. Malformed/unknown/oversized/partial records
    # must be omitted and reported without copying any original bytes.
    hostile = root / "logs" / "diagnostics.1.jsonl"
    hostile.write_bytes(b'{"schemaVersion":99,"secret":"SYNTHETIC_SECRET"}\n' + b'x' * ((16 << 10) + 1) + b'\n{"partial":"BUSINESS_CANARY"}')
    hostile.chmod(0o600)
    target = exports / "ready.zip"
    process, result = launch(root, target)
    assert result["status"] == "exported" and result["errorId"] is None
    manifest, _ = inspect(target, tokens)
    assert manifest["schemaStatus"] == "ready" and manifest["schemaVersionApplied"] == 4
    assert not manifest["coverage"]["complete"] and manifest["coverage"]["omittedRecords"] >= 2 and manifest["coverage"]["truncatedRecords"] >= 1
    assert canonical(root) == before and (root / "credentials.json").read_bytes() == credentials
    stop(process)
    process, result = launch(root, exports / "render.zip", "render")
    assert result["status"] == "exported"
    _, events = inspect(exports / "render.zip", tokens)
    assert any(e["code"] == "ui.render_fault" for e in events)
    assert canonical(root) == before and (root / "credentials.json").read_bytes() == credentials
    stop(process)
    process, result = launch(root, "cancel")
    assert result == {"status": "cancelled", "coverage": None, "errorId": None}
    stop(process)
    keep = exports / "existing.zip"
    keep.write_bytes(b"KEEP")
    process, result = launch(root, keep)
    assert result["status"] == "failed" and len(result["errorId"]) == 32 and keep.read_bytes() == b"KEEP"
    stop(process)
    process, result = launch(root, root / "refused.zip")
    assert result["status"] == "failed" and not (root / "refused.zip").exists()
    stop(process)
    owner, _ = launch(root)
    shared = {p.name: hashlib.sha256(p.read_bytes()).digest() for p in (root / "logs").iterdir()}
    process, result = launch(root, exports / "private.zip")
    assert result["status"] == "exported"
    manifest, _ = inspect(exports / "private.zip", tokens)
    assert manifest["sharedDiagnostics"] == "unavailable" and not manifest["coverage"]["complete"] and manifest["schemaStatus"] == "unknown" and manifest["schemaVersionApplied"] is None
    assert shared == {p.name: hashlib.sha256(p.read_bytes()).digest() for p in (root / "logs").iterdir()}
    assert canonical(root) == before
    stop(process)
    stop(owner)
    failed = fresh("failed-db")
    db = failed / "workspace.sqlite"
    db.write_bytes(b"SYNTHETIC_SECRET invalid database")
    db.chmod(0o600)
    original = db.read_bytes()
    process, result = launch(failed, exports / "failed-db.zip")
    assert result["status"] == "exported" and not (failed / "discovery.json").exists()
    manifest, events = inspect(exports / "failed-db.zip")
    assert manifest["schemaStatus"] in ("unavailable", "unsupported") and manifest["schemaVersionApplied"] is None
    assert any(e["code"] == "query.fault" for e in events) and db.read_bytes() == original
    stop(process)
    assert db.read_bytes() == original
    subprocess.run(["go", "run", "scripts/validate-diagnostic-export.go", *map(str, archives)], check=True)
    print("PASS native fixture export: ready/React failure/DB failure/private owner, cancellation, overwrite/protected refusal, excluded markers, exact schemas/budgets and passive SQL/receipts/credentials")
    print("Isolated synthetic workspace parent:", parent)
finally:
    for process in active:
        stop(process)
