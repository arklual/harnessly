#!/usr/bin/env python3
"""Exercise an installed runtime and an opt-in Go browser test without touching the account's state."""
import argparse
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import urllib.request

parser = argparse.ArgumentParser()
parser.add_argument("--runtime", required=True)
parser.add_argument("--test-binary", required=True)
args = parser.parse_args()
with tempfile.TemporaryDirectory(prefix="browser-install-") as temp:
    root = Path(temp)
    home = root / "home"
    home.mkdir()
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    env = {
        "HOME": str(home), "PATH": "/usr/local/bin:/usr/bin:/bin", "LANG": "C.UTF-8",
        "AGENT_DATA_DIR": str(root / "data"), "RUNTIME_GATE_LISTEN": f"127.0.0.1:{port}",
        "RUNTIME_GATE_UPSTREAM": "", "RUNTIME_APP_BASE_PATH": "/", "RUNTIME_AGENT_SKILLS": "off",
        "RUNTIME_TMUX_SOCKET": f"hs-test-browser-{os.getpid()}",
        "RUNTIME_BROWSER_CHROME_ARGS": os.environ.get("RUNTIME_BROWSER_CHROME_ARGS", ""),
        "AGENT_BROWSER_TEST_NO_XVFB": os.environ.get("AGENT_BROWSER_TEST_NO_XVFB", ""),
        "AGENT_BROWSER_DOWNLOAD_TEST": "1", "AGENT_BROWSER_TEST_RUNTIME": args.runtime,
    }
    with (root / "runtime.log").open("w+") as log:
        runtime = subprocess.Popen([args.runtime], env=env, stdout=log, stderr=log)
        try:
            for attempt in range(60):
                try:
                    with urllib.request.urlopen(f"http://127.0.0.1:{port}/api/health", timeout=1) as response:
                        print("Runtime health:", response.status, flush=True)
                    break
                except OSError:
                    if runtime.poll() is not None:
                        log.seek(0)
                        raise RuntimeError(log.read())
                    time.sleep(0.5)
            else:
                raise RuntimeError("runtime did not become healthy")
            subprocess.run([args.test_binary, "-test.v", "-test.run=^TestProvisionedChrome$"],
                           env=env, check=True, timeout=600)
        finally:
            runtime.terminate()
            try:
                runtime.wait(timeout=15)
            except subprocess.TimeoutExpired:
                runtime.kill()
                runtime.wait()
