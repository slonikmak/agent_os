#!/usr/bin/env python3
"""Foreground native smoke in a fresh private root; prints no credentials."""
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

binary = Path("bin/Personal Desk.app/Contents/MacOS/personal-desk").resolve()
root = Path(tempfile.mkdtemp(prefix="personal-desk-smoke-")).resolve()
environment = os.environ.copy()
environment.update(PERSONAL_DESK_WORKSPACE=str(root), PERSONAL_DESK_ADDRESS="127.0.0.1:0")

def launch():
    process = subprocess.Popen([str(binary)], env=environment, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(100):
        if (root / "discovery.json").exists():
            # HTTP is ready before Wails finishes installing its native signal handler.
            time.sleep(1)
            return process
        if process.poll() is not None:
            raise RuntimeError("native process exited before API readiness")
        time.sleep(0.1)
    process.terminate()
    raise RuntimeError("native API readiness timeout")

def read():
    discovery = json.loads((root / "discovery.json").read_text())
    credentials = json.loads((root / "credentials.json").read_text())
    request = urllib.request.Request(discovery["endpoint"] + "/api/v1/workspace", headers={"Authorization": "Bearer " + credentials["reader"]})
    with urllib.request.urlopen(request, timeout=3) as response:
        return json.load(response), discovery["endpoint"]

def stop(process, endpoint):
    process.send_signal(signal.SIGTERM)
    process.wait(timeout=8)
    assert not (root / "discovery.json").exists(), "discovery survived quit"
    try:
        urllib.request.urlopen(endpoint + "/api/v1", timeout=1)
    except urllib.error.URLError:
        return
    raise AssertionError("listener survived quit")

process = launch()
try:
    first, endpoint = read()
    assert first["workspaceRevision"] == 1
    assert first["tasks"] == first["inbox"] == first["sources"] == []
    assert all(value == 0 for value in first["counts"].values())
    setup = json.loads((root / "credentials.json").read_text())
    discover_request = urllib.request.Request(endpoint + "/api/v1", headers={"Authorization": "Bearer " + setup["reader"]})
    with urllib.request.urlopen(discover_request, timeout=3) as response:
        assert json.load(response)["capabilities"] == ["workspace.read", "tasks.read", "inbox.read", "inbox.receive", "inbox.resolve.createTask", "materials.read", "materials.upload"]
    credential_hash = hashlib.sha256((root / "credentials.json").read_bytes()).digest()
    for _ in range(3):
        assert read()[0] == first
    stop(process, endpoint)
    process = launch()
    second, endpoint = read()
    assert first == second
    assert credential_hash == hashlib.sha256((root / "credentials.json").read_bytes()).digest()
    stop(process, endpoint)
    print("PASS native launch, empty saved reads, quit, stable restart, private credentials")
    print("Isolated workspace retained:", root)
finally:
    if process.poll() is None:
        process.terminate()
        process.wait(timeout=8)
