"""Plan, verify and publish a pinned SFS CalVer release."""
import os
import re
import sys
import tarfile
from dataclasses import dataclass
from datetime import date, datetime
from zoneinfo import ZoneInfo

from services import (
    IMAGE, LABEL, SOURCE, Registry, ReleaseError, archive_image, command, gh,
    git_tags, is_ancestor, require_digest, require_sha, verify_labels,
)

TOKYO = ZoneInfo("Asia/Tokyo")
CALVER = re.compile(r"([0-9]{4}-[0-9]{2}-[0-9]{2})[.]([1-9][0-9]*)")
LEGACY_BASELINE = "0.1.0"
LEGACY_WORKFLOW = "56448257"
NOTES_HEADING = "\n\n<!-- github-generated-release-notes -->\n\n"


def version_key(version):
    match = CALVER.fullmatch(version)
    if not match or len(version) > 128:
        return None
    try:
        return date.fromisoformat(match[1]), int(match[2])
    except ValueError:
        return None


def next_version(names, now):
    today = now.astimezone(TOKYO).date()
    numbers = [
        key[1] for name in names
        if (key := version_key(name)) is not None and key[0] == today
    ]
    version = f"{today}.{max(numbers, default=0) + 1}"
    if not version_key(version):
        raise ReleaseError("CalVer has exhausted the registry tag length.")
    return version


def report(*lines):
    print("\n".join(lines), flush=True)
    if path := os.environ.get("GITHUB_STEP_SUMMARY"):
        with open(path, "a", encoding="utf-8") as handle:
            handle.write("\n" + "\n".join(lines) + "\n")


def release_header(version, sha, image_digest, timestamp):
    return "\n".join((
        f"## SFS {version}",
        "",
        f"- Released (JST): `{timestamp}`",
        f"- Commit: `{sha}`",
        f"- Image: `{IMAGE}`",
        f"- Version tag: `{version}`",
        f"- Digest: `{image_digest}`",
        "",
        "```sh",
        f"docker pull {IMAGE}:{version}",
        "```",
        "",
        "```yaml",
        f"image: {IMAGE}:{version}@{image_digest}",
        "```",
    ))


def verify_release(release, version, sha, image_digest):
    body = release.get("body") or ""
    head, separator, notes = body.partition(NOTES_HEADING)
    time_match = re.search(r"(?m)^- Released [(]JST[)]: `([^\x60\n]+)`$", head)
    if (
        release.get("tag_name") != version or release.get("name") != f"SFS {version}"
        or release.get("draft") is not False or release.get("prerelease") is not False
        or not release.get("published_at") or not separator or not notes.strip() or not time_match
        or head != release_header(version, sha, image_digest, time_match[1])
    ):
        raise ReleaseError(f"GitHub Release {version} differs from the expected release metadata.")
    timestamp = datetime.fromisoformat(time_match[1])
    if timestamp.utcoffset() != timestamp.astimezone(TOKYO).utcoffset():
        raise ReleaseError(f"GitHub Release {version} has an invalid JST timestamp.")


@dataclass
class State:
    refs: dict
    releases: dict
    image_tags: set

    @classmethod
    def load(cls, registry):
        refs = git_tags()
        releases = {}
        for page in gh("releases?per_page=100", paginate=True):
            for entry in page:
                version = entry["tag_name"]
                if version in releases:
                    raise ReleaseError("Multiple GitHub Releases reference one version.")
                releases[version] = entry
        return cls(refs, releases, registry.tags())

    @property
    def names(self):
        return set(self.refs) | set(self.releases) | self.image_tags

    def published(self):
        return sorted((
            version for version, entry in self.releases.items()
            if version_key(version) and not (entry["draft"] or entry["prerelease"])
        ), key=version_key)


def validate_dispatch(mode, version):
    if (
        os.environ.get("GITHUB_REPOSITORY") != SOURCE.removeprefix("https://github.com/")
        or os.environ.get("GITHUB_REF") != "refs/heads/main"
        or os.environ.get("GITHUB_EVENT_NAME") != "workflow_dispatch"
    ):
        raise ReleaseError("Dispatch Release SFS from refs/heads/main in the SFS repository.")
    if mode == "resume":
        if not version_key(version):
            raise ReleaseError("Resume requires an existing version in YYYY-MM-DD.N format.")
    elif mode not in ("release", "dry-run") or version:
        raise ReleaseError("Choose release or dry-run with an empty version field.")
    return require_sha(os.environ["GITHUB_SHA"])


