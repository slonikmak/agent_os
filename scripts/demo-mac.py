#!/usr/bin/env python3
"""One retained E1 demonstration against the normal Mac app; Python stdlib only.

Importing this module performs no I/O. The runner always creates its own private
synthetic workspace. It never accepts a caller workspace or prints HTTP bodies,
credentials, or unsanitized exceptions. Original raw response bytes stay in
memory for replay comparisons; reports retain only status/length/digests.
Existing smoke scripts deliberately remain independent.
"""
from __future__ import annotations

import argparse
from contextlib import closing
from dataclasses import dataclass
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import platform
import re
import signal
import socket
import sqlite3
import stat
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request


BASELINE = "0125f9f3087b4db4506d80849684f8ad83e9963b"
ROOT = Path(__file__).resolve().parent.parent
BINARY = ROOT / "bin/Personal Desk.app/Contents/MacOS/personal-desk"
MAX_RESPONSE = 2 << 20


class DemoError(Exception):
    """Only fixed, content-free failure codes may cross the runner boundary."""


def require(condition, code):
    if not condition:
        raise DemoError(code)


def encoded(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"),
                      default=lambda v: {"bytesHex": v.hex()}).encode()


def digest(data):
    return hashlib.sha256(data).hexdigest()


def private_write(path, data):
    # All paths are owned descendants of the newly created evidence directory.
    with open(path, "wb") as stream:
        os.fchmod(stream.fileno(), 0o600)
        stream.write(data)


def file_hash(path, limit=32 << 20):
    require(stat.S_ISREG(path.lstat().st_mode), "fingerprint_nonregular_file")
    with path.open("rb") as stream:
        data = stream.read(limit + 1)
    require(len(data) <= limit, "fingerprint_file_budget")
    return digest(data)


def fingerprint(root, closed=False, after_table=None):
    """Hash one explicit SQL read transaction, receipts, credential bytes/blobs.

    mode=ro refuses missing databases. immutable is only for a closed app, after
    its checkpoint/shutdown; it avoids recreating WAL/SHM during offline checks.
    after_table is a helper-test observation seam, never an application hook.
    """
    database = root / "workspace.sqlite"
    require(database.is_file(), "fingerprint_missing_database")
    uri = database.as_uri() + "?mode=ro" + ("&immutable=1" if closed else "")
    tables = {}
    deadline = time.monotonic() + 5
    with closing(sqlite3.connect(uri, uri=True, timeout=3)) as db:
        db.execute("PRAGMA query_only=ON")
        db.set_progress_handler(lambda: int(time.monotonic() > deadline), 1000)
        db.execute("BEGIN")
        schema = db.execute("SELECT type,name,tbl_name,sql FROM sqlite_schema ORDER BY type,name").fetchall()
        for name, in db.execute("SELECT name FROM sqlite_schema WHERE type='table' ORDER BY name").fetchall():
            quoted = '"' + name.replace('"', '""') + '"'
            rows = db.execute("SELECT * FROM " + quoted).fetchall()
            tables[name] = {"rows": len(rows), "sha256": digest(encoded(sorted(rows, key=encoded)))}
            if after_table:
                after_table(name)
        db.rollback()
    blobs = {}
    directory = root / "materials"
    if directory.exists():
        paths = sorted(directory.iterdir())
        require(len(paths) <= 128, "fingerprint_blob_budget")
        for path in paths:
            blobs[path.name] = file_hash(path, 2 << 20)
    require("mutation_receipts" in tables, "fingerprint_missing_receipts")
    return {"sqlSha256": digest(encoded([schema, tables])), "tables": tables,
            "receiptsSha256": tables["mutation_receipts"]["sha256"],
            "credentialsSha256": file_hash(root / "credentials.json", 65536), "blobs": blobs}


@dataclass
class Response:
    status: int
    raw: bytes
    request_id: str | None

    def json(self):
        try:
            return json.loads(self.raw)
        except (ValueError, UnicodeDecodeError):
            raise DemoError("http_invalid_json") from None

    def expect(self, status, code=None):
        require(self.status == status, "http_unexpected_status")
        value = self.json()
        if code:
            require(value.get("error", {}).get("code") == code, "http_unexpected_error_code")
        require(bool(re.fullmatch(r"[0-9a-f]{32}", self.request_id or "")), "http_missing_request_id")
        return value


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def endpoint_address(endpoint):
    url = urllib.parse.urlsplit(endpoint)
    require(url.scheme == "http" and url.hostname == "127.0.0.1" and url.port
            and not url.username and not url.password and url.path in ("", "/")
            and not url.query and not url.fragment, "invalid_discovery_endpoint")
    return url.hostname, url.port


