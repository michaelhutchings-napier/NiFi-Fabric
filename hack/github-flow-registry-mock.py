from http.server import BaseHTTPRequestHandler, HTTPServer
import json
from urllib.parse import parse_qs, urlparse


OWNER = "example-org"
REPOSITORY = "nifi-flows"
TOKEN = "dummytoken"
BRANCH = "main"
BRANCH_SHA = "1111111111111111111111111111111111111111"

USER = {
    "login": "nifi-fabric-bot",
    "id": 1,
    "type": "User",
    "site_admin": False,
}

REPOSITORY_PAYLOAD = {
    "id": 123,
    "node_id": "R_kgDOExample",
    "name": REPOSITORY,
    "full_name": f"{OWNER}/{REPOSITORY}",
    "private": False,
    "owner": USER,
    "default_branch": BRANCH,
    "permissions": {
        "admin": False,
        "maintain": False,
        "push": False,
        "triage": False,
        "pull": True,
    },
    "html_url": f"http://github-mock.nifi.svc.cluster.local/{OWNER}/{REPOSITORY}",
    "url": f"http://github-mock.nifi.svc.cluster.local/repos/{OWNER}/{REPOSITORY}",
    "contents_url": f"http://github-mock.nifi.svc.cluster.local/repos/{OWNER}/{REPOSITORY}/contents/{{+path}}",
    "branches_url": f"http://github-mock.nifi.svc.cluster.local/repos/{OWNER}/{REPOSITORY}/branches{{/branch}}",
}

COLLABORATOR_PERMISSION = {
    "permission": "read",
    "role_name": "read",
    "user": USER,
}

BRANCHES = [
    {
        "name": BRANCH,
        "protected": False,
        "commit": {
            "sha": BRANCH_SHA,
            "url": f"http://github-mock.nifi.svc.cluster.local/repos/{OWNER}/{REPOSITORY}/commits/{BRANCH_SHA}",
        },
    }
]

REF = {
    "ref": f"refs/heads/{BRANCH}",
    "node_id": "REF_example_main",
    "url": f"http://github-mock.nifi.svc.cluster.local/repos/{OWNER}/{REPOSITORY}/git/refs/heads/{BRANCH}",
    "object": {
        "type": "commit",
        "sha": BRANCH_SHA,
        "url": f"http://github-mock.nifi.svc.cluster.local/repos/{OWNER}/{REPOSITORY}/git/commits/{BRANCH_SHA}",
    },
}

TREE_ITEMS = [
    {
        "name": "team-a",
        "path": "flows/team-a",
        "sha": "team-a-sha",
        "type": "dir",
        "url": f"http://github-mock.nifi.svc.cluster.local/repos/{OWNER}/{REPOSITORY}/contents/flows/team-a",
        "html_url": f"http://github-mock.nifi.svc.cluster.local/{OWNER}/{REPOSITORY}/tree/{BRANCH}/flows/team-a",
        "git_url": f"http://github-mock.nifi.svc.cluster.local/repos/{OWNER}/{REPOSITORY}/git/trees/team-a-sha",
        "download_url": None,
        "_links": {
            "self": f"http://github-mock.nifi.svc.cluster.local/repos/{OWNER}/{REPOSITORY}/contents/flows/team-a",
            "git": f"http://github-mock.nifi.svc.cluster.local/repos/{OWNER}/{REPOSITORY}/git/trees/team-a-sha",
            "html": f"http://github-mock.nifi.svc.cluster.local/{OWNER}/{REPOSITORY}/tree/{BRANCH}/flows/team-a",
        },
    },
    {
        "name": "team-b",
        "path": "flows/team-b",
        "sha": "team-b-sha",
        "type": "dir",
        "url": f"http://github-mock.nifi.svc.cluster.local/repos/{OWNER}/{REPOSITORY}/contents/flows/team-b",
        "html_url": f"http://github-mock.nifi.svc.cluster.local/{OWNER}/{REPOSITORY}/tree/{BRANCH}/flows/team-b",
        "git_url": f"http://github-mock.nifi.svc.cluster.local/repos/{OWNER}/{REPOSITORY}/git/trees/team-b-sha",
        "download_url": None,
        "_links": {
            "self": f"http://github-mock.nifi.svc.cluster.local/repos/{OWNER}/{REPOSITORY}/contents/flows/team-b",
            "git": f"http://github-mock.nifi.svc.cluster.local/repos/{OWNER}/{REPOSITORY}/git/trees/team-b-sha",
            "html": f"http://github-mock.nifi.svc.cluster.local/{OWNER}/{REPOSITORY}/tree/{BRANCH}/flows/team-b",
        },
    },
]


class Handler(BaseHTTPRequestHandler):
    @staticmethod
    def _authorized(headers):
        authorization = headers.get("Authorization", "")
        return authorization in {f"token {TOKEN}", f"Bearer {TOKEN}"}

    def _send(self, code, payload, content_type="application/json"):
        body = payload if isinstance(payload, str) else json.dumps(payload)
        self.send_response(code)
        self.send_header("Content-Type", content_type)
        self.end_headers()
        self.wfile.write(body.encode())

    def _send_unauthorized(self):
        self._send(401, {"message": "Bad credentials"})

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

        if path == "/user":
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return
            self._send(200, USER)
            return

        if path == f"/repos/{OWNER}/{REPOSITORY}":
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return
            self._send(200, REPOSITORY_PAYLOAD)
            return

        if path == f"/repos/{OWNER}/{REPOSITORY}/collaborators/{USER['login']}/permission":
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return
            self._send(200, COLLABORATOR_PERMISSION)
            return

        if path == f"/repos/{OWNER}/{REPOSITORY}/branches":
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return
            self._send(200, BRANCHES)
            return

        if path == f"/repos/{OWNER}/{REPOSITORY}/git/refs/heads/{BRANCH}":
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return
            self._send(200, REF)
            return

        if path == f"/repos/{OWNER}/{REPOSITORY}/contents/flows":
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return
            ref = query.get("ref", [""])[0]
            if ref in {BRANCH, f"refs/heads/{BRANCH}"}:
                self._send(200, TREE_ITEMS)
                return

        self._send(404, {"path": self.path, "method": "GET"})


if __name__ == "__main__":
    HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
