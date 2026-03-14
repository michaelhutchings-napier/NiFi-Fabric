import base64
import hashlib
import json
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, HTTPServer
from itertools import count
from urllib.parse import parse_qs, urlparse


OWNER = "example-org"
REPOSITORY = "nifi-flows"
TOKEN = "dummytoken"
BRANCH = "main"
BRANCH_SHA = "1111111111111111111111111111111111111111"
DEFAULT_BUCKETS = ("team-a", "team-b")

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
    "permission": "write",
    "role_name": "write",
    "user": USER,
}

commit_counter = count(2)

STATE = {
    "branchSha": BRANCH_SHA,
    "files": {},
    "commitsByPath": {},
    "blobs": {},
}


def now_iso():
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def repo_url(path):
    return f"http://github-mock.nifi.svc.cluster.local/repos/{OWNER}/{REPOSITORY}/{path}"


def html_url(path):
    return f"http://github-mock.nifi.svc.cluster.local/{OWNER}/{REPOSITORY}/{path}"


def normalize_ref(ref):
    return ref.removeprefix("refs/heads/")


def known_ref(ref):
    if not ref:
        return True
    if normalize_ref(ref) == BRANCH:
        return True
    if ref == STATE["branchSha"]:
        return True
    return any(commit["sha"] == ref for commits in STATE["commitsByPath"].values() for commit in commits)


def next_commit_sha():
    return f"{next(commit_counter):040x}"


def blob_sha_for(content):
    return hashlib.sha1(content.encode()).hexdigest()


def directory_object(path):
    tree_sha = hashlib.sha1(path.encode()).hexdigest()
    return {
        "name": path.rsplit("/", 1)[-1],
        "path": path,
        "sha": tree_sha,
        "type": "dir",
        "url": repo_url(f"contents/{path}"),
        "html_url": html_url(f"tree/{BRANCH}/{path}"),
        "git_url": repo_url(f"git/trees/{tree_sha}"),
        "download_url": None,
        "_links": {
            "self": repo_url(f"contents/{path}"),
            "git": repo_url(f"git/trees/{tree_sha}"),
            "html": html_url(f"tree/{BRANCH}/{path}"),
        },
    }


def content_object(path, entry, ref=BRANCH):
    return {
        "name": path.rsplit("/", 1)[-1],
        "path": path,
        "sha": entry["sha"],
        "type": "file",
        "size": len(entry["content"].encode()),
        "url": repo_url(f"contents/{path}"),
        "html_url": html_url(f"blob/{ref}/{path}"),
        "git_url": repo_url(f"git/blobs/{entry['sha']}"),
        "download_url": repo_url(f"raw/{path}"),
        "encoding": "base64",
        "content": base64.b64encode(entry["content"].encode()).decode(),
        "_links": {
            "self": repo_url(f"contents/{path}"),
            "git": repo_url(f"git/blobs/{entry['sha']}"),
            "html": html_url(f"blob/{ref}/{path}"),
        },
    }


def list_directory(path):
    children = {}
    normalized = path.strip("/")
    prefix = f"{normalized}/" if normalized else ""

    if normalized == "flows":
        for bucket in DEFAULT_BUCKETS:
            children[f"{prefix}{bucket}"] = directory_object(f"{prefix}{bucket}")

    for file_path, entry in STATE["files"].items():
        if not file_path.startswith(prefix):
            continue
        remainder = file_path[len(prefix):]
        if not remainder:
            continue
        child_name = remainder.split("/", 1)[0]
        child_path = f"{prefix}{child_name}" if prefix else child_name
        if "/" in remainder:
            children.setdefault(child_path, directory_object(child_path))
        else:
            children[child_path] = content_object(file_path, entry)

    return [children[key] for key in sorted(children)]


def branch_payload():
    return [
        {
            "name": BRANCH,
            "protected": False,
            "commit": {
                "sha": STATE["branchSha"],
                "url": repo_url(f"commits/{STATE['branchSha']}"),
            },
        }
    ]


