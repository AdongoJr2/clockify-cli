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
    ./clockify tags
    ./clockify tasks -p "Website Redesign"
    ./clockify log -d "Tagged" --start 15:00 --duration 1h -p "Website Redesign" --tag deep-work --task "Fix login"
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
TAGS = [
    {"id": "t1", "name": "deep-work", "workspaceId": "w1"},
    {"id": "t2", "name": "review", "workspaceId": "w1"},
    {"id": "t3", "name": "legacy", "workspaceId": "w1", "archived": True},
]
TASKS = [
    {"id": "k1", "name": "Fix login", "projectId": "p1", "status": "ACTIVE", "estimate": "PT2H"},
    {"id": "k2", "name": "Write docs", "projectId": "p1", "status": "ACTIVE"},
    {"id": "k3", "name": "Design review", "projectId": "p1", "status": "DONE"},
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
        "projectId": "p1",
        "timeInterval": {
            "start": "2026-10-05T08:30:00Z",
            "end": "2026-10-05T08:45:00Z",
            "duration": "PT15M",
        },
    },
    {
        "id": "e3",
        "description": "API client rewrite",
        "billable": True,
        "projectId": "p2",
        "tags": [{"id": "t1", "name": "deep-work"}],
        "timeInterval": {
            "start": "2026-10-06T11:00:00Z",
            "end": "2026-10-06T12:30:00Z",
            "duration": "PT1H30M",
        },
    },
    {
        "id": "e4",
        "description": "Design review prep",
        "billable": True,
        "projectId": "p1",
        "tags": [{"id": "t2", "name": "review"}],
        "timeInterval": {
            "start": "2026-10-07T13:00:00Z",
            "end": "2026-10-07T14:00:00Z",
            "duration": "PT1H",
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
        elif path.endswith("/tags"):
            # Workspace tags: ?name= substring filter (?strict-name-search=true
            # for exact), ?archived=true returns archived tags only.
            params = parse_qs(urlparse(self.path).query)
            out = list(TAGS)
            if params.get("name"):
                needle = params["name"][0].lower()
                if params.get("strict-name-search") == ["true"]:
                    out = [t for t in out if t["name"].lower() == needle]
                else:
                    out = [t for t in out if needle in t["name"].lower()]
            if params.get("archived") == ["true"]:
                out = [t for t in out if t.get("archived")]
            self._send(out)
        elif "/tags/" in path:
            tag_id = path.rsplit("/", 1)[-1]
            tag = next((t for t in TAGS if t["id"] == tag_id), None)
            if tag is None:
                self._send({"message": "tag not found"}, 404)
            else:
                self._send(tag)
        elif path.endswith("/tasks"):
            # /workspaces/{ws}/projects/{pid}/tasks
            pid = path.split("/")[-2]
            params = parse_qs(urlparse(self.path).query)
            out = [t for t in TASKS if t.get("projectId") == pid]
            if params.get("name"):
                needle = params["name"][0].lower()
                out = [t for t in out if needle in t["name"].lower()]
            if params.get("is-active") == ["true"]:
                out = [t for t in out if t.get("status") == "ACTIVE"]
            self._send(out)
        elif "/tasks/" in path:
            task_id = path.rsplit("/", 1)[-1]
            task = next((t for t in TASKS if t["id"] == task_id), None)
            if task is None:
                self._send({"message": "task not found"}, 404)
            else:
                self._send(task)
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
        path = self.path.split("?")[0]
        obj_id = path.rsplit("/", 1)[-1]
        if "/tags/" in path:
            before = len(TAGS)
            TAGS[:] = [t for t in TAGS if t["id"] != obj_id]
            if len(TAGS) == before:
                self._send({"message": "tag not found"}, 404)
                return
            self.send_response(204)
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        if "/tasks/" in path:
            before = len(TASKS)
            TASKS[:] = [t for t in TASKS if t["id"] != obj_id]
            if len(TASKS) == before:
                self._send({"message": "task not found"}, 404)
                return
            self.send_response(204)
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        before = len(ENTRIES)
        ENTRIES[:] = [e for e in ENTRIES if e["id"] != obj_id]
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
            if body.get("taskId"):
                entry["taskId"] = body["taskId"]
            if body.get("tagIds"):
                entry["tagIds"] = body["tagIds"]
            ENTRIES.insert(0, entry)
            if not body.get("end"):  # timer started → mark as running
                RUNNING.clear()
                RUNNING.append({**entry, "userId": USER["id"]})
            self._send(self._entry_view(entry), 201)
        elif self.path.endswith("/tags"):
            name = (body.get("name") or "").strip()
            if not name:
                self._send({"message": "name is required"}, 400)
                return
            tag = {"id": f"t-mock-{len(TAGS) + 1}", "name": name, "workspaceId": "w1"}
            TAGS.append(tag)
            self._send(tag, 201)
        elif self.path.split("?")[0].endswith("/tasks"):
            path = self.path.split("?")[0]
            pid = path.split("/")[-2]
            name = (body.get("name") or "").strip()
            if not name:
                self._send({"message": "name is required"}, 400)
                return
            task = {"id": f"k-mock-{len(TASKS) + 1}", "name": name,
                    "projectId": pid, "status": body.get("status") or "ACTIVE"}
            if body.get("estimate"):
                task["estimate"] = body["estimate"]
            TASKS.append(task)
            self._send(task, 201)
        else:
            self._send({"message": f"no mock for POST {self.path}"}, 404)

    def do_PATCH(self):
        body = self._body()
        stopped = {**RUNNING[0], "timeInterval": {**RUNNING[0].get("timeInterval", {}),
                                                   "end": body.get("end")}} if RUNNING else {"id": "e-stopped"}
        RUNNING.clear()
        self._send(stopped)

    def do_PUT(self):
        path = self.path.split("?")[0]
        body = self._body()
        if "/tags/" in path:
            tag_id = path.rsplit("/", 1)[-1]
            tag = next((t for t in TAGS if t["id"] == tag_id), None)
            if tag is None:
                self._send({"message": "tag not found"}, 404)
                return
            if body.get("name"):
                tag["name"] = body["name"]
            if "archived" in body:
                tag["archived"] = body["archived"]
            self._send(tag)
            return
        if "/tasks/" in path:
            task_id = path.rsplit("/", 1)[-1]
            task = next((t for t in TASKS if t["id"] == task_id), None)
            if task is None:
                self._send({"message": "task not found"}, 404)
                return
            if body.get("name"):
                task["name"] = body["name"]
            if body.get("status"):
                task["status"] = body["status"]
            if body.get("estimate"):
                task["estimate"] = body["estimate"]
            self._send(task)
            return
        entry = {**ENTRIES[0], "description": body.get("description", ENTRIES[0]["description"])}
        ti = dict(ENTRIES[0]["timeInterval"])
        if body.get("start"):
            ti["start"] = body["start"]
        if "end" in body:
            ti["end"] = body["end"]
        entry["timeInterval"] = ti
        if "tagIds" in body:
            entry["tagIds"] = body["tagIds"]
        if body.get("taskId"):
            entry["taskId"] = body["taskId"]
        ENTRIES[0] = entry
        self._send(entry)


if __name__ == "__main__":
    host, port = "127.0.0.1", 8977
    print(f"Mock Clockify API listening on http://{host}:{port}")
    print("Point the CLI at it:  CLOCKIFY_BASE_URL=http://127.0.0.1:8977 ./clockify list")
    HTTPServer((host, port), Handler).serve_forever()
