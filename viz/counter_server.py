#!/usr/bin/env python3
"""
Tiny HTTP server for the FaultWall demo row-counter overlay.

Serves:
  GET /            -> counter.html  (big-number display, polls /count)
  GET /count       -> {"users": <int>, "payments": <int>, "ts": <iso>}

The counter queries Postgres DIRECTLY (bypassing FaultWall) so the display
reflects the true state of the database, not what the proxy would let you see.
This is intentional: the video needs to show the real impact of a DROP TABLE
or DELETE, independent of the policy layer.
"""

import json
import os
import subprocess
from datetime import datetime, timezone
from http.server import HTTPServer, BaseHTTPRequestHandler

PG_HOST = os.environ.get("PG_HOST", "postgres")
PG_PORT = os.environ.get("PG_PORT", "5432")
PG_USER = os.environ.get("PG_USER", "ghost")
PG_PASS = os.environ.get("PG_PASS", "ghostpass")
PG_DB = os.environ.get("PG_DB", "faultwall_demo")
LISTEN_PORT = int(os.environ.get("VIZ_PORT", "8082"))

HTML_PATH = "/app/counter.html"


def query_count(table: str) -> int:
    """Return COUNT(*) for the given table, or -1 if the table does not exist."""
    env = os.environ.copy()
    env["PGPASSWORD"] = PG_PASS
    try:
        result = subprocess.run(
            [
                "psql",
                "-h", PG_HOST, "-p", PG_PORT,
                "-U", PG_USER, "-d", PG_DB,
                "-tA", "-c", f"SELECT COUNT(*) FROM public.{table}",
            ],
            env=env,
            capture_output=True,
            text=True,
            timeout=2,
        )
    except subprocess.TimeoutExpired:
        return -1

    if result.returncode != 0:
        # Table missing, permission denied, etc. Return -1 so the UI shows DROPPED.
        return -1
    try:
        return int(result.stdout.strip())
    except (ValueError, AttributeError):
        return -1


class VizHandler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):  # quiet access log
        return

    def _cors(self):
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")

    def do_OPTIONS(self):
        self.send_response(200)
        self._cors()
        self.end_headers()

    def do_GET(self):
        if self.path in ("/", "/index.html"):
            with open(HTML_PATH, "rb") as f:
                body = f.read()
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self._cors()
            self.end_headers()
            self.wfile.write(body)
            return

        if self.path == "/count":
            users = query_count("users")
            payments = query_count("payments")
            feedback = query_count("feedback")
            body = json.dumps({
                "users": users,
                "payments": payments,
                "feedback": feedback,
                "ts": datetime.now(timezone.utc).isoformat(),
            }).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self._cors()
            self.end_headers()
            self.wfile.write(body)
            return

        self.send_error(404, "Not Found")


def main():
    srv = HTTPServer(("0.0.0.0", LISTEN_PORT), VizHandler)
    print(f"FaultWall viz counter listening on 0.0.0.0:{LISTEN_PORT}")
    print(f"Polling postgres at {PG_HOST}:{PG_PORT}/{PG_DB} as {PG_USER}")
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        srv.shutdown()


if __name__ == "__main__":
    main()