def formal_image(registry, state, version):
    manifest = registry.manifest(version)
    if manifest is None and version in state.image_tags:
        raise ReleaseError(f"GHCR listed {version} but its manifest is missing.")
    return manifest


def inspect(registry, state, version, dispatch_sha):
    manifest = formal_image(registry, state, version)
    release = state.releases.get(version)
    tag_sha = state.refs.get(version)
    sha = require_sha(tag_sha or (
        registry.labels(manifest).get(LABEL + "revision") if manifest else None
    ))
    if not is_ancestor(sha, dispatch_sha):
        raise ReleaseError(f"{version} does not target a commit in the dispatched main history.")
    if manifest:
        verify_labels(registry.labels(manifest), version, sha)
    sha_tag = registry.manifest(f"sha-{sha}")
    if sha_tag and sha_tag != manifest:
        raise ReleaseError(f"Existing SHA tag differs from the formal image for {version}.")
    if release:
        if not (tag_sha and manifest):
            raise ReleaseError(f"GitHub Release {version} requires a matching Git tag and formal image.")
        verify_release(release, version, sha, manifest[0])
    return sha, manifest, release


def previous_release(registry, state, version, sha, dispatch_sha):
    previous = next((
        candidate for candidate in reversed(state.published())
        if version_key(candidate) < version_key(version)
    ), None)
    if previous:
        previous_sha, manifest, _ = inspect(registry, state, previous, dispatch_sha)
        if registry.manifest(f"sha-{previous_sha}") != manifest:
            raise ReleaseError(f"Previous release {previous} is missing its SHA tag.")
        if not is_ancestor(previous_sha, sha):
            raise ReleaseError(f"Previous release {previous} is outside the target commit history.")
        return previous
    baseline_sha = state.refs.get(LEGACY_BASELINE)
    if baseline_sha and is_ancestor(require_sha(baseline_sha), sha):
        return LEGACY_BASELINE
    if (set(state.refs) | set(state.releases)) - {version}:
        # With no explicit baseline, GitHub could select a failed or unrelated tag.
        raise ReleaseError("No safe first-release comparison. Check the legacy 0.1.0 tag and history.")
    return ""


def check_new(registry, state, sha, version):
    if (
        version in state.names
        or registry.manifest(version)
        or registry.manifest(f"sha-{sha}")
    ):
        raise ReleaseError(f"Existing version or SHA tag conflicts with {version}. Reload the state.")
    for candidate, commit in state.refs.items():
        if commit == sha and version_key(candidate):
            raise ReleaseError(f"Commit already has CalVer tag {candidate}. Check it with resume.")
    for candidate in state.image_tags - set(state.refs):
        if version_key(candidate):
            manifest = formal_image(registry, state, candidate)
            if manifest and registry.labels(manifest).get(LABEL + "revision") == sha:
                raise ReleaseError(f"Commit already has formal image {candidate}. Check it with resume.")
    if state.published() and version_key(state.published()[-1]) >= version_key(version):
        raise ReleaseError("The planned version is older than an existing published release.")


def generate_notes(version, sha, previous):
    payload = {
        "tag_name": version, "target_commitish": sha,
        "configuration_file_path": ".github/release.yml",
    }
    if previous:
        payload["previous_tag_name"] = previous
    notes = gh("releases/generate-notes", payload).get("body")
    if not isinstance(notes, str) or not notes.strip():
        raise ReleaseError("GitHub returned empty generated release notes.")
    return notes


def plan(registry, mode, requested, dispatch_sha):
    state = State.load(registry)
    manifest, release = None, None
    if mode == "resume":
        if requested not in state.names:
            raise ReleaseError("No existing release state matches the requested resume version.")
        version = requested
        report(f"- Resume version: `{version}`")
        sha, manifest, release = inspect(registry, state, version, dispatch_sha)
    else:
        sha = dispatch_sha
        version = next_version(state.names, datetime.now(TOKYO))
        check_new(registry, state, sha, version)
    previous = "" if release else previous_release(registry, state, version, sha, dispatch_sha)
    report(
        f"- Target commit: `{sha}`", f"- CalVer: `{version}`",
        f"- Comparison: `{previous or ('existing release' if release else 'initial history')}`",
        f"- Planned tags: `{version}`, `sha-{sha}`, `latest`, `main`",
        f"- Existing formal image digest: `{manifest[0] if manifest else 'none'}`",
    )
    return {
        "version": version, "sha": sha, "previous": previous,
        "build": "false" if manifest else "true",
    }


