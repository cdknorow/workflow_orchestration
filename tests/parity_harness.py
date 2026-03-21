#!/usr/bin/env python3
"""Functional parity test harness.

Spins up both Python and Go backends with isolated temp directories,
runs identical API calls against both, then compares database state.

Usage:
    python tests/parity_harness.py [--scenarios tests/parity_scenarios.py]
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import signal
import subprocess
import sys
import tempfile
import time
from pathlib import Path

import httpx

# ── Configuration ─────────────────────────────────────────────────────

PY_PORT = 8420
GO_PORT = 8421
HEALTH_ENDPOINT = "/api/system/status"
STARTUP_TIMEOUT = 30  # seconds
SHUTDOWN_TIMEOUT = 10  # seconds

PROJECT_ROOT = Path(__file__).resolve().parent.parent


# ── Server Lifecycle ──────────────────────────────────────────────────

class ServerProcess:
    """Manages a backend server process with an isolated data directory."""

    def __init__(self, name: str, port: int, home_dir: Path):
        self.name = name
        self.port = port
        self.home_dir = home_dir
        self.proc: subprocess.Popen | None = None
        self.url = f"http://localhost:{port}"

    def start(self) -> None:
        raise NotImplementedError

    def stop(self) -> None:
        if self.proc is None:
            return
        print(f"  Stopping {self.name} (pid {self.proc.pid})...")
        self.proc.send_signal(signal.SIGTERM)
        try:
            self.proc.wait(timeout=SHUTDOWN_TIMEOUT)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait()
        self.proc = None

    def wait_healthy(self) -> bool:
        """Poll the health endpoint until it responds or timeout."""
        deadline = time.time() + STARTUP_TIMEOUT
        while time.time() < deadline:
            try:
                resp = httpx.get(f"{self.url}{HEALTH_ENDPOINT}", timeout=2.0)
                if resp.status_code == 200:
                    print(f"  {self.name} is healthy on port {self.port}")
                    return True
            except (httpx.ConnectError, httpx.ReadTimeout):
                pass
            time.sleep(0.5)
        print(f"  ERROR: {self.name} failed to start within {STARTUP_TIMEOUT}s")
        return False

    @property
    def db_path(self) -> Path:
        raise NotImplementedError

    @property
    def board_db_path(self) -> Path:
        raise NotImplementedError


class PythonServer(ServerProcess):
    """Python/FastAPI backend."""

    def __init__(self, port: int, home_dir: Path):
        super().__init__("Python", port, home_dir)

    def start(self) -> None:
        env = os.environ.copy()
        env["HOME"] = str(self.home_dir)
        env["CORAL_PORT"] = str(self.port)
        env["SSH_CONNECTION"] = "test"  # Suppress browser popup
        # Ensure the .coral directory exists
        (self.home_dir / ".coral").mkdir(parents=True, exist_ok=True)

        self.proc = subprocess.Popen(
            ["coral", "--host", "127.0.0.1", "--port", str(self.port)],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        print(f"  Started {self.name} (pid {self.proc.pid}) on port {self.port}")

    @property
    def db_path(self) -> Path:
        return self.home_dir / ".coral" / "sessions.db"

    @property
    def board_db_path(self) -> Path:
        return self.home_dir / ".coral" / "messageboard.db"


class GoServer(ServerProcess):
    """Go backend."""

    def __init__(self, port: int, home_dir: Path, binary: str | None = None):
        super().__init__("Go", port, home_dir)
        self.binary = binary or self._find_binary()

    def _find_binary(self) -> str:
        # Always rebuild to avoid stale binary issues
        print("  Building Go binary...")
        result = subprocess.run(
            ["go", "build", "-o", "/tmp/coral-go-parity-test", "./cmd/coral/"],
            cwd=str(PROJECT_ROOT / "coral-go"),
            capture_output=True, text=True,
        )
        if result.returncode == 0:
            return "/tmp/coral-go-parity-test"
        raise RuntimeError(f"Failed to build Go binary: {result.stderr}")

    def start(self) -> None:
        coral_dir = self.home_dir / ".coral-go"
        coral_dir.mkdir(parents=True, exist_ok=True)
        self._coral_dir = coral_dir

        env = os.environ.copy()
        env["HOME"] = str(self.home_dir)
        env["CORAL_DIR"] = str(coral_dir)
        env["CORAL_PORT"] = str(self.port)
        # Prevent browser popup
        env["SSH_CONNECTION"] = "test"

        self.proc = subprocess.Popen(
            [self.binary, "--host", "127.0.0.1", "--port", str(self.port), "--no-browser"],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        print(f"  Started {self.name} (pid {self.proc.pid}) on port {self.port}")

    @property
    def db_path(self) -> Path:
        return self.home_dir / ".coral-go" / "sessions.db"

    @property
    def board_db_path(self) -> Path:
        return self.home_dir / ".coral-go" / "messageboard.db"


# ── Test Runner ───────────────────────────────────────────────────────

class ParityResult:
    """Stores the result of a parity test scenario."""

    def __init__(self, name: str):
        self.name = name
        self.passed = True
        self.differences: list[str] = []

    def add_diff(self, msg: str) -> None:
        self.passed = False
        self.differences.append(msg)


def run_scenario(py_url: str, go_url: str, method: str, path: str,
                 body: dict | None = None, name: str = "") -> ParityResult:
    """Make the same API call to both backends and compare responses."""
    result = ParityResult(name or f"{method} {path}")
    client = httpx.Client(timeout=10.0)

    try:
        if method.upper() == "GET":
            py_resp = client.get(f"{py_url}{path}")
            go_resp = client.get(f"{go_url}{path}")
        elif method.upper() == "POST":
            py_resp = client.post(f"{py_url}{path}", json=body)
            go_resp = client.post(f"{go_url}{path}", json=body)
        elif method.upper() == "PUT":
            py_resp = client.put(f"{py_url}{path}", json=body)
            go_resp = client.put(f"{go_url}{path}", json=body)
        elif method.upper() == "PATCH":
            py_resp = client.patch(f"{py_url}{path}", json=body)
            go_resp = client.patch(f"{go_url}{path}", json=body)
        elif method.upper() == "DELETE":
            py_resp = client.delete(f"{py_url}{path}", json=body)
            go_resp = client.delete(f"{go_url}{path}", json=body)
        else:
            result.add_diff(f"Unknown method: {method}")
            return result

        # Compare status codes
        if py_resp.status_code != go_resp.status_code:
            result.add_diff(
                f"Status code: Python={py_resp.status_code}, Go={go_resp.status_code}"
            )

        # Compare response bodies (normalize away timestamps/UUIDs)
        try:
            py_json = py_resp.json()
            go_json = go_resp.json()
            diffs = compare_json(py_json, go_json, path="$")
            for d in diffs:
                result.add_diff(d)
        except Exception as e:
            result.add_diff(f"JSON parse error: {e}")

    except Exception as e:
        result.add_diff(f"Request error: {e}")
    finally:
        client.close()

    return result


def compare_json(py_val, go_val, path: str = "$") -> list[str]:
    """Recursively compare two JSON values, ignoring timestamps and UUIDs."""
    diffs = []

    # Skip fields that are expected to differ
    skip_fields = {"created_at", "updated_at", "subscribed_at", "recorded_at",
                   "session_id", "id", "startup_time", "uptime_seconds",
                   "started_at", "finished_at", "version", "scheduled_at",
                   "path"}

    if isinstance(py_val, dict) and isinstance(go_val, dict):
        all_keys = set(py_val.keys()) | set(go_val.keys())
        for key in sorted(all_keys):
            if key in skip_fields:
                continue
            if key not in py_val:
                diffs.append(f"{path}.{key}: missing in Python response")
            elif key not in go_val:
                diffs.append(f"{path}.{key}: missing in Go response")
            else:
                diffs.extend(compare_json(py_val[key], go_val[key], f"{path}.{key}"))
    elif isinstance(py_val, list) and isinstance(go_val, list):
        if len(py_val) != len(go_val):
            diffs.append(f"{path}: array length Python={len(py_val)}, Go={len(go_val)}")
        else:
            for i in range(len(py_val)):
                diffs.extend(compare_json(py_val[i], go_val[i], f"{path}[{i}]"))
    elif py_val != go_val:
        # Check if both are numeric and approximately equal
        if isinstance(py_val, (int, float)) and isinstance(go_val, (int, float)):
            if abs(py_val - go_val) < 0.01:
                return diffs
        diffs.append(f"{path}: Python={py_val!r}, Go={go_val!r}")

    return diffs


# ── Main Harness ──────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="Functional parity test harness")
    parser.add_argument("--scenarios", type=str, help="Path to scenarios module")
    parser.add_argument("--go-binary", type=str, help="Path to Go binary")
    parser.add_argument("--py-port", type=int, default=8430, help="Python server port")
    parser.add_argument("--go-port", type=int, default=8431, help="Go server port")
    parser.add_argument("--skip-python", action="store_true", help="Skip Python backend")
    parser.add_argument("--skip-go", action="store_true", help="Skip Go backend")
    args = parser.parse_args()

    py_port = args.py_port
    go_port = args.go_port

    # Create temp directories
    py_home = Path(tempfile.mkdtemp(prefix="coral-parity-py-"))
    go_home = Path(tempfile.mkdtemp(prefix="coral-parity-go-"))

    print(f"Python home: {py_home}")
    print(f"Go home:     {go_home}")

    py_server = PythonServer(py_port, py_home)
    go_server = GoServer(go_port, go_home, binary=args.go_binary)

    try:
        # Start servers
        print("\n=== Starting servers ===")
        py_server.start()
        go_server.start()

        # Wait for health
        print("\n=== Waiting for servers to be healthy ===")
        py_healthy = py_server.wait_healthy()
        go_healthy = go_server.wait_healthy()

        if not py_healthy or not go_healthy:
            print("ERROR: One or both servers failed to start")
            sys.exit(1)

        # Run test scenarios
        print("\n=== Running test scenarios ===")

        # Run Backend Dev's comprehensive scenarios if available
        scenarios_path = args.scenarios or str(PROJECT_ROOT / "tests" / "parity" / "test_scenarios.py")
        ext_results = []
        if os.path.isfile(scenarios_path):
            import importlib.util
            spec = importlib.util.spec_from_file_location("parity_scenarios", scenarios_path)
            mod = importlib.util.module_from_spec(spec)
            sys.modules["parity_scenarios"] = mod
            spec.loader.exec_module(mod)
            if hasattr(mod, "run_all_scenarios"):
                print(f"  Loading external scenarios from {scenarios_path}")
                comp_results = mod.run_all_scenarios(py_port=py_port, go_port=go_port)
                # Convert ComparisonResult to ParityResult
                for cr in comp_results:
                    pr = ParityResult(f"{cr.method} {cr.endpoint} ({cr.scenario})")
                    pr.passed = cr.passed
                    if not cr.passed:
                        if cr.py_status != cr.go_status:
                            pr.differences.append(f"Status: Python={cr.py_status}, Go={cr.go_status}")
                        if cr.diff:
                            pr.differences.append(cr.diff)
                    ext_results.append(pr)
                print(f"  External scenarios: {len(ext_results)} tests")
            else:
                print(f"  WARNING: {scenarios_path} has no run_all_scenarios()")

        # Run external scenarios first (they manage their own cleanup),
        # then built-in scenarios.  This avoids state leaks between suites.
        results = list(ext_results)
        results.extend(run_default_scenarios(py_server.url, go_server.url))

        # Print results
        print("\n=== Results ===")
        passed = sum(1 for r in results if r.passed)
        failed = sum(1 for r in results if not r.passed)

        for r in results:
            status = "PASS" if r.passed else "FAIL"
            print(f"  [{status}] {r.name}")
            if not r.passed:
                for d in r.differences:
                    print(f"         {d}")

        print(f"\n{passed} passed, {failed} failed, {len(results)} total")

        # Stop servers
        print("\n=== Stopping servers ===")
        py_server.stop()
        go_server.stop()

        # Compare databases using Go Expert's db-compare tool
        print("\n=== Comparing databases ===")
        db_compare = str(PROJECT_ROOT / "coral-go" / "cmd" / "db-compare" / "db-compare")
        if not os.path.isfile(db_compare):
            # Try building it
            subprocess.run(
                ["go", "build", "-o", db_compare, "./cmd/db-compare/"],
                cwd=str(PROJECT_ROOT / "coral-go"),
                capture_output=True,
            )

        if os.path.isfile(db_compare):
            db_result = subprocess.run(
                [db_compare,
                 str(py_server.db_path), str(go_server.db_path),
                 str(py_server.board_db_path), str(go_server.board_db_path)],
                capture_output=True, text=True,
            )
            print(db_result.stdout)
            if db_result.stderr:
                print(db_result.stderr)
            if db_result.returncode != 0:
                failed += 1
        else:
            print("  WARNING: db-compare tool not found, skipping DB comparison")
            print(f"  Python DB: {py_server.db_path}")
            print(f"  Go DB:     {go_server.db_path}")

        sys.exit(0 if failed == 0 else 1)

    except KeyboardInterrupt:
        print("\nInterrupted")
    finally:
        py_server.stop()
        go_server.stop()
        # Clean up temp dirs
        print(f"\nTemp dirs preserved for inspection:")
        print(f"  Python: {py_home}")
        print(f"  Go:     {go_home}")


def run_default_scenarios(py_url: str, go_url: str) -> list[ParityResult]:
    """Run the built-in test scenarios."""
    results = []

    # 1. System status
    results.append(run_scenario(py_url, go_url, "GET", "/api/system/status",
                                name="System status"))

    # 2. Settings CRUD
    results.append(run_scenario(py_url, go_url, "GET", "/api/settings",
                                name="Get settings (empty)"))
    results.append(run_scenario(py_url, go_url, "PUT", "/api/settings",
                                body={"theme": "dark", "sidebar_collapsed": True},
                                name="Put settings"))
    results.append(run_scenario(py_url, go_url, "GET", "/api/settings",
                                name="Get settings (after put)"))

    # 3. Tags CRUD
    results.append(run_scenario(py_url, go_url, "POST", "/api/tags",
                                body={"name": "test-tag", "color": "#ff0000"},
                                name="Create tag"))
    results.append(run_scenario(py_url, go_url, "GET", "/api/tags",
                                name="List tags"))

    # 4. Webhooks CRUD
    results.append(run_scenario(py_url, go_url, "POST", "/api/webhooks",
                                body={"name": "test-hook", "url": "http://localhost:9999/hook", "events": ["session.idle"]},
                                name="Create webhook"))
    results.append(run_scenario(py_url, go_url, "GET", "/api/webhooks",
                                name="List webhooks"))

    # 5. Scheduled jobs CRUD
    results.append(run_scenario(py_url, go_url, "POST", "/api/scheduled/jobs",
                                body={
                                    "name": "test-job", "cron_expr": "0 * * * *",
                                    "timezone": "UTC", "agent_type": "claude",
                                    "repo_path": "/tmp", "prompt": "test",
                                },
                                name="Create scheduled job"))
    results.append(run_scenario(py_url, go_url, "GET", "/api/scheduled/jobs",
                                name="List scheduled jobs"))

    # 6. Board operations
    results.append(run_scenario(py_url, go_url, "POST", "/api/board/test-project/subscribe",
                                body={"session_id": "test-sess-1", "job_title": "Tester"},
                                name="Board subscribe"))
    results.append(run_scenario(py_url, go_url, "POST", "/api/board/test-project/messages",
                                body={"session_id": "test-sess-1", "content": "Hello from parity test"},
                                name="Board post message"))
    results.append(run_scenario(py_url, go_url, "GET", "/api/board/test-project/messages/all?limit=50",
                                name="Board list messages"))
    results.append(run_scenario(py_url, go_url, "GET", "/api/board/projects",
                                name="Board list projects"))

    # 7. Board pause/resume
    results.append(run_scenario(py_url, go_url, "POST", "/api/board/test-project/pause",
                                name="Board pause"))
    results.append(run_scenario(py_url, go_url, "GET", "/api/board/test-project/paused",
                                name="Board get paused"))
    results.append(run_scenario(py_url, go_url, "POST", "/api/board/test-project/resume",
                                name="Board resume"))

    # 8. History (empty — should return empty lists)
    results.append(run_scenario(py_url, go_url, "GET", "/api/sessions/history",
                                name="History list (empty)"))

    # 9. Themes
    results.append(run_scenario(py_url, go_url, "GET", "/api/themes",
                                name="List themes"))

    return results


if __name__ == "__main__":
    main()