class Client:
    def __init__(self, root, report):
        self.endpoint = json.loads((root / "discovery.json").read_bytes())["endpoint"]
        endpoint_address(self.endpoint)
        self.credentials = json.loads((root / "credentials.json").read_bytes())
        self.report = report
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())

    def request(self, label, path, role="reader", body=None, key=None, extra=None):
        require(bool(re.fullmatch(r"[a-z0-9-]+", label)), "invalid_response_label")
        headers = {"Authorization": "Bearer " + self.credentials[role]}
        data = None if body is None else json.dumps(body, ensure_ascii=False).encode()
        if body is not None:
            headers["Content-Type"] = "application/json"
        if key is not None:
            headers["Idempotency-Key"] = key
        headers.update(extra or {})
        call = urllib.request.Request(self.endpoint + path, data=data, headers=headers)
        try:
            response = self.opener.open(call, timeout=5)
        except urllib.error.HTTPError as failure:
            response = failure
        with response:
            raw = response.read(MAX_RESPONSE + 1)
            self.report["responses"].append({"check": label, "status": response.status,
                                             "bytes": len(raw), "sha256": digest(raw)})
            require(len(raw) <= MAX_RESPONSE, "http_response_budget")
            # A server response must never reflect any credential, even on error.
            require(not any(token.encode() in raw for token in self.credentials.values()),
                    "http_credential_reflection")
            return Response(response.status, raw, response.headers.get("X-Request-ID"))


class Canary:
    def __init__(self):
        self.counts = {"GET": 0, "HEAD": 0}
        counts = self.counts

        class Handler(BaseHTTPRequestHandler):
            def observe(self):
                counts[self.command] += 1
                self.send_response(200)
                self.end_headers()

            do_GET = do_HEAD = observe

            def log_message(self, *args):
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.daemon_threads = True
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.coordinate = f"http://127.0.0.1:{self.server.server_port}/must-stay-inert"

    def close(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=3)
        require(not self.thread.is_alive(), "canary_shutdown_timeout")


class App:
    def __init__(self, binary, root, report):
        self.binary, self.root, self.report = binary, root, report
        self.process, self.endpoint, self.record = None, None, None

    def launch(self, timeout=15, settle=1):
        require(self.binary.is_file() and os.access(self.binary, os.X_OK), "missing_executable")
        require(not (self.root / "discovery.json").exists(), "stale_discovery")
        environment = dict(os.environ, PERSONAL_DESK_WORKSPACE=str(self.root), PERSONAL_DESK_ADDRESS="127.0.0.1:0")
        # The normal production executable ignores fixture switches; remove them
        # as well to make the process environment's intent unambiguous.
        for name in tuple(environment):
            if name.startswith("PERSONAL_DESK_EXPORT_FIXTURE"):
                del environment[name]
        self.process = subprocess.Popen([str(self.binary)], env=environment, stdout=subprocess.DEVNULL,
                                        stderr=subprocess.DEVNULL, start_new_session=True)
        self.record = {"pid": self.process.pid, "readiness": "pending", "exitCode": None}
        self.report["processes"].append(self.record)
        try:
            deadline = time.monotonic() + timeout
            while time.monotonic() < deadline:
                require(self.process.poll() is None, "child_exited_before_readiness")
                try:
                    endpoint = json.loads((self.root / "discovery.json").read_bytes())["endpoint"]
                    host, port = endpoint_address(endpoint)
                    with socket.create_connection((host, port), timeout=0.2):
                        pass
                    self.endpoint = endpoint
                    time.sleep(settle)  # Native shutdown hooks install after HTTP readiness.
                    require(self.process.poll() is None, "child_exited_before_readiness")
                    self.record["readiness"] = "ready"
                    return
                except (OSError, ValueError, KeyError):
                    time.sleep(0.1)
            raise DemoError("readiness_timeout")
        except BaseException:
            self.record["readiness"] = "failed"
            self.cleanup()
            raise

    def cleanup(self):
        if self.process is not None:
            if self.process.poll() is None:
                self.process.terminate()
                try:
                    self.process.wait(timeout=8)
                except subprocess.TimeoutExpired:
                    self.record["forcedKill"] = True
                    self.process.kill()
                    self.process.wait(timeout=3)
            self.record["exitCode"] = self.process.poll()

    def verify_exit(self):
        require(self.process.poll() == 0, "unclean_app_exit")
        self.record["exitCode"] = self.process.returncode
        require(not (self.root / "discovery.json").exists(), "discovery_survived_exit")
        host, port = endpoint_address(self.endpoint)
        try:
            with socket.create_connection((host, port), timeout=0.5):
                raise DemoError("listener_survived_exit")
        except OSError:
            pass
        marker = json.loads((self.root / "logs/session.json").read_bytes())
        require(marker.get("clean") is True, "unclean_session_marker")
        self.record.update(discoveryRemoved=True, listenerClosed=True, sessionClean=True)

    def stop(self):
        self.cleanup()
        self.verify_exit()