def ref_payload():
    return {
        "ref": f"refs/heads/{BRANCH}",
        "node_id": "REF_example_main",
        "url": repo_url(f"git/refs/heads/{BRANCH}"),
        "object": {
            "type": "commit",
            "sha": STATE["branchSha"],
            "url": repo_url(f"git/commits/{STATE['branchSha']}"),
        },
    }


def commit_payload(commit):
    return {
        "sha": commit["sha"],
        "node_id": commit["sha"],
        "commit": {
            "author": {
                "name": USER["login"],
                "email": "nifi-fabric-bot@example.org",
                "date": commit["date"],
            },
            "committer": {
                "name": USER["login"],
                "email": "nifi-fabric-bot@example.org",
                "date": commit["date"],
            },
            "message": commit["message"],
        },
        "author": USER,
        "committer": USER,
        "url": repo_url(f"commits/{commit['sha']}"),
    }


def decode_content(raw_content):
    try:
        return base64.b64decode(raw_content, validate=True).decode()
    except Exception:
        return raw_content


def write_file(path, message, raw_content, provided_sha):
    existing = STATE["files"].get(path)
    if provided_sha and existing and provided_sha != existing["sha"]:
        raise ValueError("provided sha does not match existing content")
    if provided_sha and not existing:
        raise FileNotFoundError(path)

    content = decode_content(raw_content)
    sha = blob_sha_for(content)
    commit_sha = next_commit_sha()
    commit = {
        "sha": commit_sha,
        "message": message,
        "date": now_iso(),
        "path": path,
    }
    STATE["files"][path] = {
        "content": content,
        "sha": sha,
        "commitSha": commit_sha,
    }
    STATE["blobs"][sha] = content
    STATE["commitsByPath"].setdefault(path, [])
    STATE["commitsByPath"][path].insert(0, commit)
    STATE["branchSha"] = commit_sha
    return STATE["files"][path], commit


def delete_file(path, message, provided_sha):
    existing = STATE["files"].get(path)
    if existing is None:
        raise FileNotFoundError(path)
    if provided_sha and provided_sha != existing["sha"]:
        raise ValueError("provided sha does not match existing content")

    commit_sha = next_commit_sha()
    commit = {
        "sha": commit_sha,
        "message": message,
        "date": now_iso(),
        "path": path,
        "deleted": True,
    }
    STATE["commitsByPath"].setdefault(path, [])
    STATE["commitsByPath"][path].insert(0, commit)
    STATE["branchSha"] = commit_sha
    del STATE["files"][path]
    return commit