def check_legacy_workflow():
    endpoint = f"actions/workflows/{LEGACY_WORKFLOW}"
    if gh(endpoint)["state"] not in ("disabled_manually", "deleted"):
        raise ReleaseError("Disable the legacy upload.yml workflow and drain its runs. See docs/release.md.")
    for page in gh(endpoint + "/runs?per_page=100", paginate=True):
        if any(run["status"] != "completed" for run in page["workflow_runs"]):
            raise ReleaseError("A legacy upload.yml run is still pending or running.")


def update_aliases(registry, version, manifest, dispatch_sha):
    for alias in ("latest", "main"):
        state = State.load(registry)
        published = state.published()
        if not published:
            raise ReleaseError("The current GitHub Release disappeared during promotion.")
        newest = published[-1]
        if newest != version:
            if version_key(newest) < version_key(version):
                raise ReleaseError("The current GitHub Release disappeared during promotion.")
            newest_sha, newest_manifest, _ = inspect(registry, state, newest, dispatch_sha)
            if registry.manifest(f"sha-{newest_sha}") != newest_manifest:
                raise ReleaseError(f"Newer release {newest} has an inconsistent SHA tag.")
            report(f"- Moving aliases preserved for newer release `{newest}`.")
            return
        sha, current, _ = inspect(registry, state, version, dispatch_sha)
        if current != manifest or registry.manifest(f"sha-{sha}") != manifest:
            raise ReleaseError(f"Formal image or SHA tag for {version} changed before promotion.")
        existing = registry.manifest(alias)
        if existing == manifest:
            report(f"- Verified alias: `{alias}`")
            continue
        if existing:
            labels = registry.labels(existing, allow_index=True)
            old_version = labels.get(LABEL + "version", "")
            if labels.get(LABEL + "source") != SOURCE or (
                old_version != "main" and (
                    not version_key(old_version)
                    or version_key(old_version) >= version_key(version)
                    or registry.manifest(old_version) != existing
                )
            ):
                raise ReleaseError(f"Alias {alias} has unknown, inconsistent or newer state.")
        registry.put(alias, manifest)
        report(f"- Updated alias: `{alias}`")


def ensure_tag(registry, tag, manifest, *, must_be_new=False):
    existing = registry.manifest(tag)
    if existing is None:
        registry.put(tag, manifest)
        report(f"- Published container tag: `{tag}`")
    elif must_be_new or existing != manifest:
        raise ReleaseError(f"GHCR tag {tag} would be overwritten.")
    else:
        report(f"- Verified container tag: `{tag}`")