def source_identity(binary):
    def git(*args):
        return subprocess.check_output(["git", "-C", str(ROOT), *args], timeout=10)

    revision = git("rev-parse", "HEAD").decode().strip()
    dirty = git("status", "--porcelain=v1", "--untracked-files=all").decode().splitlines()
    names = git("ls-tree", "-r", "--name-only", BASELINE).decode().splitlines()

    def is_nonproduction(name):
        # Keep the audit scoped to application inputs. These paths describe or
        # operate the app, but are not embedded in the production executable.
        return (name.startswith(("docs/", "scripts/"))
                or name in {"README.md", ".gitignore", ".github/workflows/desktop.yml"})

    audited, changed = [], []
    for name in names:
        # Everything that can enter the application, including schemas, SQL,
        # bindings, assets, native config and pins; only harness/docs excluded.
        if is_nonproduction(name):
            continue
        path = ROOT / name
        original = git("show", BASELINE + ":" + name)
        if path.is_symlink():
            current = os.fsencode(os.readlink(path))
        elif path.is_file():
            current = path.read_bytes()
        else:
            audited.append({"path": name, "sha256": None})
            changed.append(name)
            continue
        audited.append({"path": name, "sha256": digest(current)})
        if original != current:
            changed.append(name)
    # Catch newly added production inputs as well as edits/deletions.
    current_paths = git("ls-files", "--cached", "--others", "--exclude-standard").decode().splitlines()
    for name in current_paths:
        if name not in names and not is_nonproduction(name):
            changed.append(name)
    require(not changed, "production_changed_since_baseline")
    require(binary.is_file(), "missing_executable")
    return {"revision": revision, "dirty": bool(dirty), "status": dirty, "baseline": BASELINE,
            "productionByteAudit": {"result": "identical", "paths": audited},
            "executable": {"path": str(binary), "sha256": file_hash(binary, 128 << 20),
                           "bytes": binary.stat().st_size},
            "runtime": {"system": platform.system(), "release": platform.release(),
                        "machine": platform.machine(), "python": platform.python_version()}}


