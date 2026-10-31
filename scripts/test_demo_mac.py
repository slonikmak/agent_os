#!/usr/bin/env python3
"""Failure-mode regressions for demo-mac.py; no native app/build needed."""
import contextlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import importlib.util
import io
import json
import os
from pathlib import Path
import signal
import sqlite3
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest import mock


SCRIPT = Path(__file__).with_name("demo-mac.py")


def load(name):
    spec = importlib.util.spec_from_file_location(name, SCRIPT)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


demo = load("demo_mac_tests_subject")


class Fixture(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="personal-desk-demo-helper-")
        self.root = Path(self.temp.name)

    def tearDown(self):
        self.temp.cleanup()

    def database(self, wal=False):
        db = sqlite3.connect(self.root / "workspace.sqlite")
        if wal:
            db.execute("PRAGMA journal_mode=WAL")
        db.executescript("CREATE TABLE alpha(value); INSERT INTO alpha VALUES(1);"
                         "CREATE TABLE omega(value); INSERT INTO omega VALUES(1);"
                         "CREATE TABLE mutation_receipts(body BLOB); INSERT INTO mutation_receipts VALUES(x'000102');")
        db.commit()
        (self.root / "credentials.json").write_bytes(b'PRIVATE-CREDENTIAL-BYTES')
        (self.root / "materials").mkdir()
        (self.root / "materials/blob").write_bytes("Привет 👋".encode())
        return db

    def executable(self, text):
        path = self.root / "owned-child"
        path.write_text("#!" + sys.executable + "\n" + text)
        path.chmod(0o700)
        return path


