#!/usr/bin/env python3
"""Offline contract tests for the read-only staging smoke script."""
import http.server
import json
import os
import urllib.parse
import subprocess
import threading
import unittest
from pathlib import Path

SMOKE = Path(__file__).with_name("staging-smoke.sh")

class Endpoint(http.server.BaseHTTPRequestHandler):
    reason = "business_api_not_migrated"
    def do_GET(self):
        if self.path == "/health/live":
            status, body = 200, {"status": "alive"}
        elif self.path == "/health/ready":
            status, body = 503, {"status": "not_ready", "reason": type(self).reason}
        elif self.path == "/api/session":
            if self.headers.get("Cookie") == "it_map_session=synthetic-verified":
                status, body = 200, {"name": "Eugene", "role": "admin", "isAdmin": True}
            else:
                status, body = 401, {"error": "authentication required"}
        elif self.path == "/api/companies/list":
            status, body = 401, {"error": "authentication required"}
        else:
            status, body = 404, {"error": "not_found"}
        payload = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", "0"))).decode()
        form = urllib.parse.parse_qs(body)
        if self.path == "/auth" and form.get("login") == ["eugene"] and form.get("password") == ["TEST_SYNTHETIC_PASSWORD"]:
            self.send_response(303)
            self.send_header("Set-Cookie", "it_map_session=synthetic-verified; Path=/; HttpOnly; Secure")
            self.send_header("Location", "/")
            self.end_headers()
        else:
            self.send_response(401)
            self.end_headers()

    def log_message(self, *_args):
        pass

class StagingSmokeTests(unittest.TestCase):
    def test_ready_gate_is_closed_only_after_db_connected(self):
        httpd = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Endpoint)
        worker = threading.Thread(target=httpd.serve_forever, daemon=True)
        worker.start()
        url = f"http://127.0.0.1:{httpd.server_port}"
        try:
            Endpoint.reason = "business_api_not_migrated"
            good = subprocess.run(["bash", str(SMOKE), url], capture_output=True, text=True, timeout=15)
            self.assertEqual(good.returncode, 0, good.stderr)
            Endpoint.reason = "mariadb_not_connected"
            missing = subprocess.run(["bash", str(SMOKE), url], capture_output=True, text=True, timeout=15)
            self.assertNotEqual(missing.returncode, 0)
            Endpoint.reason = "mariadb_unavailable"
            down = subprocess.run(["bash", str(SMOKE), url], capture_output=True, text=True, timeout=15)
            self.assertNotEqual(down.returncode, 0)
            auth = subprocess.run(["bash", str(SMOKE), url], capture_output=True, text=True, timeout=15, env={
                **os.environ,
                "ITMAP_SMOKE_AUTH": "1", "ITMAP_SMOKE_LOGIN": "eugene", "ITMAP_SMOKE_PASSWORD": "TEST_SYNTHETIC_PASSWORD",
            })
            # The previous case intentionally left readiness unavailable.
            self.assertNotEqual(auth.returncode, 0)
            Endpoint.reason = "business_api_not_migrated"
            auth = subprocess.run(["bash", str(SMOKE), url], capture_output=True, text=True, timeout=15, env={
                **os.environ,
                "ITMAP_SMOKE_AUTH": "1", "ITMAP_SMOKE_LOGIN": "eugene", "ITMAP_SMOKE_PASSWORD": "TEST_SYNTHETIC_PASSWORD",
            })
            self.assertEqual(auth.returncode, 0, auth.stderr)
        finally:
            httpd.shutdown()
            httpd.server_close()
            worker.join(timeout=5)

if __name__ == "__main__":
    unittest.main()