def state_payload():
    return {
        "branch": BRANCH,
        "branchSha": STATE["branchSha"],
        "files": {
            path: entry
            for path, entry in sorted(STATE["files"].items())
        },
        "commitsByPath": {
            path: commits
            for path, commits in sorted(STATE["commitsByPath"].items())
        },
    }


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

    def _read_json(self):
        content_length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(content_length) if content_length else b"{}"
        return json.loads(body.decode() or "{}")

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

        if path == "/debug/state":
            self._send(200, state_payload())
            return

        if path == "/user":
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return
            self._send(200, USER)
            return

        if path == f"/users/{USER['login']}":
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
            self._send(200, branch_payload())
            return

        if path in {
            f"/repos/{OWNER}/{REPOSITORY}/git/refs/heads/{BRANCH}",
            f"/repos/{OWNER}/{REPOSITORY}/git/ref/heads/{BRANCH}",
        }:
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return
            self._send(200, ref_payload())
            return

        if path == f"/repos/{OWNER}/{REPOSITORY}/commits":
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return
            target_path = query.get("path", [""])[0]
            commits = STATE["commitsByPath"].get(target_path, [])
            self._send(200, [commit_payload(commit) for commit in commits])
            return

        if path.startswith(f"/repos/{OWNER}/{REPOSITORY}/commits/"):
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return
            target_sha = path.rsplit("/", 1)[-1]
            for commits in STATE["commitsByPath"].values():
                for commit in commits:
                    if commit["sha"] == target_sha:
                        self._send(200, commit_payload(commit))
                        return
            self._send(404, {"message": "Not Found", "sha": target_sha})
            return

        if path.startswith(f"/repos/{OWNER}/{REPOSITORY}/git/blobs/"):
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return
            sha = path.rsplit("/", 1)[-1]
            content = STATE["blobs"].get(sha)
            if content is None:
                self._send(404, {"message": "Not Found"})
                return
            self._send(
                200,
                {
                    "sha": sha,
                    "encoding": "base64",
                    "content": base64.b64encode(content.encode()).decode(),
                },
            )
            return

        if path.startswith(f"/repos/{OWNER}/{REPOSITORY}/contents/"):
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return

            content_path = path.removeprefix(f"/repos/{OWNER}/{REPOSITORY}/contents/")
            ref = query.get("ref", [""])[0]
            if not known_ref(ref):
                self._send(404, {"message": "Ref not found", "ref": ref})
                return

            if content_path in STATE["files"]:
                self._send(200, content_object(content_path, STATE["files"][content_path], normalize_ref(ref) or BRANCH))
                return

            entries = list_directory(content_path)
            if entries:
                self._send(200, entries)
                return

        self._send(404, {"path": self.path, "method": "GET"})

    def do_PUT(self):
        parsed = urlparse(self.path)
        path = parsed.path

        if path.startswith(f"/repos/{OWNER}/{REPOSITORY}/contents/"):
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return

            payload = self._read_json()
            branch = normalize_ref(payload.get("branch", BRANCH))
            if branch != BRANCH:
                self._send(404, {"message": "Branch not found", "branch": branch})
                return

            content_path = path.removeprefix(f"/repos/{OWNER}/{REPOSITORY}/contents/")
            try:
                entry, commit = write_file(
                    content_path,
                    payload.get("message", "Update content"),
                    payload.get("content", ""),
                    payload.get("sha"),
                )
            except FileNotFoundError:
                self._send(404, {"message": "Not Found", "path": content_path})
                return
            except ValueError as exc:
                self._send(409, {"message": str(exc), "path": content_path})
                return

            self._send(
                200,
                {
                    "content": content_object(content_path, entry),
                    "commit": {
                        "sha": commit["sha"],
                        "message": commit["message"],
                        "url": repo_url(f"commits/{commit['sha']}"),
                    },
                },
            )
            return

        self._send(404, {"path": self.path, "method": "PUT"})

    def do_DELETE(self):
        parsed = urlparse(self.path)
        path = parsed.path
        query = parse_qs(parsed.query)

        if path.startswith(f"/repos/{OWNER}/{REPOSITORY}/contents/"):
            if not self._authorized(self.headers):
                self._send_unauthorized()
                return

            content_path = path.removeprefix(f"/repos/{OWNER}/{REPOSITORY}/contents/")
            request_path = query.get("path", [""])[0]
            if request_path and request_path != content_path:
                self._send(400, {"message": "path query does not match request path"})
                return

            branch = normalize_ref(query.get("branch", [BRANCH])[0])
            if branch != BRANCH:
                self._send(404, {"message": "Branch not found", "branch": branch})
                return

            try:
                commit = delete_file(
                    content_path,
                    query.get("message", ["Delete content"])[0],
                    query.get("sha", [""])[0],
                )
            except FileNotFoundError:
                self._send(404, {"message": "Not Found", "path": content_path})
                return
            except ValueError as exc:
                self._send(409, {"message": str(exc), "path": content_path})
                return

            self._send(
                200,
                {
                    "content": None,
                    "commit": {
                        "sha": commit["sha"],
                        "message": commit["message"],
                        "url": repo_url(f"commits/{commit['sha']}"),
                    },
                },
            )
            return

        self._send(404, {"path": self.path, "method": "DELETE"})


if __name__ == "__main__":
    HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