class Helpers(Fixture):
    def test_import_has_no_launch_workspace_network_thread_or_umask_effects(self):
        with mock.patch.object(subprocess, "Popen", side_effect=AssertionError("process")), \
             mock.patch.object(tempfile, "mkdtemp", side_effect=AssertionError("workspace")), \
             mock.patch.object(threading.Thread, "start", side_effect=AssertionError("thread")), \
             mock.patch.object(os, "umask", side_effect=AssertionError("umask")), \
             mock.patch.object(demo.socket, "create_connection", side_effect=AssertionError("network")), \
             contextlib.redirect_stdout(io.StringIO()) as output:
            module = load("demo_mac_import_guard")
        self.assertTrue(callable(module.main))
        self.assertEqual(output.getvalue(), "")
        self.assertEqual(list(self.root.iterdir()), [])

    def test_missing_database_never_creates_files(self):
        with self.assertRaisesRegex(demo.DemoError, "fingerprint_missing_database"):
            demo.fingerprint(self.root)
        self.assertEqual(list(self.root.iterdir()), [])

    def test_database_disappearing_after_existence_check_is_not_recreated(self):
        with mock.patch.object(Path, "is_file", return_value=True):
            with self.assertRaises(sqlite3.OperationalError):
                demo.fingerprint(self.root)
        self.assertEqual(list(self.root.iterdir()), [])

    def test_corrupt_database_fails_without_replacing_it(self):
        path = self.root / "workspace.sqlite"
        path.write_bytes(b"invalid retained database")
        with self.assertRaises(sqlite3.DatabaseError):
            demo.fingerprint(self.root)
        self.assertEqual(path.read_bytes(), b"invalid retained database")

    def test_closed_readonly_fingerprint_does_not_create_journals(self):
        self.database().close()
        before = {p.name: p.read_bytes() for p in self.root.iterdir() if p.is_file()}
        first = demo.fingerprint(self.root, closed=True)
        self.assertEqual(demo.fingerprint(self.root, closed=True), first)
        self.assertEqual({p.name: p.read_bytes() for p in self.root.iterdir() if p.is_file()}, before)
        self.assertFalse((self.root / "workspace.sqlite-wal").exists())
        self.assertFalse((self.root / "workspace.sqlite-shm").exists())

    def test_explicit_read_transaction_is_consistent_across_concurrent_commit(self):
        writer = self.database(wal=True)
        before = demo.fingerprint(self.root)

        def commit_after_alpha(name):
            if name == "alpha":
                writer.execute("UPDATE alpha SET value=2")
                writer.execute("UPDATE omega SET value=2")
                writer.commit()

        try:
            during = demo.fingerprint(self.root, after_table=commit_after_alpha)
            self.assertEqual(during, before)
            self.assertNotEqual(demo.fingerprint(self.root), before)
        finally:
            writer.close()

    def test_fingerprint_closes_wal_connection_without_gc_on_success_and_failure(self):
        connect = sqlite3.connect
        for fail in (False, True):
            with self.subTest(failure=fail):
                root = self.root / ("failure" if fail else "success")
                root.mkdir()
                writer = connect(root / "workspace.sqlite")
                writer.execute("PRAGMA journal_mode=WAL").fetchall()
                writer.executescript("CREATE TABLE mutation_receipts(body);"
                                     "INSERT INTO mutation_receipts VALUES('committed');")
                writer.commit()
                (root / "credentials.json").write_bytes(b"private-test-credentials")
                readers = []

                def retain_reader(*args, **kwargs):
                    reader = connect(*args, **kwargs)
                    readers.append(reader)  # Strong reference prevents GC cleanup.
                    return reader

                def fail_during_read(name):
                    raise RuntimeError("synthetic fingerprint read failure")

                try:
                    with mock.patch.object(demo.sqlite3, "connect", side_effect=retain_reader):
                        if fail:
                            with self.assertRaisesRegex(RuntimeError, "synthetic fingerprint read failure"):
                                demo.fingerprint(root, after_table=fail_during_read)
                        else:
                            demo.fingerprint(root)
                    self.assertEqual(len(readers), 1)
                    self.assertTrue((root / "workspace.sqlite-wal").is_file())
                    self.assertTrue((root / "workspace.sqlite-shm").is_file())
                    writer.close()
                    # An unclosed reader keeps a shared WAL lock even after its
                    # transaction ends. A new lifetime must acquire exclusive
                    # ownership without collecting the retained reader first.
                    with contextlib.closing(connect(root / "workspace.sqlite", timeout=0.1)) as restarted:
                        restarted.execute("PRAGMA locking_mode=EXCLUSIVE").fetchall()
                        restarted.execute("BEGIN EXCLUSIVE")
                        self.assertEqual(restarted.execute("SELECT body FROM mutation_receipts").fetchone(),
                                         ("committed",))
                        restarted.rollback()
                        self.assertEqual(restarted.execute("PRAGMA wal_checkpoint(TRUNCATE)").fetchall(),
                                         [(0, 0, 0)])
                    with self.assertRaises(sqlite3.ProgrammingError):
                        readers[0].execute("SELECT 1")
                    with contextlib.closing(connect((root / "workspace.sqlite").as_uri()
                                                   + "?mode=ro&immutable=1", uri=True)) as reopened:
                        self.assertEqual(reopened.execute("SELECT body FROM mutation_receipts").fetchone(),
                                         ("committed",))
                finally:
                    writer.close()
                    for reader in readers:
                        reader.close()

    def test_receipt_credential_and_blob_drift_are_independently_detected(self):
        db = self.database()
        before = demo.fingerprint(self.root)
        db.execute("UPDATE mutation_receipts SET body=x'ff'")
        db.commit()
        receipt = demo.fingerprint(self.root)
        self.assertNotEqual(receipt["receiptsSha256"], before["receiptsSha256"])
        self.assertNotEqual(receipt["sqlSha256"], before["sqlSha256"])
        (self.root / "materials/blob").write_bytes(b"drift")
        blob = demo.fingerprint(self.root)
        self.assertNotEqual(blob["blobs"], receipt["blobs"])
        self.assertEqual(blob["sqlSha256"], receipt["sqlSha256"])
        (self.root / "credentials.json").write_bytes(b"changed")
        self.assertNotEqual(demo.fingerprint(self.root)["credentialsSha256"], blob["credentialsSha256"])
        (self.root / "materials/blob").unlink()
        self.assertEqual(demo.fingerprint(self.root)["blobs"], {})
        db.close()

    def test_blob_symlink_is_refused(self):
        self.database().close()
        (self.root / "materials/alias").symlink_to(self.root / "credentials.json")
        with self.assertRaisesRegex(demo.DemoError, "fingerprint_nonregular_file"):
            demo.fingerprint(self.root)

    def test_missing_executable_has_no_child(self):
        report = {"processes": []}
        app = demo.App(self.root / "missing", self.root, report)
        with self.assertRaisesRegex(demo.DemoError, "missing_executable"):
            app.launch(timeout=0.1, settle=0)
        self.assertEqual(report["processes"], [])

    def test_readiness_timeout_reaps_its_owned_child(self):
        binary = self.executable("import time\ntime.sleep(60)\n")
        report = {"processes": []}
        app = demo.App(binary, self.root, report)
        with self.assertRaisesRegex(demo.DemoError, "readiness_timeout"):
            app.launch(timeout=0.15, settle=0)
        self.assertIsNotNone(app.process.poll())
        self.assertEqual(report["processes"][0]["readiness"], "failed")
        self.assertIsNotNone(report["processes"][0]["exitCode"])

    def test_early_exit_is_reaped_and_never_ready(self):
        binary = self.executable("raise SystemExit(7)\n")
        report = {"processes": []}
        app = demo.App(binary, self.root, report)
        with self.assertRaisesRegex(demo.DemoError, "child_exited_before_readiness"):
            app.launch(timeout=1, settle=0)
        self.assertEqual(app.process.poll(), 7)
        self.assertEqual(report["processes"][0]["readiness"], "failed")

    def test_canary_observes_get_and_head_and_closes(self):
        canary = demo.Canary()
        for method in ("GET", "HEAD"):
            req = demo.urllib.request.Request(canary.coordinate, method=method)
            with demo.urllib.request.urlopen(req, timeout=2):
                pass
        self.assertEqual(canary.counts, {"GET": 1, "HEAD": 1})
        canary.close()
        self.assertFalse(canary.thread.is_alive())

    def test_failed_or_interrupted_runner_retains_honest_report_and_cleans(self):
        for failure, status, code in ((ValueError("CREDENTIAL secret body"), "failed", 1),
                                      (KeyboardInterrupt(), "interrupted", 130)):
            parent = self.root / status
            parent.mkdir()
            fake_app, fake_canary = mock.Mock(), mock.Mock(counts={"GET": 0, "HEAD": 0})
            with mock.patch.object(demo.tempfile, "mkdtemp", return_value=str(parent)), \
                 mock.patch.object(demo, "source_identity", return_value={"test": True}), \
                 mock.patch.object(demo, "App", return_value=fake_app), \
                 mock.patch.object(demo, "Canary", return_value=fake_canary), \
                 mock.patch.object(demo, "run_cycle", side_effect=failure), \
                 contextlib.redirect_stdout(io.StringIO()) as output:
                self.assertEqual(demo.main([]), code)
            report = json.loads((parent / "report.json").read_bytes())
            self.assertEqual(report["status"], status)
            self.assertEqual(report["checks"], [])
            self.assertNotIn("CREDENTIAL", output.getvalue() + json.dumps(report))
            self.assertEqual(report["humanObservations"]["status"], "not-recorded")
            fake_app.cleanup.assert_called_once()
            fake_canary.close.assert_called_once()
            self.assertTrue((parent / "workspace").is_dir())
            self.assertEqual((parent / "report.json").stat().st_mode & 0o777, 0o600)

    def test_sigterm_becomes_interrupted_report_with_cleanup(self):
        parent = self.root / "sigterm"
        parent.mkdir()
        app, canary = mock.Mock(), mock.Mock(counts={"GET": 0, "HEAD": 0})

        def terminate(*args):
            os.kill(os.getpid(), signal.SIGTERM)

        with mock.patch.object(demo.tempfile, "mkdtemp", return_value=str(parent)), \
             mock.patch.object(demo, "source_identity", return_value={}), \
             mock.patch.object(demo, "App", return_value=app), \
             mock.patch.object(demo, "Canary", return_value=canary), \
             mock.patch.object(demo, "run_cycle", side_effect=terminate), \
             contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(demo.main([]), 130)
        self.assertEqual(json.loads((parent / "report.json").read_bytes())["status"], "interrupted")
        app.cleanup.assert_called_once()
        canary.close.assert_called_once()

    def test_app_cleanup_failure_still_closes_canary_and_never_passes(self):
        parent = self.root / "cleanup-failure"
        parent.mkdir()
        app, canary = mock.Mock(), mock.Mock(counts={"GET": 0, "HEAD": 0})
        app.cleanup.side_effect = subprocess.TimeoutExpired("owned-child", 3)
        with mock.patch.object(demo.tempfile, "mkdtemp", return_value=str(parent)), \
             mock.patch.object(demo, "source_identity", return_value={}), \
             mock.patch.object(demo, "App", return_value=app), \
             mock.patch.object(demo, "Canary", return_value=canary), \
             mock.patch.object(demo, "run_cycle", side_effect=ValueError("private body")), \
             contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(demo.main([]), 1)
        report = json.loads((parent / "report.json").read_bytes())
        self.assertEqual(report["status"], "failed")
        self.assertEqual(report["cleanupFailure"], "owned_resource_cleanup_failed")
        canary.close.assert_called_once()


