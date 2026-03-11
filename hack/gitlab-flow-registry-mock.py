from http.server import BaseHTTPRequestHandler, HTTPServer
import json
from urllib.parse import parse_qs, urlparse


PROJECT = {
    "id": 123,
    "description": "NiFi Flow Repo",
    "name": "nifi-flows",
    "name_with_namespace": "example-group / nifi-flows",
    "path": "nifi-flows",
    "path_with_namespace": "example-group/nifi-flows",
    "default_branch": "main",
    "web_url": "http://gitlab-mock.nifi.svc.cluster.local/example-group/nifi-flows",
    "http_url_to_repo": "http://gitlab-mock.nifi.svc.cluster.local/example-group/nifi-flows.git",
    "ssh_url_to_repo": "ssh://git@gitlab-mock/example-group/nifi-flows.git",
    "namespace": {
        "id": 77,
        "name": "example-group",
        "path": "example-group",
        "kind": "group",
        "full_path": "example-group",
    },
    "permissions": {
        "project_access": {"access_level": 40},
        "group_access": {"access_level": 40},
    },
}

PERSONAL_ACCESS_TOKEN = {
    "id": 1,
    "user_id": 1,
    "name": "nifi-fabric",
    "revoked": "false",
    "created_at": "2026-03-11T00:00:00.000Z",
    "expires_at": "2026-12-31",
    "last_used_at": "2026-03-11T00:00:00.000Z",
    "scopes": ["api"],
    "active": True,
}

BRANCHES = [
    {
        "name": "main",
        "default": True,
        "protected": False,
        "can_push": True,
        "web_url": "http://gitlab-mock.nifi.svc.cluster.local/example-group/nifi-flows/-/tree/main",
    }
]

TREE_ITEMS = [
    {
        "id": "bucket-a",
        "name": "team-a",
        "type": "tree",
        "path": "flows/team-a",
        "mode": "040000",
    },
    {
        "id": "bucket-b",
        "name": "team-b",
        "type": "tree",
        "path": "flows/team-b",
        "mode": "040000",
    },
]


class Handler(BaseHTTPRequestHandler):
    @staticmethod
    def _project_path_matches(path):
        return path in {
            "/api/v4/projects/example-group%2Fnifi-flows",
            "/api/v4/projects/example%2Dgroup%2Fnifi%2Dflows",
            "/api/v4/projects/example-group/nifi-flows",
        }

    @staticmethod
    def _project_repository_path_matches(path, suffix):
        prefixes = {
            "/api/v4/projects/123",
            "/api/v4/projects/example-group%2Fnifi-flows",
            "/api/v4/projects/example%2Dgroup%2Fnifi%2Dflows",
            "/api/v4/projects/example-group/nifi-flows",
        }
        return any(path == f"{prefix}{suffix}" for prefix in prefixes)

    def _send(self, code, payload, content_type="application/json"):
        body = payload if isinstance(payload, str) else json.dumps(payload)
        self.send_response(code)
        self.send_header("Content-Type", content_type)
        self.end_headers()
        self.wfile.write(body.encode())

    def log_message(self, fmt, *args):
        print(
            "LOG",
            self.command,
            self.path,
            self.headers.get("PRIVATE-TOKEN"),
            flush=True,
        )

    def do_GET(self):
        parsed = urlparse(self.path)
        path = parsed.path
        query = parse_qs(parsed.query)

        if path == "/api/v4/personal_access_tokens/self":
            self._send(200, PERSONAL_ACCESS_TOKEN)
            return

        if self._project_path_matches(path):
            self._send(200, PROJECT)
            return

        if self._project_repository_path_matches(path, "/repository/branches"):
            self._send(200, BRANCHES)
            return

        if self._project_repository_path_matches(path, "/repository/tree"):
            tree_path = query.get("path", [""])[0]
            ref = query.get("ref", [""])[0]
            if tree_path in {"flows", "flows/"} and ref == "main":
                self._send(200, TREE_ITEMS)
                return

        self._send(404, {"path": self.path, "method": "GET"})


if __name__ == "__main__":
    HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
