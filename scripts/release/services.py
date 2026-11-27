"""GitHub and GHCR access for the SFS release workflow."""
import base64
import hashlib
import json
import os
import re
import subprocess
import tarfile
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode, urljoin, urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener

REPOSITORY = "FEATO-org/support-feato-system"
SOURCE = f"https://github.com/{REPOSITORY}"
PACKAGE = REPOSITORY.lower()
REGISTRY = "https://ghcr.io"
IMAGE = f"ghcr.io/{PACKAGE}"
MANIFEST = "application/vnd.oci.image.manifest.v1+json"
ACCEPT = ", ".join((
    MANIFEST,
    "application/vnd.oci.image.index.v1+json",
    "application/vnd.docker.distribution.manifest.v2+json",
    "application/vnd.docker.distribution.manifest.list.v2+json",
))
DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
SHA = re.compile(r"[0-9a-f]{40}")
LABEL = "org.opencontainers.image."


class ReleaseError(Exception):
    pass


def command(*args, payload=None):
    result = subprocess.run(args, input=payload, text=True, capture_output=True, check=False)
    if result.returncode:
        # API or registry responses may contain credentials or user-controlled text.
        status = re.search(r"HTTP [0-9]{3}", result.stderr)
        suffix = f", {status.group()}" if status else ""
        raise ReleaseError(f"{' '.join(args)} failed (exit {result.returncode}{suffix})")
    return result.stdout


def gh(endpoint, payload=None, paginate=False):
    args = ["gh", "api", f"repos/{REPOSITORY}/{endpoint}"]
    if paginate:
        args.extend(("--paginate", "--slurp"))
    if payload is not None:
        args.extend(("--method", "POST", "--input", "-"))
    return json.loads(command(*args, payload=None if payload is None else json.dumps(payload)))


def git_tags():
    refs = {}
    peeled = {}
    for line in command("git", "ls-remote", "--tags", "origin").splitlines():
        sha, ref = line.split("\t")
        tag = ref.removeprefix("refs/tags/")
        if tag.endswith("^{}"):
            peeled[tag[:-3]] = sha
        else:
            refs[tag] = sha
    return refs | peeled


def is_ancestor(older, newer):
    result = subprocess.run(
        ["git", "merge-base", "--is-ancestor", older, newer],
        capture_output=True, check=False,
    )
    if result.returncode not in (0, 1):
        raise ReleaseError("Could not verify commit ancestry. Check the main and tag history.")
    return result.returncode == 0


def digest(raw):
    return "sha256:" + hashlib.sha256(raw).hexdigest()


def require_digest(value):
    if not isinstance(value, str) or not DIGEST.fullmatch(value):
        raise ReleaseError("Invalid or missing SHA-256 image digest.")
    return value


def require_sha(value):
    if not isinstance(value, str) or not SHA.fullmatch(value):
        raise ReleaseError("Invalid or missing full Git commit SHA.")
    return value


def check_config(config):
    if (config.get("os"), config.get("architecture")) != ("linux", "amd64"):
        raise ReleaseError("Expected a linux/amd64 image.")
    return config["config"].get("Labels") or {}


def verify_labels(labels, version, sha):
    expected = {"source": SOURCE, "version": version, "revision": sha}
    if any(labels.get(LABEL + key) != value for key, value in expected.items()):
        raise ReleaseError(f"Unexpected OCI source, version or revision for {version}.")


