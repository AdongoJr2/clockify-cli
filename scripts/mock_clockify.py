#!/usr/bin/env python3
"""Mock Clockify API server for trying out the clockify CLI without an account.

Usage:
    python3 scripts/mock_clockify.py            # listens on 127.0.0.1:8977

Then, in another terminal (from the repo root):

    export CLOCKIFY_API_KEY=mock-key
    export CLOCKIFY_WORKSPACE_ID=w1
    export CLOCKIFY_BASE_URL=http://127.0.0.1:8977
    ./clockify list
    ./clockify log -d "Demo work" --start 14:00 --duration 1h -p "Website Redesign"
    ./clockify start -d "Demo timer"
    ./clockify stop

In-memory only: restart to reset state. Not for production use.
"""

import json
from http.server import BaseHTTPRequestHandler, HTTPServer

USER = {"id": "u1", "name": "Demo User", "email": "demo@example.com", "activeWorkspace": "w1"}
WORKSPACES = [{"id": "w1", "name": "My Workspace"}]
PROJECTS = [
    {"id": "p1", "name": "Website Redesign", "billable": True},
    {"id": "p2", "name": "Backend API", "billable": True},
]
ENTRIES = [
    {
        "id": "e1",
        "description": "Deep work",
        "billable": True,
        "timeInterval": {
            "start": "2026-10-05T09:00:00Z",
            "end": "2026-10-05T10:30:00Z",
            "duration": "PT1H30M",
        },
    },
    {
        "id": "e2",
        "description": "Standup",
        "billable": False,
        "project": {"id": "p1", "name": "Website Redesign", "clientName": "Acme"},
        "timeInterval": {
            "start": "2026-10-05T08:30:00Z",
            "end": "2026-10-05T08:45:00Z",
            "duration": "PT15M",
        },
    },
]
RUNNING = []  # populated by POST without end (start timer)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        print(f"[mock] {self.command} {self.path}")

    def _send(self, obj, status=200):
        data = json.dumps(obj).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def _body(self):
        length = int(self.headers.get("Content-Length", 0))
        return json.loads(self.rfile.read(length) or b"{}")

    def do_GET(self):
        path = self.path.split("?")[0]
        if path == "/user":
            self._send(USER)
        elif path == "/workspaces":
            self._send(WORKSPACES)
        elif path.endswith("/projects"):
            self._send(PROJECTS)
        elif path.endswith("/time-entries/status/in-progress"):
            self._send(RUNNING)
        elif "/time-entries" in path:
            self._send(ENTRIES)
        else:
            self._send({"message": f"no mock for GET {path}"}, 404)

    def do_POST(self):
        body = self._body()
        if self.path.endswith("/time-entries"):
            entry = {"id": "e-new", "description": body.get("description", ""),
                     "billable": body.get("billable", True),
                     "timeInterval": {"start": body.get("start"), "end": body.get("end")}}
            if not body.get("end"):  # timer started → mark as running
                RUNNING.clear()
                RUNNING.append({**entry, "userId": USER["id"]})
            self._send(entry, 201)
        else:
            self._send({"message": f"no mock for POST {self.path}"}, 404)

    def do_PATCH(self):
        body = self._body()
        stopped = {**RUNNING[0], "timeInterval": {**RUNNING[0].get("timeInterval", {}),
                                                   "end": body.get("end")}} if RUNNING else {"id": "e-stopped"}
        RUNNING.clear()
        self._send(stopped)

    def do_PUT(self):
        self._send({"id": "e-updated", "description": self._body().get("description", "")})


if __name__ == "__main__":
    host, port = "127.0.0.1", 8977
    print(f"Mock Clockify API listening on http://{host}:{port}")
    print("Point the CLI at it:  CLOCKIFY_BASE_URL=http://127.0.0.1:8977 ./clockify list")
    HTTPServer((host, port), Handler).serve_forever()
