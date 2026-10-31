#!/usr/bin/env python3
"""Neutral HTTP receive/retry/conflict/restart against the actual Mac bundle.

Creates and retains one isolated synthetic workspace, and never prints tokens.
No provider, material or source URL is opened. Run from the repository root after packaging.
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
import urllib.error
import urllib.request

os.umask(0o077)
binary = Path("bin/Personal Desk.app/Contents/MacOS/personal-desk").resolve()
root = Path(tempfile.mkdtemp(prefix="personal-desk-inbox-smoke-")).resolve()
environment = os.environ.copy()
environment.update(PERSONAL_DESK_WORKSPACE=str(root), PERSONAL_DESK_ADDRESS="127.0.0.1:0")

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
        return [database.execute("SELECT * FROM " + table + " ORDER BY 1").fetchall() for table in
                ["workspace_meta", "workspace_counters", "inbox_messages", "inbox_payloads", "mutation_receipts", "tasks"]]

process = launch()
try:
    status, empty, _, _ = request("/api/v1/workspace")
    assert status == 200 and empty["workspaceRevision"] == 1 and empty["tasks"] == empty["inbox"] == []
    message = {"source": "manual", "scope": "synthetic-demo", "messageId": "request-001", "kind": "user_request",
               "sender": "Synthetic requester", "occurredAt": "2026-10-28T10:00:00Z",
               "text": "Original saved request\nInspect the durable Inbox without creating a task.\n<script>inert text</script> ![inert](https://example.invalid/image)",
               "sourceUrl": "https://example.invalid/source", "taskRef": {"taskId": "T-unconfirmed", "revision": 3},
               "artifacts": [{"kind": "file", "coordinate": "/synthetic/material.txt", "label": "Inert file coordinate"}]}
    status, first, first_attempt, raw = request("/api/v1/inbox/messages", "worker", message, "receive-001")
    assert status == 201 and first["workspaceRevision"] == 2 and first["message"]["status"] == "pending"
    assert first["message"]["text"] == message["text"] and first["message"]["submittedBy"]["role"] == "worker"
    status, replay, attempt, replay_raw = request("/api/v1/inbox/messages", "worker", message, "receive-001")
    assert status == 201 and replay_raw == raw and attempt != first_attempt
    status, duplicate, _, _ = request("/api/v1/inbox/messages", "coordinator", message, "identity-duplicate")
    assert status == 200 and duplicate == first
    changed = dict(message, text="Conflicting content")
    for key, code in [("receive-001", "idempotency_conflict"), ("new-key", "message_identity_conflict")]:
        status, outcome, _, _ = request("/api/v1/inbox/messages", "worker", changed, key)
        assert status == 409 and outcome["error"]["code"] == code
    status, outcome, _, _ = request("/api/v1/inbox/messages", "reader", message, "forbidden")
    assert status == 403 and outcome["error"]["code"] == "forbidden"
    second_input = dict(message, messageId="request-002", kind="agent_report", sender="Synthetic agent",
                        text="Second saved original report. A proposal remains unconfirmed.")
    status, second, _, _ = request("/api/v1/inbox/messages", "coordinator", second_input, "receive-002")
    assert status == 201 and second["workspaceRevision"] == 3
    before = canonical()
    for _ in range(3):
        status, snapshot, _, _ = request("/api/v1/workspace")
        assert status == 200 and snapshot["counts"]["tasks"] == 0 and snapshot["counts"]["inbox"] == 2
        assert snapshot["inbox"][0]["id"] == second["message"]["id"] and "text" not in snapshot["inbox"][0]
        status, details, _, _ = request("/api/v1/inbox/messages/" + first["message"]["id"])
        assert status == 200 and details["message"] == first["message"] and details["workspaceRevision"] == 3
    assert canonical() == before, "passive reads changed canonical data"
    credentials_hash = hashlib.sha256((root / "credentials.json").read_bytes()).digest()
    stop(process)
    process = launch()
    status, restarted, _, _ = request("/api/v1/inbox/messages/" + first["message"]["id"])
    assert status == 200 and restarted["message"] == first["message"] and restarted["workspaceRevision"] == 3
    status, replay, _, replay_raw = request("/api/v1/inbox/messages", "worker", message, "receive-001")
    assert status == 201 and replay_raw == raw, "restart did not replay original committed result"
    assert canonical() == before and hashlib.sha256((root / "credentials.json").read_bytes()).digest() == credentials_hash
    stop(process)
    print("PASS native receive, exact retry, identity duplicate, both conflicts, role refusal, passive reads, restart; zero tasks")
    print("Isolated workspace retained:", root)
finally:
    if process.poll() is None:
        process.terminate()
        process.wait(timeout=8)
