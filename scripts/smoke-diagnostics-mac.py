#!/usr/bin/env python3
"""Real Mac bundle diagnostic lifecycle smoke; only isolated synthetic roots."""
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

binary = Path("bin/Personal Desk.app/Contents/MacOS/personal-desk").resolve()
parent = Path(tempfile.mkdtemp(prefix="personal-desk-diagnostics-")).resolve()

def fresh(name):
    root = parent / name
    root.mkdir(mode=0o700)
    return root

def launch(root, ready=True):
    env = os.environ.copy()
    env.update(PERSONAL_DESK_WORKSPACE=str(root), PERSONAL_DESK_ADDRESS="127.0.0.1:0")
    process = subprocess.Popen([str(binary)], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(100):
        marker = root / "logs" / "session.json"
        log = root / "logs" / "diagnostics.jsonl"
        if ((root / "discovery.json").exists() if ready else log.exists() and b'"query.fault"' in log.read_bytes()):
            time.sleep(1)
            return process
        if process.poll() is not None:
            raise RuntimeError("native process exited before diagnostic shell")
        time.sleep(0.1)
    process.terminate()
    raise RuntimeError("diagnostic shell timeout")

def stop(process):
    process.send_signal(signal.SIGTERM)
    process.wait(timeout=8)

def marker(root):
    return json.loads((root / "logs" / "session.json").read_text())

def records(root):
    records = []
    for name in ["diagnostics.jsonl"] + [f"diagnostics.{n}.jsonl" for n in range(1, 5)]:
        path = root / "logs" / name
        if not path.exists():
            continue
        data = path.read_bytes()
        assert len(data) <= 5 << 20
        assert not data or data.endswith(b"\n")
        assert path.stat().st_mode & 0o777 == 0o600
        for line in data.splitlines():
            assert len(line) + 1 <= 16 << 10
            event = json.loads(line)
            assert event["schemaVersion"] == 1 and len(event["sessionId"]) == 32
            assert set(event) <= {"schemaVersion", "at", "level", "code", "component", "operation", "requestId", "errorId", "sessionId", "durationMs", "appVersion", "buildVersion", "errorClass", "frames"}
            assert not any(value in line for value in [b"SYNTHETIC_SECRET", b"BUSINESS", b"/private", str(parent).encode()])
            records.append(event)
    return records

active = []
try:
    normal = fresh("normal")
    process = launch(normal); active.append(process)
    assert marker(normal)["clean"] is False
    credentials = json.loads((normal / "credentials.json").read_text())
    endpoint = json.loads((normal / "discovery.json").read_text())["endpoint"]
    request = urllib.request.Request(endpoint + "/api/v1/workspace", headers={"Authorization": "Bearer " + credentials["reader"]})
    with urllib.request.urlopen(request, timeout=3) as response:
        assert json.load(response)["workspaceRevision"] == 1
    # Deny the already-open log descriptor through its private-file policy.
    # The command response and every durable receipt remain exact on retry.
    log = normal / "logs" / "diagnostics.jsonl"
    log.chmod(0o400)
    body = json.dumps({"source": "manual", "scope": "diagnostic-smoke", "messageId": "denied-sink", "kind": "user_request", "text": "SYNTHETIC_SECRET BUSINESS /private"}).encode()
    def post():
        command = urllib.request.Request(endpoint + "/api/v1/inbox/messages", data=body, headers={"Authorization": "Bearer " + credentials["coordinator"], "Content-Type": "application/json", "Idempotency-Key": "diagnostic-smoke"}, method="POST")
        with urllib.request.urlopen(command, timeout=3) as response:
            assert response.status == 201
            return response.read()
    original = post()
    def facts():
        with sqlite3.connect("file:" + str(normal / "workspace.sqlite") + "?mode=ro", uri=True) as db:
            return [(name, db.execute("SELECT * FROM " + name + " ORDER BY 1,2").fetchall()) for name in ["workspace_meta", "workspace_counters", "inbox_messages", "inbox_payloads", "tasks", "task_creation", "task_events", "inbox_resolutions", "mutation_receipts", "task_materials"]]
    before_facts = facts()
    for _ in range(3):
        assert post() == original
    assert facts() == before_facts
    time.sleep(0.2)
    log.chmod(0o600)
    time.sleep(0.2)
    shared_before = {p.name: hashlib.sha256(p.read_bytes()).digest() for p in (normal / "logs").iterdir()}
    competitor_env = os.environ.copy()
    competitor_env.update(PERSONAL_DESK_WORKSPACE=str(normal), PERSONAL_DESK_ADDRESS="127.0.0.1:0")
    competitor = subprocess.Popen([str(binary)], env=competitor_env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL); active.append(competitor)
    time.sleep(2)
    assert competitor.poll() is None
    assert shared_before == {p.name: hashlib.sha256(p.read_bytes()).digest() for p in (normal / "logs").iterdir()}
    stop(competitor)
    # A killed session signals only a prior interruption on next launch.
    process.kill(); process.wait(timeout=5)
    assert marker(normal)["clean"] is False
    interrupted_session = marker(normal)["sessionId"]
    process = launch(normal); active.append(process)
    current_session = marker(normal)["sessionId"]
    assert current_session != interrupted_session
    assert any(e["sessionId"] == current_session and e["code"] == "framework.event" and e["component"] == "bootstrap" and e.get("errorClass") == "runtime" and not e.get("errorId") for e in records(normal))
    stop(process)
    assert marker(normal)["clean"] is True
    assert not (normal / "discovery.json").exists()
    assert any(e["sessionId"] == current_session and e["code"] == "app.stop" for e in records(normal))
    failed = fresh("failed")
    db = failed / "workspace.sqlite"
    db.write_bytes(b"SYNTHETIC_SECRET BUSINESS /private invalid database")
    db.chmod(0o600)
    before = hashlib.sha256(db.read_bytes()).digest()
    process = launch(failed, ready=False); active.append(process)
    faults = [e for e in records(failed) if e["code"] == "query.fault"]
    assert faults and len(faults[-1]["errorId"]) == len(faults[-1]["requestId"]) == 32
    assert not marker(failed)["clean"] and not (failed / "discovery.json").exists()
    assert hashlib.sha256(db.read_bytes()).digest() == before
    stop(process)
    assert marker(failed)["clean"] and hashlib.sha256(db.read_bytes()).digest() == before
    print("PASS native diagnostics: valid private records, competing owner isolation, interruption/restart, denied-sink committed replay, failed DB shell and clean drain")
    print("Isolated synthetic workspace parent:", parent)
finally:
    for process in active:
        if process.poll() is None:
            process.terminate()
            process.wait(timeout=8)
