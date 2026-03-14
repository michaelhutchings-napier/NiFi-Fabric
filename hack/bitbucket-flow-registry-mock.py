from http.server import BaseHTTPRequestHandler, HTTPServer
import json
from urllib.parse import parse_qs, urlparse


WORKSPACE = "example-workspace"
REPOSITORY = "nifi-flows"
TOKEN = "dummytoken"
BRANCH = "main"
BRANCH_SHA = "1111111111111111111111111111111111111111"

REPOSITORY_PAYLOAD = {
    "uuid": "{repo-uuid}",
    "name": REPOSITORY,
    "full_name": f"{WORKSPACE}/{REPOSITORY}",
    "is_private": False,
    "scm": "git",
    "mainbranch": {"name": BRANCH},
    "links": {
        "self": {"href": f"http://bitbucket-mock.nifi.svc.cluster.local:8080/2.0/repositories/{WORKSPACE}/{REPOSITORY}"},
        "html": {"href": f"http://bitbucket-mock.nifi.svc.cluster.local/{WORKSPACE}/{REPOSITORY}"},
    },
}

BRANCHES = {
    "pagelen": 10,
    "page": 1,
    "size": 1,
    "values": [
        {
            "name": BRANCH,
            "target": {
                "hash": BRANCH_SHA,
                "message": "seed flow repo",
                "type": "commit",
            },
        }
    ],
}

TREE_ITEMS = {
    "pagelen": 10,
    "page": 1,
    "size": 2,
    "values": [
        {
            "path": "flows/team-a",
            "type": "commit_directory",
        },
        {
            "path": "flows/team-b",
            "type": "commit_directory",
        },
    ],
}


class Handler(BaseHTTPRequestHandler):
    @staticmethod
    def _authorized(headers):
        authorization = headers.get("Authorization", "")
        return authorization == f"Bearer {TOKEN}"

    def _send(self, code, payload, content_type="application/json"):
        body = payload if isinstance(payload, str) else json.dumps(payload)
        self.send_response(code)
        self.send_header("Content-Type", content_type)
        self.end_headers()
        self.wfile.write(body.encode())

    def _send_unauthorized(self):
        self._send(401, {"type": "error", "error": {"message": "Unauthorized"}})

    def log_message(self, fmt, *args):
        print(
            "LOG",
            self.command,
            self.path,
            self.headers.get("Authorization"),
            flush=True,
        )

    def do_GET(self):
        parsed = urlparse(self.path)
        path = parsed.path
        query = parse_qs(parsed.query)

        if path == f"/2.0/repositories/{WORKSPACE}/{REPOSITORY}":
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return
            self._send(200, REPOSITORY_PAYLOAD)
            return

        if path == f"/2.0/repositories/{WORKSPACE}/{REPOSITORY}/refs/branches":
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return
            self._send(200, BRANCHES)
            return

        src_prefix = f"/2.0/repositories/{WORKSPACE}/{REPOSITORY}/src/"
        if path in {
            f"{src_prefix}{BRANCH}/flows",
            f"{src_prefix}{BRANCH}/flows/",
            f"{src_prefix}{BRANCH_SHA}/flows",
            f"{src_prefix}{BRANCH_SHA}/flows/",
        }:
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return
            self._send(200, TREE_ITEMS)
            return

        commit_collection = f"/2.0/repositories/{WORKSPACE}/{REPOSITORY}/commits"
        commit_prefix = f"{commit_collection}/"
        if path == commit_collection or path.startswith(commit_prefix):
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return
            branch = path.removeprefix(commit_prefix) if path.startswith(commit_prefix) else query.get("include", [BRANCH])[0]
            repo_path = query.get("path", [""])[0]
            if branch == BRANCH and repo_path in {"", "flows", "flows/", "flows/team-a", "flows/team-b"}:
                self._send(
                    200,
                    {
                        "pagelen": 10,
                        "page": 1,
                        "size": 1,
                        "values": [
                            {
                                "hash": BRANCH_SHA,
                                "message": "seed flow repo",
                                "author": {"raw": "NiFi Fabric <nifi@example.com>"},
                                "date": "2026-03-14T00:00:00+00:00",
                            }
                        ],
                    },
                )
                return

        self._send(404, {"path": self.path, "method": "GET"})


if __name__ == "__main__":
    HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
