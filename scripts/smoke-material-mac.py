#!/usr/bin/env python3
"""Actual bundle: explicit text upload, confined reads and passive restart.

Only isolated synthetic data is used. A canary proves that saved source/image
text is never fetched automatically; deliberate browser navigation is a parent
native CUA acceptance check.
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
root = Path(tempfile.mkdtemp(prefix="personal-desk-material-smoke-")).resolve()
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
                 "inbox_messages", "inbox_payloads", "inbox_resolutions", "mutation_receipts", "task_materials"]]
    return hashlib.sha256(json.dumps(rows, sort_keys=True).encode()).hexdigest()

def stop(process):
    process.send_signal(signal.SIGTERM)
    process.wait(timeout=8)
    assert not (root / "discovery.json").exists(), "discovery survived shutdown"

process = launch()
try:
    status, discovery, _ = request("/api/v1")
    assert status == 200 and discovery["apiRevision"] == 2
    assert "materials.read" in discovery["capabilities"] and "materials.upload" in discovery["capabilities"]
    message = {"source": "manual", "scope": "materials", "messageId": "material-one", "kind": "user_request", "text": "Original context", "sourceUrl": coordinate,
               "artifacts": [{"kind": "file", "coordinate": "/private/credentials.json", "label": "Inert path"}]}
    status, received, _ = request("/api/v1/inbox/messages", "worker", message, "receive")
    assert status == 201
    command = {"expectedMessageRevision": 1, "action": "createTask", "reason": "Explicit coordinator creation", "task": {"kind": "request", "title": "Saved material task", "summary": "Saved context stays usable"}}
    status, created, _ = request("/api/v1/inbox/messages/" + received["message"]["id"] + "/resolutions", "coordinator", command, "create")
    assert status == 201
    path = "/api/v1/tasks/" + created["task"]["id"] + "/materials"
    text = "# Saved Unicode 👋\n<script>inert</script>\n![image](" + coordinate + ")\n" + coordinate + "\n" + "long-" * 300
    upload = {"expectedTaskRevision": 1, "mediaType": "text/markdown", "text": text, "label": "Saved text"}
    for role in ["reader", "worker"]:
        status, error, _ = request(path, role, upload, "role-refusal")
        assert status == 403 and error["error"]["code"] == "forbidden"
    status, receipt, _ = request(path, "coordinator", upload, "upload")
    assert status == 201 and receipt["taskRef"]["revision"] == 2 and receipt["workspaceRevision"] == 4
    material_path = "/api/v1/materials/" + receipt["materialId"]
    before = canonical()
    for role in ["reader", "worker", "coordinator"]:
        status, result, _ = request(material_path, role)
        assert status == 200 and result["material"]["text"] == text and result["material"]["mediaType"] == "text/markdown"
    for _ in range(3):
        status, replay, _ = request(path, "coordinator", upload, "upload")
        assert status == 201 and replay == receipt
    status, error, _ = request(path, "coordinator", dict(upload, text="changed"), "upload")
    assert status == 409 and error["error"]["code"] == "idempotency_conflict"
    status, error, _ = request(path, "coordinator", upload, "stale")
    assert status == 409 and error["error"]["code"] == "revision_conflict"
    for material_id in ["workspace.sqlite", "credentials.json", "M-unknown"]:
        status, error, _ = request("/api/v1/materials/" + material_id)
        assert status == 404 and error["error"]["code"] == "not_found"
    assert canonical() == before and not fetches
    credentials_hash = hashlib.sha256((root / "credentials.json").read_bytes()).hexdigest()
    stop(process)
    process = launch()
    status, result, _ = request(material_path)
    assert status == 200 and result["material"]["text"] == text
    status, replay, _ = request(path, "coordinator", upload, "upload")
    assert status == 201 and replay == receipt and canonical() == before
    assert hashlib.sha256((root / "credentials.json").read_bytes()).hexdigest() == credentials_hash
    # Known missing bytes affect only the read result; associations remain saved.
    digest = hashlib.sha256(text.encode()).hexdigest()
    (root / "materials" / digest).unlink()
    status, missing, _ = request(material_path)
    assert status == 200 and missing["material"] == {"id": receipt["materialId"], "availability": "unavailable", "mediaType": None, "text": None, "unavailableReason": "missing"}
    status, task, _ = request("/api/v1/tasks/" + created["task"]["id"])
    assert status == 200 and len(task["task"]["materials"]) == 2 and task["task"]["summary"] == "Saved context stays usable"
    assert canonical() == before and not fetches
    stop(process)
    print("PASS native materials: coordinator upload, all-role exact text, immutable replay, conflicts, registry-only authority, passive hash, restart, missing state, zero automatic fetches")
    print("Canonical state SHA-256:", before)
    print("Isolated workspace retained:", root)
finally:
    if process.poll() is None:
        process.terminate()
        process.wait(timeout=8)
    canary.shutdown()
    canary.server_close()