class RegistryRedirects(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        # Blob downloads may redirect to signed storage URLs. Keep auth at GHCR.
        if req.get_method() != "GET" or urlsplit(newurl).scheme != "https":
            return None
        redirect = super().redirect_request(req, fp, code, msg, headers, newurl)
        if redirect is not None:
            redirect.remove_header("Authorization")
        return redirect


class Registry:
    def __init__(self, write=False):
        self.path = f"/v2/{PACKAGE}"
        self.opener = build_opener(RegistryRedirects())
        self.headers = {}
        auth = {}
        scope = "pull"
        if write:
            scope = "pull,push"
            credentials = f"{os.environ['GITHUB_ACTOR']}:{os.environ['GH_TOKEN']}"
            auth["Authorization"] = "Basic " + base64.b64encode(credentials.encode()).decode()
        query = urlencode({"service": "ghcr.io", "scope": f"repository:{PACKAGE}:{scope}"})
        token = json.loads(self.request(f"/token?{query}", headers=auth)[0])["token"]
        self.headers = {"Authorization": f"Bearer {token}"}

    def request(self, path, *, headers=None, data=None, absent=False):
        method = "GET" if data is None else "PUT"
        req = Request(REGISTRY + path, data=data, method=method, headers=headers or {})
        try:
            with self.opener.open(req, timeout=45) as response:
                return response.read(), response.headers
        except HTTPError as error:
            if absent and error.code == 404:
                codes = {item["code"] for item in json.load(error)["errors"]}
                if codes == {"MANIFEST_UNKNOWN"}:
                    return None
            raise ReleaseError(f"GHCR {method} {urlsplit(path).path}: HTTP {error.code}") from None
        except (URLError, TimeoutError):
            raise ReleaseError(f"GHCR {method} {urlsplit(path).path}: connection failed") from None

    def tags(self):
        path = self.path + "/tags/list"
        page = path + "?n=100"
        seen = set()
        tags = set()
        while page:
            if page in seen:
                raise ReleaseError("GHCR tag pagination repeated a page.")
            seen.add(page)
            raw, headers = self.request(page, headers=self.headers)
            listing = json.loads(raw)
            if listing["name"] != PACKAGE:
                raise ReleaseError("Unexpected repository in GHCR tag listing.")
            page_tags = listing["tags"]
            if page_tags is not None:
                if not isinstance(page_tags, list) or not all(isinstance(tag, str) for tag in page_tags):
                    raise ReleaseError("Invalid GHCR tag listing.")
                tags.update(page_tags)
            link = headers.get("Link")
            if not link:
                break
            match = re.fullmatch(r'<([^>]+)>;\s*rel="?next"?', link)
            if not match:
                raise ReleaseError("Unrecognized GHCR tag pagination response.")
            target = urlsplit(urljoin(REGISTRY + page, match[1]))
            if (target.scheme, target.netloc, target.path) != ("https", "ghcr.io", path):
                raise ReleaseError("Unexpected GHCR tag pagination URL.")
            page = f"{target.path}?{target.query}"
        return tags

    def manifest(self, ref):
        reply = self.request(
            self.path + f"/manifests/{ref}", headers=self.headers | {"Accept": ACCEPT},
            absent=not ref.startswith("sha256:"),
        )
        if reply is None:
            return None
        raw, headers = reply
        actual = digest(raw)
        if headers.get("Docker-Content-Digest") != actual or (
            ref.startswith("sha256:") and ref != actual
        ):
            raise ReleaseError(f"GHCR manifest digest mismatch for {ref}.")
        return actual, raw

    def labels(self, manifest, *, allow_index=False):
        document = json.loads(manifest[1])
        if allow_index and "manifests" in document:
            candidates = [
                child for child in document["manifests"]
                if child.get("platform", {}).get("os") == "linux"
                and child["platform"].get("architecture") == "amd64"
            ]
            if len(candidates) != 1:
                raise ReleaseError("Expected exactly one linux/amd64 alias image.")
            document = json.loads(self.manifest(require_digest(candidates[0]["digest"]))[1])
        elif document.get("mediaType") != MANIFEST:
            raise ReleaseError("Expected a single OCI image manifest.")
        config_digest = require_digest(document["config"]["digest"])
        raw, _ = self.request(self.path + f"/blobs/{config_digest}", headers=self.headers)
        if digest(raw) != config_digest:
            raise ReleaseError("GHCR image config digest mismatch.")
        return check_config(json.loads(raw))

    def put(self, tag, manifest):
        self.request(
            self.path + f"/manifests/{tag}", data=manifest[1],
            headers=self.headers | {"Content-Type": MANIFEST},
        )
        if self.manifest(tag) != manifest:
            raise ReleaseError(f"Could not verify GHCR tag {tag} after writing it.")


def archive_image(path, version, sha):
    def blob(archive, descriptor):
        expected = require_digest(descriptor["digest"])
        raw = read(archive, "blobs/sha256/" + expected.split(":")[1])
        if digest(raw) != expected:
            raise ReleaseError("OCI archive blob digest mismatch.")
        return raw

    def read(archive, filename):
        handle = archive.extractfile(filename)
        if handle is None:
            raise ReleaseError("Invalid OCI archive entry.")
        return handle.read()

    with tarfile.open(path) as archive:
        manifests = json.loads(read(archive, "index.json"))["manifests"]
        if len(manifests) != 1 or manifests[0]["mediaType"] != MANIFEST:
            raise ReleaseError("Expected exactly one OCI image manifest in the build archive.")
        raw = blob(archive, manifests[0])
        config = json.loads(blob(archive, json.loads(raw)["config"]))
        verify_labels(check_config(config), version, sha)
        return digest(raw), raw
