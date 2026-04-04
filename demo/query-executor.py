#!/usr/bin/env python3
"""
Tiny HTTP server that executes SQL queries through FaultWall's wire-protocol proxy.
Dashboard attack buttons POST here → this runs psql against port 5433 → FaultWall intercepts.

POST /execute
{
  "agent_id": "data-analyst",
  "query": "DROP TABLE customers"
}

Returns:
{
  "agent_id": "data-analyst",
  "query": "DROP TABLE customers",
  "output": "ERROR: BLOCKED by FaultWall...",
  "blocked": true,
  "timestamp": "2026-04-03T..."
}
"""

import json
import os
import subprocess
import sys
from datetime import datetime, timezone
from http.server import HTTPServer, BaseHTTPRequestHandler

PROXY_HOST = os.environ.get("PROXY_HOST", "faultwall-proxy")
PROXY_PORT = os.environ.get("PROXY_PORT", "5433")
PG_USER = os.environ.get("PG_USER", "ghost")
PG_PASS = os.environ.get("PG_PASS", "ghostpass")
PG_DB = os.environ.get("PG_DB", "faultwall_demo")
LISTEN_PORT = int(os.environ.get("EXECUTOR_PORT", "9090"))


class QueryHandler(BaseHTTPRequestHandler):
    def do_OPTIONS(self):
        self.send_response(200)
        self._cors()
        self.end_headers()

    def do_POST(self):
        if self.path != "/execute":
            self.send_error(404)
            return

        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)

        try:
            data = json.loads(body)
        except json.JSONDecodeError:
            self.send_error(400, "Invalid JSON")
            return

        agent_id = data.get("agent_id", "unknown")
        query = data.get("query", "")

        if not query:
            self.send_error(400, "Missing query")
            return

        # Build application_name in FaultWall's format
        app_name = f"agent:{agent_id}:mission:demo-attack"

        # Execute through psql → FaultWall proxy
        try:
            result = subprocess.run(
                [
                    "psql",
                    "-h", PROXY_HOST,
                    "-p", PROXY_PORT,
                    "-U", PG_USER,
                    "-d", PG_DB,
                    f"--set=application_name={app_name}",
                    "-c", query,
                ],
                env={**os.environ, "PGPASSWORD": PG_PASS},
                capture_output=True,
                text=True,
                timeout=10,
            )
            output = result.stdout + result.stderr
            exit_code = result.returncode
        except subprocess.TimeoutExpired:
            output = "Query timed out (10s)"
            exit_code = 1
        except FileNotFoundError:
            output = "psql not found — install postgresql-client"
            exit_code = 1

        blocked = any(
            kw in output.lower()
            for kw in ["blocked by faultwall", "fatal", "connection", "permission denied"]
        )

        response = {
            "agent_id": agent_id,
            "query": query,
            "output": output.strip(),
            "blocked": blocked,
            "exit_code": exit_code,
            "timestamp": datetime.now(timezone.utc).isoformat(),
        }

        self.send_response(200)
        self._cors()
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(response).encode())

    def do_GET(self):
        if self.path == "/health":
            self.send_response(200)
            self._cors()
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"status": "ok"}).encode())
        else:
            self.send_error(404)

    def _cors(self):
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")

    def log_message(self, format, *args):
        print(f"[executor] {args[0]}", file=sys.stderr)


if __name__ == "__main__":
    server = HTTPServer(("0.0.0.0", LISTEN_PORT), QueryHandler)
    print(f"[executor] Query executor listening on :{LISTEN_PORT}", file=sys.stderr)
    print(f"[executor] Proxying to {PROXY_HOST}:{PROXY_PORT}", file=sys.stderr)
    server.serve_forever()
