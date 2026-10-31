#!/usr/bin/env python3
"""Actual bundle: full task facts, all read roles, passive state and restart.

Only isolated synthetic data is used. Coordinates and Markdown image text point
to a local canary; any automatic content fetch fails this smoke.
"""
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import signal
import sqlite3
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request

os.umask(0o077)
binary = Path("bin/Personal Desk.app/Contents/MacOS/personal-desk").resolve()
root = Path(tempfile.mkdtemp(prefix="personal-desk-task-details-smoke-")).resolve()
environment = dict(os.environ, PERSONAL_DESK_WORKSPACE=str(root), PERSONAL_DESK_ADDRESS="127.0.0.1:0")
fetches = []

class Canary(BaseHTTPRequestHandler):
    def do_GET(self):
        fetches.append(self.path)
        self.send_response(200)
        self.end_headers()
    def log_message(self, *args):
        pass

canary = ThreadingHTTPServer(("127.0.0.1", 0), Canary)
thread = threading.Thread(target=canary.serve_forever, daemon=True)
thread.start()
coordinate = "http://127.0.0.1:" + str(canary.server_port) + "/must-stay-inert"

def launch():
    process = subprocess.Popen([str(binary)], env=environment, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(100):
        if (root / "discovery.json").exists():
            time.sleep(1)
            return process
        assert process.poll() is None, "native process exited before readiness"
        time.sleep(0.1)
    process.terminate()
    process.wait(timeout=8)
    raise AssertionError("native readiness timeout")

def request(path, role="reader", body=None, key=None, extra=None):
    endpoint = json.loads((root / "discovery.json").read_text())["endpoint"]
    credentials = json.loads((root / "credentials.json").read_text())
    headers = {"Authorization": "Bearer " + credentials[role]}
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    if key is not None:
        headers["Idempotency-Key"] = key
    headers.update(extra or {})
    call = urllib.request.Request(endpoint + path, data=data, headers=headers)
    try:
        response = urllib.request.urlopen(call, timeout=5)
    except urllib.error.HTTPError as failure:
        response = failure
    with response:
        return response.status, json.load(response), response.headers["X-Request-ID"]

def canonical():
    with sqlite3.connect(root / "workspace.sqlite") as database:
        rows = [database.execute("SELECT * FROM " + table + " ORDER BY 1,2").fetchall() for table in
                ["workspace_meta", "workspace_counters", "tasks", "task_creation", "task_events",
                 "inbox_messages", "inbox_payloads", "inbox_resolutions", "mutation_receipts"]]
    return hashlib.sha256(json.dumps(rows, sort_keys=True).encode()).hexdigest()

def stop(process):
    process.send_signal(signal.SIGTERM)
    process.wait(timeout=8)
    assert not (root / "discovery.json").exists(), "discovery survived shutdown"

process = launch()
try:
    status, discovery, _ = request("/api/v1")
    assert status == 200 and discovery["capabilities"] == ["workspace.read", "tasks.read", "inbox.read", "inbox.receive", "inbox.resolve.createTask", "materials.read", "materials.upload"]
    message = {"source": "claimed-synthetic", "scope": "task-details", "messageId": "detail-one", "kind": "user_request",
               "sender": "Claimed sender", "text": "Original <script>inert</script>\n![image](" + coordinate + ")",
               "sourceUrl": coordinate, "artifacts": [{"kind": "url", "coordinate": coordinate, "label": "Inert source"}]}
    status, received, _ = request("/api/v1/inbox/messages", "worker", message, "receive")
    assert status == 201
    command = {"expectedMessageRevision": 1, "action": "createTask", "reason": "Explicit saved coordinator reason",
               "task": {"kind": "request", "title": "Saved detail task", "summary": "Saved summary", "nextAction": "Discuss the result", "nextActor": "Owner"}}
    status, created, _ = request("/api/v1/inbox/messages/" + received["message"]["id"] + "/resolutions", "coordinator", command, "create")
    assert status == 201
    task = created["task"]
    path = "/api/v1/tasks/" + task["id"]
    expected = {"workspaceRevision": created["workspaceRevision"], "task": task}
    assert task["provenance"]["submittedBy"] == received["message"]["submittedBy"]
    assert task["timeline"][0]["appliedBy"]["role"] == "coordinator" and task["timeline"][0]["reason"] == command["reason"]
    assert task["materials"] == received["message"]["artifacts"] and task["materials"][0]["materialId"] is None
    before = canonical()
    attempts = set()
    for role in ["reader", "worker", "coordinator"]:
        for _ in range(3):
            status, detail, attempt = request(path, role)
            assert status == 200 and detail == expected
            attempts.add(attempt)
    assert len(attempts) == 9
    for bad_path, extra, status_code, code in [
        ("/api/v1/tasks/T-missing", {}, 404, "not_found"),
        ("/api/v1/tasks/%20", {}, 400, "invalid_request"),
        (path + "/extra", {}, 404, "not_found"),
        (path + "?token=PRIVATE", {}, 400, "invalid_request"),
        (path, {"Origin": ""}, 403, "forbidden"),
        (path, {"Host": "localhost:8765"}, 403, "forbidden"),
        (path, {"Authorization": "Bearer PRIVATE"}, 401, "unauthorized"),
        (path, {"X-API-Version": "v2"}, 400, "unsupported_api_version"),
        (path, {"Idempotency-Key": "PRIVATE"}, 400, "invalid_request"),
    ]:
        status, failure, attempt = request(bad_path, extra=extra)
        assert status == status_code and failure["error"]["code"] == code
        assert len(attempt) == 32 and "PRIVATE" not in json.dumps(failure)
    assert canonical() == before and not fetches
    credentials_hash = hashlib.sha256((root / "credentials.json").read_bytes()).hexdigest()
    stop(process)
    process = launch()
    status, detail, _ = request(path)
    assert status == 200 and detail == expected and canonical() == before
    assert hashlib.sha256((root / "credentials.json").read_bytes()).hexdigest() == credentials_hash
    assert not fetches, "passive task views fetched source/image coordinates"
    stop(process)
    print("PASS native task details: all-role saved facts, provenance/history/materials, strict safe reads, passive hash, restart, zero canary fetches")
    print("Canonical state SHA-256:", before)
    print("Isolated workspace retained:", root)
finally:
    if process.poll() is None:
        process.terminate()
        process.wait(timeout=8)
    canary.shutdown()
    canary.server_close()
