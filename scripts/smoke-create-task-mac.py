#!/usr/bin/env python3
"""Neutral create/retry/conflict/restart smoke against the actual Mac bundle.

Only a fresh private synthetic workspace is used. Tokens are never printed;
source/evidence URLs and file coordinates remain inert.
"""
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import signal
import sqlite3
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

os.umask(0o077)
binary = Path("bin/Personal Desk.app/Contents/MacOS/personal-desk").resolve()
root = Path(tempfile.mkdtemp(prefix="personal-desk-create-task-smoke-")).resolve()
environment = dict(os.environ, PERSONAL_DESK_WORKSPACE=str(root), PERSONAL_DESK_ADDRESS="127.0.0.1:0")

def launch():
    process = subprocess.Popen([str(binary)], env=environment, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(100):
        if (root / "discovery.json").exists():
            time.sleep(1)
            return process
        if process.poll() is not None:
            raise AssertionError("native process exited before readiness")
        time.sleep(0.1)
    process.terminate()
    process.wait(timeout=8)
    raise AssertionError("native API readiness timeout")

def request(path, role="reader", body=None, key=None):
    endpoint = json.loads((root / "discovery.json").read_text())["endpoint"]
    credentials = json.loads((root / "credentials.json").read_text())
    headers = {"Authorization": "Bearer " + credentials[role]}
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    if key is not None:
        headers["Idempotency-Key"] = key
    call = urllib.request.Request(endpoint + path, data=data, headers=headers)
    try:
        response = urllib.request.urlopen(call, timeout=5)
    except urllib.error.HTTPError as failure:
        response = failure
    with response:
        raw = response.read()
        return response.status, json.loads(raw), response.headers["X-Request-ID"], raw

def stop(process):
    process.send_signal(signal.SIGTERM)
    process.wait(timeout=8)
    assert not (root / "discovery.json").exists(), "discovery survived quit"

def canonical():
    with sqlite3.connect(root / "workspace.sqlite") as database:
        return [database.execute("SELECT * FROM " + table + " ORDER BY 1,2").fetchall() for table in
                ["workspace_meta", "workspace_counters", "inbox_messages", "inbox_payloads", "mutation_receipts",
                 "tasks", "task_creation", "task_events", "inbox_resolutions"]]

process = launch()
try:
    message = {"source": "manual", "scope": "synthetic-demo", "messageId": "create-001", "kind": "user_request",
               "text": "Original saved obligation\n<script>inert</script> ![inert](https://example.invalid/image)",
               "sourceUrl": "https://example.invalid/source", "artifacts": [
                   {"kind": "file", "coordinate": "/synthetic/material.txt", "label": "Inert file coordinate"}]}
    status, pending, _, receive_raw = request("/api/v1/inbox/messages", "worker", message, "receive")
    assert status == 201 and pending["workspaceRevision"] == 2 and pending["message"]["status"] == "pending"
    path = "/api/v1/inbox/messages/" + pending["message"]["id"] + "/resolutions"
    command = {"expectedMessageRevision": 1, "action": "createTask", "reason": "A separate coordinator decision",
               "task": {"kind": "request", "title": "Synthetic durable task"}}
    before = canonical()
    for role in ["reader", "worker"]:
        status, failure, _, _ = request(path, role, command, "create")
        assert status == 403 and failure["error"]["code"] == "forbidden"
    assert canonical() == before
    with ThreadPoolExecutor(max_workers=8) as pool:
        results = list(pool.map(lambda _: request(path, "coordinator", command, "create"), range(8)))
    status, created, attempt, raw = results[0]
    assert all(item[0] == 201 and item[3] == raw for item in results)
    assert len({item[2] for item in results}) == 8
    assert created["workspaceRevision"] == 3 and created["message"]["revision"] == 2 and created["message"]["status"] == "applied"
    task = created["task"]
    assert task["kind"] == "request" and task["status"] == "new" and task["revision"] == 1
    assert task["summary"] is task["nextAction"] is task["nextActor"] is None
    assert task["provenance"]["submittedBy"] == pending["message"]["submittedBy"]
    assert task["timeline"][0]["kind"] == "task.created" and task["timeline"][0]["reason"] == command["reason"]
    assert task["timeline"][0]["appliedBy"]["role"] == "coordinator"
    assert task["createdAt"] == task["updatedAt"] == created["message"]["resolution"]["appliedAt"]
    assert task["createdAt"].endswith("Z") and task["materials"] == pending["message"]["artifacts"]
    assert task["materials"][0]["materialId"] is None
    normalized = dict(command, task=dict(command["task"], summary=None, nextAction=None, nextActor=None))
    status, _, retry_attempt, retry_raw = request(path, "coordinator", normalized, "create")
    assert status == 201 and retry_raw == raw and retry_attempt != attempt
    before = canonical()
    for key, body, code in [("create", dict(command, reason="Changed"), "idempotency_conflict"),
                            ("other", command, "terminal_resolution")]:
        status, failure, _, _ = request(path, "coordinator", body, key)
        assert status == 409 and failure["error"]["code"] == code
    assert canonical() == before
    status, _, _, replay_raw = request("/api/v1/inbox/messages", "worker", message, "receive")
    assert status == 201 and replay_raw == receive_raw
    status, duplicate, _, _ = request("/api/v1/inbox/messages", "worker", message, "redelivery")
    assert status == 200 and duplicate["message"] == created["message"] and duplicate["workspaceRevision"] == 3
    before = canonical()
    for _ in range(3):
        status, saved, _, _ = request("/api/v1/inbox/messages/" + pending["message"]["id"])
        assert status == 200 and saved["message"] == created["message"]
        status, snapshot, _, _ = request("/api/v1/workspace")
        assert status == 200 and snapshot["tasks"][0]["id"] == task["id"] and snapshot["tasks"][0]["title"] == task["title"]
        assert snapshot["counts"] == {"tasks": 1, "inbox": 1, "unprocessedInbox": 0, "needsAttention": 0}
        assert snapshot["inbox"][0]["status"] == "applied"
    status, details, _, _ = request("/api/v1/tasks/" + task["id"])
    assert status == 200 and details == {"workspaceRevision": 3, "task": task}
    assert canonical() == before, "passive reads changed canonical data"
    with sqlite3.connect(root / "workspace.sqlite") as database:
        assert database.execute("SELECT count(*) FROM task_events").fetchone()[0] == 1
        assert database.execute("SELECT count(*) FROM inbox_resolutions").fetchone()[0] == 1
        assert database.execute("SELECT task_sequence,event_sequence,resolution_sequence FROM workspace_counters").fetchone() == (1, 1, 1)
    credential_hash = hashlib.sha256((root / "credentials.json").read_bytes()).digest()
    stop(process)
    process = launch()
    status, _, _, restarted_raw = request(path, "coordinator", command, "create")
    assert status == 201 and restarted_raw == raw and canonical() == before
    assert hashlib.sha256((root / "credentials.json").read_bytes()).digest() == credential_hash
    stop(process)
    print("PASS native atomic create, concurrent exact retry, normalized replay, terminal/key conflicts, role refusal, saved views, restart")
    print("Isolated workspace retained:", root)
finally:
    if process.poll() is None:
        process.terminate()
        process.wait(timeout=8)
