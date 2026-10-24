#!/usr/bin/env python3
"""Offline contract tests for the read-only staging smoke script."""
import http.server
import json
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
        else:
            status, body = 404, {"error": "not_found"}
        payload = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)
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
        finally:
            httpd.shutdown()
            httpd.server_close()
            worker.join(timeout=5)

if __name__ == "__main__":
    unittest.main()