def publish(registry, mode, version, sha, previous, dispatch_sha, archive):
    state = State.load(registry)
    manifest, release = None, None
    if mode == "resume":
        actual_sha, manifest, release = inspect(registry, state, version, dispatch_sha)
        if actual_sha != sha:
            raise ReleaseError("The release target changed since planning.")
    else:
        check_new(registry, state, sha, version)
    report(f"- Target commit: `{sha}`", f"- CalVer: `{version}`")
    if not manifest:
        if not archive:
            raise ReleaseError("The formal manifest disappeared and no validated build is available.")
        manifest = archive_image(archive, version, sha)
    image_digest = manifest[0]
    report(f"- Image digest: `{image_digest}`", f"- Deployment: `{IMAGE}:{version}@{image_digest}`")
    notes = None
    if not release:
        actual_previous = previous_release(registry, state, version, sha, dispatch_sha)
        if previous != actual_previous:
            raise ReleaseError("The comparison changed since planning. Start a fresh dispatch.")
        notes = generate_notes(version, sha, previous)
    refs = git_tags()
    if refs.get(version) is None:
        gh("git/refs", {"ref": f"refs/tags/{version}", "sha": sha})
        report(f"- Created Git tag: `{version}`")
    elif mode == "release" or refs[version] != sha:
        raise ReleaseError(f"Git tag {version} conflicts with this run.")
    else:
        report(f"- Verified Git tag: `{version}`")
    existing = registry.manifest(version)
    if mode == "release" and existing:
        raise ReleaseError(f"GHCR version {version} appeared before publication.")
    if existing is None:
        if not archive:
            raise ReleaseError("Formal image disappeared before publication.")
        # Upload to the content-addressed reference. Only the verified manifest is tagged.
        command("skopeo", "copy", "--preserve-digests", f"oci-archive:{archive}",
                f"docker://{IMAGE}@{image_digest}")
        if registry.manifest(image_digest) != manifest:
            raise ReleaseError("Registry did not preserve the built manifest digest.")
    ensure_tag(registry, version, manifest, must_be_new=mode == "release")
    verify_labels(registry.labels(manifest), version, sha)
    ensure_tag(registry, f"sha-{sha}", manifest, must_be_new=mode == "release")
    state = State.load(registry)
    if not release:
        if state.refs.get(version) != sha or version in state.releases:
            raise ReleaseError("Git tag or GitHub Release changed before publication. Inspect and resume.")
        if registry.manifest(version) != manifest or registry.manifest(f"sha-{sha}") != manifest:
            raise ReleaseError("Formal image or SHA tag changed before GitHub Release creation.")
        if previous_release(registry, state, version, sha, dispatch_sha) != previous:
            raise ReleaseError("The comparison changed during publication. Inspect and resume.")
        latest = not state.published() or version_key(state.published()[-1]) < version_key(version)
        timestamp = datetime.now(TOKYO).isoformat(timespec="seconds")
        release = gh("releases", {
            "tag_name": version, "target_commitish": sha, "name": f"SFS {version}",
            "body": release_header(version, sha, image_digest, timestamp) + NOTES_HEADING + notes,
            "draft": False, "prerelease": False, "make_latest": "true" if latest else "false",
        })
    verify_release(release, version, sha, image_digest)
    report(f"- GitHub Release: [{version}]({SOURCE}/releases/tag/{version})")
    update_aliases(registry, version, manifest, dispatch_sha)


def output(values):
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as handle:
        for name, value in values.items():
            handle.write(f"{name}={value}\n")


def main(action):
    mode = os.environ["RELEASE_MODE"]
    dispatch_sha = validate_dispatch(mode, os.environ.get("RESUME_VERSION", ""))
    if action == "plan":
        output(plan(Registry(), mode, os.environ.get("RESUME_VERSION", ""), dispatch_sha))
        return

    version = os.environ["VERSION"]
    sha = require_sha(os.environ["TARGET_SHA"])
    previous = os.environ["PREVIOUS"]
    if not version_key(version) or (previous and previous != LEGACY_BASELINE and not version_key(previous)):
        raise ReleaseError("Invalid version or comparison passed from the planning job.")
    if mode == "resume" and version != os.environ["RESUME_VERSION"]:
        raise ReleaseError("Planned version differs from the requested resume version.")
    if mode in ("release", "dry-run") and sha != dispatch_sha:
        raise ReleaseError("Planned commit differs from the dispatch SHA.")
    if action == "archive":
        image_digest, _ = archive_image(os.environ["IMAGE_ARCHIVE"], version, sha)
        report(f"- Built OCI digest: `{image_digest}`")
    elif action == "notes" and mode == "dry-run":
        registry = Registry()
        state = State.load(registry)
        check_new(registry, state, sha, version)
        if previous_release(registry, state, version, sha, dispatch_sha) != previous:
            raise ReleaseError("The comparison changed since planning. Start a fresh dispatch.")
        generate_notes(version, sha, previous)
        report("- GitHub Release Notes preview generated successfully. No release was created.")
    elif action == "publish" and mode in ("release", "resume"):
        check_legacy_workflow()
        publish(Registry(write=True), mode, version, sha, previous, dispatch_sha,
                os.environ.get("IMAGE_ARCHIVE"))
    else:
        raise ReleaseError("Unexpected release action or mode.")


if __name__ == "__main__":
    try:
        main(sys.argv[1])
    except ReleaseError as error:
        report(f"- Error: {error}")
        sys.exit(1)
    except (OSError, ValueError, TypeError, KeyError, IndexError, tarfile.TarError) as error:
        report(f"- Could not verify release state ({type(error).__name__}). Check the API, Git refs and GHCR.")
        sys.exit(1)
