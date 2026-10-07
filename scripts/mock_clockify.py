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
from urllib.parse import parse_qs, urlparse

USER = {"id": "u1", "name": "Demo User", "email": "demo@example.com", "activeWorkspace": "w1"}
WORKSPACES = [{"id": "w1", "name": "My Workspace"}]
PROJECTS = [
    {"id": "p1", "name": "Website Redesign", "billable": True},
    {"id": "p2", "name": "Backend API", "billable": True},
    {"id": "p3", "name": "Old Website", "billable": False, "archived": True},
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
_next_id = 1  # counter for entries created via POST


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
            # Mirror the real API: ?name= is a substring filter,
            # ?archived=true returns archived projects only.
            params = parse_qs(urlparse(self.path).query)
            out = list(PROJECTS)
            if params.get("name"):
                needle = params["name"][0].lower()
                out = [p for p in out if needle in p["name"].lower()]
            if params.get("archived") == ["true"]:
                out = [p for p in out if p.get("archived")]
            self._send(out)
        elif path.endswith("/time-entries/status/in-progress"):
            self._send(RUNNING)
        elif "/time-entries/" in path:  # single entry: .../time-entries/{id}
            entry_id = path.rsplit("/", 1)[-1]
            entry = next((e for e in ENTRIES if e["id"] == entry_id), None)
            if entry is None:
                self._send({"message": "time entry not found"}, 404)
            else:
                self._send(entry)
        elif "/time-entries" in path:
            # Honor ?start= / ?end= filters (ISO-8601, compared as strings).
            query = parse_qs(urlparse(self.path).query)
            items = ENTRIES
            start = query.get("start", [None])[0]
            end = query.get("end", [None])[0]
            if start:
                items = [e for e in items if (e.get("timeInterval") or {}).get("start", "") >= start]
            if end:
                items = [e for e in items if (e.get("timeInterval") or {}).get("start", "") <= end]
            self._send([self._entry_view(e) for e in items])
        else:
            self._send({"message": f"no mock for GET {path}"}, 404)

    def do_DELETE(self):
        entry_id = self.path.rsplit("/", 1)[-1]
        before = len(ENTRIES)
        ENTRIES[:] = [e for e in ENTRIES if e["id"] != entry_id]
        if len(ENTRIES) == before:
            self._send({"message": "time entry not found"}, 404)
            return
        self.send_response(204)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def _entry_view(self, entry):
        """Attach the project name so lists look like the real hydrated API."""
        view = dict(entry)
        pid = view.get("projectId")
        if pid and not view.get("project"):
            project = next((p for p in PROJECTS if p["id"] == pid), None)
            if project:
                view["project"] = {"id": project["id"], "name": project["name"]}
        return view

    def do_POST(self):
        global _next_id
        body = self._body()
        if self.path.endswith("/duplicate"):
            # POST .../time-entries/{id}/duplicate — copy the entry with a fresh id.
            parts = self.path.split("?")[0].rstrip("/").split("/")
            src_id = parts[-2] if len(parts) >= 2 else ""
            src = next((e for e in ENTRIES if e["id"] == src_id), None)
            if src is None:
                self._send({"message": "time entry not found"}, 404)
                return
            dup = {**src, "id": "e-mock-dup"}
            ENTRIES.insert(0, dup)
            self._send(dup, 201)
        elif self.path.endswith("/time-entries"):
            desc = body.get("description", "")
            # Failure injection for demos: descriptions containing "fail" are rejected.
            if "fail" in desc.lower():
                self._send({"message": "entry rejected by mock"}, 400)
                return
            entry = {"id": f"e-mock-{_next_id}", "description": desc,
                     "billable": body.get("billable", True),
                     "timeInterval": {"start": body.get("start"), "end": body.get("end")}}
            _next_id += 1
            if body.get("projectId"):
                entry["projectId"] = body["projectId"]
            ENTRIES.insert(0, entry)
            if not body.get("end"):  # timer started → mark as running
                RUNNING.clear()
                RUNNING.append({**entry, "userId": USER["id"]})
            self._send(self._entry_view(entry), 201)
        else:
            self._send({"message": f"no mock for POST {self.path}"}, 404)

    def do_PATCH(self):
        body = self._body()
        stopped = {**RUNNING[0], "timeInterval": {**RUNNING[0].get("timeInterval", {}),
                                                   "end": body.get("end")}} if RUNNING else {"id": "e-stopped"}
        RUNNING.clear()
        self._send(stopped)

    def do_PUT(self):
        body = self._body()
        entry = {**ENTRIES[0], "description": body.get("description", ENTRIES[0]["description"])}
        ti = dict(ENTRIES[0]["timeInterval"])
        if body.get("start"):
            ti["start"] = body["start"]
        if "end" in body:
            ti["end"] = body["end"]
        entry["timeInterval"] = ti
        ENTRIES[0] = entry
        self._send(entry)


if __name__ == "__main__":
    host, port = "127.0.0.1", 8977
    print(f"Mock Clockify API listening on http://{host}:{port}")
    print("Point the CLI at it:  CLOCKIFY_BASE_URL=http://127.0.0.1:8977 ./clockify list")
    HTTPServer((host, port), Handler).serve_forever()