def run_cycle(app, root, report, canary, checkpoint):
    app.launch()
    client = Client(root, report)
    discovery = client.request("discovery", "/api/v1").expect(200)
    require(discovery["apiRevision"] == 2 and discovery["capabilities"] == [
        "workspace.read", "tasks.read", "inbox.read", "inbox.receive", "inbox.resolve.createTask",
        "materials.read", "materials.upload"], "unexpected_capabilities")
    empty = client.request("empty", "/api/v1/workspace").expect(200)
    require(empty["workspaceRevision"] == 1 and empty["tasks"] == empty["inbox"] == empty["sources"] == []
            and all(v == 0 for v in empty["counts"].values()), "workspace_not_empty")
    checkpoint("fresh-empty")
    message = {"source": "manual", "scope": "e1-demo", "messageId": "request-001", "kind": "user_request",
               "sender": "Synthetic requester", "occurredAt": "2026-10-28T10:00:00Z",
               "text": "Original saved request — Привет 👋\n<script>inert</script>\n![image](" + canary.coordinate + ")",
               "sourceUrl": canary.coordinate,
               "artifacts": [{"kind": "file", "coordinate": "/synthetic/context.txt", "label": "Inert file coordinate"}]}
    receive = client.request("receive-original", "/api/v1/inbox/messages", "worker", message, "receive-001")
    pending = receive.expect(201)
    original = pending["message"]
    require(pending["workspaceRevision"] == 2 and original["status"] == "pending"
            and original["revision"] == 1 and original["text"] == message["text"]
            and original["sender"] == message["sender"] and original["occurredAt"] == message["occurredAt"]
            and original["sourceUrl"] == canary.coordinate and original["submittedBy"]["role"] == "worker"
            and original["receivedAt"].endswith("Z"), "pending_original_facts")
    snapshot = client.request("pending-zero-tasks", "/api/v1/workspace").expect(200)
    require(snapshot["tasks"] == [] and snapshot["counts"]["unprocessedInbox"] == 1, "receive_created_task")
    resolution_path = "/api/v1/inbox/messages/" + original["id"] + "/resolutions"
    command = {"expectedMessageRevision": 1, "action": "createTask", "reason": "Separate coordinator decision",
               "task": {"kind": "request", "title": "Review the saved E1 request", "summary": "Saved synthetic context"}}
    checkpoint("worker-receive-pending-zero-tasks")

    before = fingerprint(root)
    for extra, status, code, label in [
        ({"Authorization": "Bearer INVALID"}, 401, "unauthorized", "bad-auth"),
        ({"Host": "localhost:8765"}, 403, "forbidden", "bad-host"),
        ({"Origin": "http://127.0.0.1"}, 403, "forbidden", "bad-origin")]:
        client.request(label, "/api/v1/workspace", extra=extra).expect(status, code)
    client.request("reader-receive-refused", "/api/v1/inbox/messages", "reader", message, "refused").expect(403, "forbidden")
    for role in ("reader", "worker"):
        client.request(role + "-create-refused", resolution_path, role, command, "create-001").expect(403, "forbidden")
    for key, code in (("receive-001", "idempotency_conflict"), ("identity-changed", "message_identity_conflict")):
        client.request("receive-" + code.replace("_", "-"), "/api/v1/inbox/messages", "worker",
                       dict(message, text="Changed immutable content"), key).expect(409, code)
    client.request("stale-message", resolution_path, "coordinator", dict(command, expectedMessageRevision=2), "stale-message").expect(409, "revision_conflict")
    require(fingerprint(root) == before, "rejections_changed_state")
    checkpoint("auth-role-host-origin-identity-revision-refusals")

    creation = client.request("create-original", resolution_path, "coordinator", command, "create-001")
    created = creation.expect(201)
    task, applied = created["task"], created["message"]
    require(created["workspaceRevision"] == 3 and task["revision"] == 1 and task["kind"] == "request"
            and task["status"] == "new" and task["nextAction"] is task["nextActor"] is None,
            "creation_saved_facts")
    require(task["provenance"]["messageId"] == original["id"]
            and task["provenance"]["submittedBy"] == original["submittedBy"]
            and task["materials"] == original["artifacts"] and len(task["timeline"]) == 1
            and task["timeline"][0]["kind"] == "task.created"
            and task["timeline"][0]["reason"] == command["reason"]
            and task["timeline"][0]["appliedBy"]["role"] == "coordinator"
            and applied["status"] == "applied" and applied["revision"] == 2
            and applied["resolution"]["taskId"] == task["id"]
            and task["createdAt"] == task["updatedAt"] == applied["resolution"]["appliedAt"], "creation_links_history")
    for field in ("text", "sender", "source", "scope", "messageId", "sourceUrl", "occurredAt", "receivedAt", "submittedBy", "artifacts"):
        require(applied[field] == original[field], "original_message_changed")
    material_path = "/api/v1/tasks/" + task["id"] + "/materials"
    text = "# Saved Unicode — Привет 👋\n<script>inert</script>\n![image](" + canary.coordinate + ")\n" + "long-" * 120
    upload = {"expectedTaskRevision": 1, "mediaType": "text/markdown", "text": text, "label": "Saved Unicode material"}
    before = fingerprint(root)
    for role in ("reader", "worker"):
        client.request(role + "-upload-refused", material_path, role, upload, "upload-001").expect(403, "forbidden")
    client.request("create-key-conflict", resolution_path, "coordinator", dict(command, reason="Changed"), "create-001").expect(409, "idempotency_conflict")
    client.request("terminal-refused", resolution_path, "coordinator", command, "other-create").expect(409, "terminal_resolution")
    require(fingerprint(root) == before, "create_refusals_changed_state")
    material = client.request("upload-original", material_path, "coordinator", upload, "upload-001")
    uploaded = material.expect(201)
    require(uploaded["workspaceRevision"] == 4 and uploaded["taskRef"] == {"taskId": task["id"], "revision": 2}, "material_revision")
    saved_material_path = "/api/v1/materials/" + uploaded["materialId"]
    before = fingerprint(root)
    client.request("upload-key-conflict", material_path, "coordinator", dict(upload, text="Changed"), "upload-001").expect(409, "idempotency_conflict")
    client.request("upload-stale-revision", material_path, "coordinator", upload, "stale-upload").expect(409, "revision_conflict")
    for bad_id in ("credentials.json", "workspace.sqlite", "M-unknown"):
        client.request("material-private-refused", "/api/v1/materials/" + bad_id).expect(404, "not_found")
    require(fingerprint(root) == before, "upload_refusals_changed_state")
    # Keep a second pending message for the native Inbox's default filter.
    second = dict(message, messageId="report-002", kind="agent_report", sender="Synthetic agent",
                  text="Pending synthetic report. No task is created by receiving it.")
    pending_second = client.request("second-pending", "/api/v1/inbox/messages", "worker", second, "receive-002").expect(201)
    require(pending_second["workspaceRevision"] == 5 and pending_second["message"]["status"] == "pending", "second_pending")
    checkpoint("separate-create-material-pending-and-applied")

    def replay():
        for label, path, role, body, key, original_response in [
            ("receive-replay", "/api/v1/inbox/messages", "worker", message, "receive-001", receive),
            ("create-replay", resolution_path, "coordinator", command, "create-001", creation),
            ("upload-replay", material_path, "coordinator", upload, "upload-001", material)]:
            response = client.request(label, path, role, body, key)
            response.expect(original_response.status)
            require(response.raw == original_response.raw and response.request_id != original_response.request_id,
                    "original_receipt_not_replayed")

    def read_views():
        expected = None
        for role in ("reader", "worker", "coordinator"):
            board = client.request(role + "-board", "/api/v1/workspace", role).expect(200)
            detail = client.request(role + "-task", "/api/v1/tasks/" + task["id"], role).expect(200)
            inbox = client.request(role + "-applied", "/api/v1/inbox/messages/" + original["id"], role).expect(200)
            pending_view = client.request(role + "-pending", "/api/v1/inbox/messages/" + pending_second["message"]["id"], role).expect(200)
            content = client.request(role + "-material", saved_material_path, role).expect(200)
            require(board["workspaceRevision"] == detail["workspaceRevision"] == inbox["workspaceRevision"]
                    == pending_view["workspaceRevision"] == content["workspaceRevision"] == 5, "live_workspace_revision")
            live = detail["task"]
            require(board["counts"] == {"tasks": 1, "inbox": 2, "unprocessedInbox": 1, "needsAttention": 0}
                    and len(board["tasks"]) == 1 and len(board["inbox"]) == 2, "saved_counts")
            require(live["revision"] == 2 and board["tasks"][0] == {k: live[k] for k in board["tasks"][0]}
                    and live["timeline"] == task["timeline"] and live["provenance"] == task["provenance"]
                    and len(live["materials"]) == 2 and live["materials"][0] == task["materials"][0]
                    and live["materials"][1]["materialId"] == uploaded["materialId"], "live_task_associations")
            require(inbox["message"] == applied and pending_view["message"] == pending_second["message"], "saved_inbox")
            require(content["material"]["text"] == text and content["material"]["mediaType"] == "text/markdown"
                    and content["material"]["availability"] == "available", "exact_unicode_material")
            values = [board, detail, inbox, pending_view, content]
            require(expected is None or values == expected, "read_roles_disagree")
            expected = values
        return expected

    before = fingerprint(root)
    replay()
    views = read_views()
    require(fingerprint(root) == before, "first_exact_replay_changed_state")
    duplicate = client.request("identity-redelivery-current-view", "/api/v1/inbox/messages", "coordinator", message, "new-delivery").expect(200)
    require(duplicate["message"] == applied and duplicate["workspaceRevision"] == 5, "identity_current_view")
    # Identity redelivery stores a new receipt but no business revision/effect.
    stable = fingerprint(root)
    require(all(value == stable["tables"][name] for name, value in before["tables"].items()
                if name not in ("mutation_receipts", "sqlite_sequence"))
            and before["credentialsSha256"] == stable["credentialsSha256"] and before["blobs"] == stable["blobs"]
            and stable["tables"]["task_events"]["rows"] == 1
            and stable["tables"]["inbox_resolutions"]["rows"] == 1, "duplicate_canonical_effect")
    replay()
    require(read_views() == views and fingerprint(root) == stable, "passive_or_replay_drift")
    require(canary.counts == {"GET": 0, "HEAD": 0}, "automatic_source_fetch")
    report["fingerprint"] = stable
    report["savedFacts"] = {"workspaceRevision": 5, "taskRevision": 2, "taskId": task["id"],
                            "appliedInboxId": original["id"], "pendingInboxId": pending_second["message"]["id"],
                            "materialId": uploaded["materialId"], "originalReceiptRevisions": {"receive": 2, "create": 3, "upload": 4}}
    app.stop()
    require(fingerprint(root, closed=True) == stable, "first_exit_drift")
    checkpoint("first-lifetime-clean-passive-replay")
    app.launch()
    client = Client(root, report)
    require(fingerprint(root) == stable, "restart_drift")
    replay()
    require(read_views() == views and fingerprint(root) == stable, "restart_replay_or_view_drift")
    checkpoint("second-lifetime-restart-original-byte-replay")
    return stable


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inspect", action="store_true", help="keep the restarted app/canary alive until native Quit")
    args = parser.parse_args(argv)
    os.umask(0o077)
    parent = Path(tempfile.mkdtemp(prefix="personal-desk-e1-demo-")).resolve()
    root = parent / "workspace"
    root.mkdir(mode=0o700)
    (parent / "exports").mkdir(mode=0o700)
    report = {"format": 1, "status": "running", "mode": "inspect" if args.inspect else "automated",
              "workspace": str(root), "exportsDestination": str(parent / "exports"),
              "responses": [], "processes": [], "checks": [],
              "humanObservations": {"status": "not-recorded", "note": "Native navigation/layout/Help/Save require separate reviewer evidence."},
              "deferred": ["story10 installed/manual replacement", "Windows native", "distribution signing", "full cross-platform epic acceptance"]}
    app, canary = App(BINARY, root, report), None

    def interrupted(signum, frame):
        raise KeyboardInterrupt

    previous_term = signal.signal(signal.SIGTERM, interrupted)

    def save():
        private_write(parent / "report.json", json.dumps(report, indent=2, sort_keys=True).encode() + b"\n")

    def checkpoint(name):
        report["checks"].append({"name": name, "status": "passed"})
        save()

    save()
    print("Retained evidence:", parent, flush=True)
    exit_code = 1
    try:
        report["identity"] = source_identity(BINARY)
        canary = Canary()
        stable = run_cycle(app, root, report, canary, checkpoint)
        if args.inspect:
            report["status"] = "awaiting-native-quit"
            save()
            print("Inspect the restarted saved workspace; use native Quit when finished.", flush=True)
            print("Native export destination:", parent / "exports", flush=True)
            while app.process.poll() is None:
                time.sleep(0.2)
            app.verify_exit()
        else:
            app.stop()
        require(fingerprint(root, closed=True) == stable, "final_passive_state_drift")
        require(canary.counts == {"GET": 0, "HEAD": 0}, "automatic_source_fetch")
        checkpoint("final-clean-quit-passive-fingerprints-zero-canary")
        report["status"] = "passed"
        exit_code = 0
    except KeyboardInterrupt:
        report.update(status="interrupted", failure="runner_interrupted")
        exit_code = 130
    except Exception as failure:
        # Do not serialize arbitrary exception messages (HTTP bodies, tokens,
        # paths or command output). Raw bytes are held only in memory.
        code = str(failure) if isinstance(failure, DemoError) else type(failure).__name__
        report.update(status="failed", failure=code)
    finally:
        if canary:
            report["canary"] = dict(canary.counts)
        for cleanup in ([app.cleanup, canary.close] if canary else [app.cleanup]):
            try:
                cleanup()
            except Exception:
                report.update(status="failed", cleanupFailure="owned_resource_cleanup_failed")
                exit_code = 1
        save()
        signal.signal(signal.SIGTERM, previous_term)
    print(report["status"].upper() + ": " + str(parent / "report.json"), flush=True)
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