class SourceIdentity(Fixture):
    def setUp(self):
        super().setUp()
        self.repo = self.root / "repo"
        self.repo.mkdir()
        files = {
            "main.go": b"package main\n",
            "internal/app.go": b"package internal\n",
            "docs/guide.md": b"initial docs\n",
            "scripts/helper.py": b"initial helper\n",
            "README.md": b"initial readme\n",
            ".gitignore": b"initial ignores\n",
            ".github/workflows/desktop.yml": b"initial workflow\n",
        }
        for name, content in files.items():
            path = self.repo / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
        subprocess.run(["git", "-C", str(self.repo), "init", "-b", "main"],
                       check=True, stdout=subprocess.DEVNULL)
        subprocess.run(["git", "-C", str(self.repo), "config", "user.name", "Demo tests"], check=True)
        subprocess.run(["git", "-C", str(self.repo), "config", "user.email", "demo-tests@example.invalid"], check=True)
        subprocess.run(["git", "-C", str(self.repo), "add", "."], check=True)
        subprocess.run(["git", "-C", str(self.repo), "commit", "-m", "fixture baseline"],
                       check=True, stdout=subprocess.DEVNULL)
        self.baseline = subprocess.check_output(
            ["git", "-C", str(self.repo), "rev-parse", "HEAD"], text=True).strip()
        self.binary = self.root / "personal-desk"
        self.binary.write_bytes(b"test executable")

    def source_identity(self):
        with mock.patch.object(demo, "ROOT", self.repo), \
             mock.patch.object(demo, "BASELINE", self.baseline):
            return demo.source_identity(self.binary)

    def test_identical_sources_are_audited_with_root_relative_paths(self):
        identity = self.source_identity()
        self.assertEqual(identity["productionByteAudit"]["result"], "identical")
        paths = {item["path"] for item in identity["productionByteAudit"]["paths"]}
        self.assertEqual(paths, {"main.go", "internal/app.go"})

    def test_changed_production_file_is_rejected(self):
        (self.repo / "internal/app.go").write_bytes(b"package internal // changed\n")
        with self.assertRaisesRegex(demo.DemoError, "production_changed_since_baseline"):
            self.source_identity()

    def test_added_production_file_is_rejected(self):
        (self.repo / "internal/new.go").write_bytes(b"package internal\n")
        with self.assertRaisesRegex(demo.DemoError, "production_changed_since_baseline"):
            self.source_identity()

    def test_deleted_production_file_is_rejected(self):
        (self.repo / "internal/app.go").unlink()
        with self.assertRaisesRegex(demo.DemoError, "production_changed_since_baseline"):
            self.source_identity()

    def test_harness_docs_readme_gitignore_and_workflow_are_excluded(self):
        for name in ("docs/guide.md", "scripts/helper.py", "README.md", ".gitignore",
                     ".github/workflows/desktop.yml"):
            (self.repo / name).write_bytes(b"changed outside the production audit\n")
        identity = self.source_identity()
        self.assertEqual(identity["productionByteAudit"]["result"], "identical")


class HTTP(Fixture):
    def setUp(self):
        super().setUp()
        owner = self
        self.raw, self.status = b'{ "error": {"code": "revision_conflict"} }\n', 409

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(owner.status)
                self.send_header("X-Request-ID", "a" * 32)
                self.end_headers()
                self.wfile.write(owner.raw)

            def log_message(self, *args):
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        (self.root / "discovery.json").write_text(json.dumps({"endpoint": f"http://127.0.0.1:{self.server.server_port}"}))
        (self.root / "credentials.json").write_text(json.dumps({"reader": "PRIVATE-TOKEN"}))
        self.report = {"responses": []}
        self.client = demo.Client(self.root, self.report)

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=2)
        super().tearDown()

    def test_raw_http_error_preserves_status_bytes_and_safe_index(self):
        response = self.client.request("conflict", "/conflict")
        self.assertEqual(response.status, 409)
        self.assertEqual(response.raw, self.raw)
        response.expect(409, "revision_conflict")
        entry = self.report["responses"][0]
        self.assertEqual(entry["sha256"], demo.digest(self.raw))
        self.assertNotIn("PRIVATE-TOKEN", json.dumps(self.report))
        self.assertNotIn("body", json.dumps(self.report))
        self.assertEqual(sorted(p.name for p in self.root.iterdir()), ["credentials.json", "discovery.json"])
        with self.assertRaisesRegex(demo.DemoError, "http_unexpected_status"):
            response.expect(201)

    def test_malformed_json_raw_is_preserved_in_memory_without_body_logging(self):
        self.raw, self.status = b"not JSON or a usable success", 200
        response = self.client.request("invalid-json", "/bad")
        with self.assertRaisesRegex(demo.DemoError, "http_invalid_json"):
            response.expect(200)
        self.assertEqual(response.raw, self.raw)
        self.assertNotIn("not JSON", json.dumps(self.report))

    def test_oversized_response_is_bounded_and_never_successful(self):
        self.raw = b"x" * 100
        with mock.patch.object(demo, "MAX_RESPONSE", 8):
            with self.assertRaisesRegex(demo.DemoError, "http_response_budget"):
                self.client.request("oversized", "/large")
        self.assertEqual(self.report["responses"][0]["bytes"], 9)

    def test_credential_reflection_and_external_discovery_are_refused(self):
        self.raw = b'{"echo": "PRIVATE-TOKEN"}'
        with self.assertRaisesRegex(demo.DemoError, "http_credential_reflection"):
            self.client.request("reflection", "/unsafe")
        for endpoint in ("https://127.0.0.1:123", "http://example.invalid:123", "http://user@127.0.0.1:123", "http://127.0.0.1:123/path"):
            with self.assertRaises(demo.DemoError):
                demo.endpoint_address(endpoint)


if __name__ == "__main__":
    unittest.main(verbosity=2)
