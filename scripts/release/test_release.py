from datetime import datetime, timezone
from pathlib import Path
from unittest import TestCase, mock

import release
from services import LABEL, SOURCE, ReleaseError

SHA = "a" * 40
NEW_SHA = "b" * 40
VERSION = "2026-11-26.1"
NEW_VERSION = "2026-11-26.2"
MANIFEST = ("sha256:" + "c" * 64, b"test manifest")
NEW_MANIFEST = ("sha256:" + "d" * 64, b"new manifest")
LEGACY = ("sha256:" + "e" * 64, b"legacy index")
NOTES = "## Changes\n- Example"


class FakeRegistry:
    def __init__(self):
        self.manifests = {}
        self.configs = {
            MANIFEST: self.config(VERSION, SHA),
            NEW_MANIFEST: self.config(NEW_VERSION, NEW_SHA),
            LEGACY: self.config("main", SHA),
        }
        self.writes = []
        self.fail_tag = None

    @staticmethod
    def config(version, sha):
        return {
            LABEL + "source": SOURCE,
            LABEL + "revision": sha,
            LABEL + "version": version,
        }

    def tags(self):
        return {ref for ref in self.manifests if not ref.startswith("sha256:")}

    def manifest(self, ref):
        return self.manifests.get(ref)

    def labels(self, manifest, *, allow_index=False):
        return self.configs[manifest]

    def put(self, tag, manifest):
        if tag == self.fail_tag:
            raise ReleaseError("Simulated registry write error.")
        self.writes.append(tag)
        self.manifests[tag] = manifest


class VersionsTest(TestCase):
    def test_jst_boundary_and_numeric_sequence(self):
        versions = {f"2026-11-26.{value}" for value in (1, 3, 9)}
        before = datetime(2026, 11, 26, 14, 59, tzinfo=timezone.utc)
        after = datetime(2026, 11, 26, 15, tzinfo=timezone.utc)
        self.assertEqual(release.next_version(set(), before), VERSION)
        self.assertEqual(release.next_version(versions, before), "2026-11-26.10")
        self.assertEqual(release.next_version(versions, after), "2026-11-27.1")

    def test_bad_calver_is_ignored(self):
        invalid = {"2026-11-26.00", "2026-11-26.09", "2026-11-26.0",
                   "v2026-11-26.19", "2026-02-30.1", "2026-11-26.2-rc"}
        self.assertTrue(all(release.version_key(version) is None for version in invalid))
        now = datetime(2026, 11, 26, 14, tzinfo=timezone.utc)
        self.assertEqual(release.next_version(invalid, now), VERSION)

    def test_numbering_reserves_names_from_all_three_systems(self):
        state = release.State({VERSION: SHA}, {NEW_VERSION: {"draft": True}}, {"2026-11-26.9"})
        now = datetime(2026, 11, 26, 14, tzinfo=timezone.utc)
        self.assertEqual(release.next_version(state.names, now), "2026-11-26.10")

    def test_non_main_and_regular_version_override_are_rejected(self):
        base = {
            "GITHUB_REPOSITORY": "FEATO-org/support-feato-system",
            "GITHUB_EVENT_NAME": "workflow_dispatch",
            "GITHUB_SHA": SHA,
        }
        with mock.patch.dict(release.os.environ, base | {"GITHUB_REF": "refs/heads/develop"}):
            with self.assertRaisesRegex(ReleaseError, "main"):
                release.validate_dispatch("release", "")
        with mock.patch.dict(release.os.environ, base | {"GITHUB_REF": "refs/heads/main"}):
            with self.assertRaises(ReleaseError):
                release.validate_dispatch("release", VERSION)
            with self.assertRaises(ReleaseError):
                release.validate_dispatch("resume", "")
            self.assertEqual(release.validate_dispatch("dry-run", ""), SHA)


class PublicationTest(TestCase):
    def setUp(self):
        self.registry = FakeRegistry()
        self.refs = {"0.1.0": "0" * 40}
        self.releases = {}
        self.events = []
        self.fail_release = False
        patches = [
            mock.patch.object(release, "git_tags", side_effect=lambda: self.refs.copy()),
            mock.patch.object(release, "is_ancestor", side_effect=lambda old, new: (old, new) != (NEW_SHA, SHA)),
            mock.patch.object(release, "gh", side_effect=self.api),
            mock.patch.object(release, "command", side_effect=self.transfer),
            mock.patch.object(release, "archive_image", return_value=MANIFEST),
            mock.patch.object(release, "report"),
        ]
        for patch in patches:
            patch.start()
            self.addCleanup(patch.stop)

    def api(self, endpoint, payload=None, paginate=False):
        if endpoint == "releases?per_page=100":
            return [list(self.releases.values())]
        if endpoint == "releases/generate-notes":
            self.events.append(("notes", payload))
            return {"body": NOTES}
        if endpoint == "git/refs":
            self.events.append(("git", payload))
            self.refs[payload["ref"].removeprefix("refs/tags/")] = payload["sha"]
            return {}
        if endpoint == "releases":
            if self.fail_release:
                raise ReleaseError("Simulated GitHub API outage.")
            self.events.append(("release", payload))
            record = payload | {"published_at": "2026-11-26T01:02:03Z"}
            self.releases[payload["tag_name"]] = record
            return record
        self.fail(f"Unexpected API endpoint {endpoint}")

    def transfer(self, *args):
        self.assertEqual(args[-1], f"docker://{release.IMAGE}@{MANIFEST[0]}")
        self.events.append(("transfer", args))
        self.registry.manifests[MANIFEST[0]] = MANIFEST

    def publish(self, mode="release", archive="oci.tar"):
        previous = "" if VERSION in self.releases else "0.1.0"
        release.publish(self.registry, mode, VERSION, SHA, previous, NEW_SHA, archive)

    def test_normal_publication_and_completed_retry(self):
        self.registry.manifests["main"] = LEGACY
        self.publish()
        self.assertEqual([event for event, _ in self.events], ["notes", "git", "transfer", "release"])
        self.assertEqual(self.registry.writes, [VERSION, f"sha-{SHA}", "latest", "main"])
        self.assertTrue(all(self.registry.manifest(tag) == MANIFEST for tag in self.registry.writes))
        self.assertIn("0.1.0", self.events[0][1]["previous_tag_name"])
        self.assertEqual(self.events[-1][1]["make_latest"], "true")
        self.events.clear()
        self.registry.writes.clear()
        self.publish("resume", archive=None)
        self.assertEqual(self.events, [])
        self.assertEqual(self.registry.writes, [])
        with self.assertRaises(ReleaseError):
            self.publish()

    def test_initial_release_with_no_git_or_release_history(self):
        self.refs.clear()
        release.publish(self.registry, "release", VERSION, SHA, "", SHA, "oci.tar")
        self.assertNotIn("previous_tag_name", self.events[0][1])
        self.assertEqual(self.registry.manifest("main"), MANIFEST)

    def test_formal_tag_appearing_after_plan_is_rejected(self):
        self.registry.manifests[VERSION] = MANIFEST
        with self.assertRaisesRegex(ReleaseError, "conflicts"):
            self.publish()
        self.assertEqual(self.events, [])
        self.assertEqual(self.registry.writes, [])

    def test_duplicate_commit_and_image_only_duplicate(self):
        for has_git in (True, False):
            with self.subTest(has_git=has_git):
                if has_git:
                    self.refs[VERSION] = SHA
                else:
                    del self.refs[VERSION]
                    self.registry.manifests[VERSION] = MANIFEST
                state = release.State.load(self.registry)
                with self.assertRaisesRegex(ReleaseError, "already"):
                    release.check_new(self.registry, state, SHA, NEW_VERSION)

    def test_failed_git_tag_only_can_be_rebuilt(self):
        self.refs[VERSION] = SHA
        self.publish("resume")
        self.assertEqual([event for event, _ in self.events], ["notes", "transfer", "release"])

    def test_image_only_recovers_original_commit_without_build(self):
        self.registry.manifests[VERSION] = MANIFEST
        self.publish("resume", archive=None)
        self.assertEqual(self.refs[VERSION], SHA)
        self.assertEqual([event for event, _ in self.events], ["notes", "git", "release"])

    def test_release_creation_failure_then_resume_without_build(self):
        self.fail_release = True
        with self.assertRaisesRegex(ReleaseError, "outage"):
            self.publish()
        self.assertEqual(self.registry.writes, [VERSION, f"sha-{SHA}"])
        self.fail_release = False
        self.events.clear()
        self.publish("resume", archive=None)
        self.assertEqual([event for event, _ in self.events], ["notes", "release"])

    def test_alias_failure_then_resume_updates_only_missing_alias(self):
        self.registry.fail_tag = "main"
        with self.assertRaisesRegex(ReleaseError, "write error"):
            self.publish()
        self.registry.fail_tag = None
        self.events.clear()
        self.registry.writes.clear()
        self.publish("resume", archive=None)
        self.assertEqual(self.events, [])
        self.assertEqual(self.registry.writes, ["main"])

    def test_older_recovery_never_moves_aliases(self):
        self.refs.update({VERSION: SHA, NEW_VERSION: NEW_SHA})
        self.registry.manifests.update({
            VERSION: MANIFEST,
            NEW_VERSION: NEW_MANIFEST,
            f"sha-{NEW_SHA}": NEW_MANIFEST,
            "main": NEW_MANIFEST,
            "latest": NEW_MANIFEST,
        })
        self.releases[NEW_VERSION] = {
            "tag_name": NEW_VERSION, "name": f"SFS {NEW_VERSION}",
            "draft": False, "prerelease": False, "published_at": "2026-11-26T01:02:03Z",
            "body": release.release_header(NEW_VERSION, NEW_SHA, NEW_MANIFEST[0], "2026-11-26T10:02:03+09:00")
                    + release.NOTES_HEADING + NOTES,
        }
        self.publish("resume", archive=None)
        self.assertEqual(self.registry.writes, [f"sha-{SHA}"])
        self.assertEqual(self.events[-1][1]["make_latest"], "false")

    def test_prior_comparison_uses_normal_release_over_orphan_git_tag(self):
        self.publish()
        self.refs[NEW_VERSION] = "f" * 40
        state = release.State.load(self.registry)
        self.assertEqual(
            release.previous_release(self.registry, state, "2026-11-27.1", NEW_SHA, NEW_SHA), VERSION,
        )

    def test_resume_stops_if_the_formal_digest_changed_after_planning(self):
        self.refs[VERSION] = SHA
        self.registry.manifests[VERSION] = MANIFEST
        with self.assertRaisesRegex(ReleaseError, "digest changed"):
            release.publish(self.registry, "resume", VERSION, SHA, "0.1.0", NEW_SHA, None,
                            planned_digest=NEW_MANIFEST[0])
        self.assertEqual(self.events, [])
        self.assertEqual(self.registry.writes, [])

    def test_git_and_image_revision_mismatch_stops_before_writes(self):
        self.refs[VERSION] = NEW_SHA
        self.registry.manifests[VERSION] = MANIFEST
        with self.assertRaisesRegex(ReleaseError, "revision"):
            self.publish("resume", archive=None)
        self.assertEqual(self.events, [])
        self.assertEqual(self.registry.writes, [])

    def test_sha_tag_conflict_and_existing_release_body_mismatch_stop(self):
        self.publish()
        self.events.clear()
        self.registry.writes.clear()
        self.registry.manifests[f"sha-{SHA}"] = NEW_MANIFEST
        with self.assertRaisesRegex(ReleaseError, "SHA tag differs"):
            self.publish("resume", archive=None)
        self.registry.manifests[f"sha-{SHA}"] = MANIFEST
        self.releases[VERSION]["body"] = self.releases[VERSION]["body"].replace(MANIFEST[0], NEW_MANIFEST[0])
        with self.assertRaisesRegex(ReleaseError, "metadata"):
            self.publish("resume", archive=None)
        self.assertEqual(self.events, [])
        self.assertEqual(self.registry.writes, [])

    def test_listing_error_is_not_absence(self):
        with mock.patch.object(release, "gh", side_effect=ReleaseError("API failure")):
            with self.assertRaisesRegex(ReleaseError, "API failure"):
                self.publish()
        with mock.patch.object(self.registry, "tags", side_effect=ReleaseError("Registry failure")):
            with self.assertRaisesRegex(ReleaseError, "Registry failure"):
                self.publish()
        self.assertEqual(self.events, [])
        self.assertEqual(self.registry.writes, [])

    def test_dry_run_plans_and_generates_notes_without_publication(self):
        with mock.patch.object(release, "datetime") as clock:
            clock.now.return_value = datetime(2026, 11, 26, 12, tzinfo=timezone.utc)
            plan = release.plan(self.registry, "dry-run", "", SHA)
        env = {
            "GITHUB_REPOSITORY": "FEATO-org/support-feato-system", "GITHUB_REF": "refs/heads/main",
            "GITHUB_EVENT_NAME": "workflow_dispatch", "GITHUB_SHA": SHA, "RELEASE_MODE": "dry-run",
            "RESUME_VERSION": "", "VERSION": plan["version"], "TARGET_SHA": plan["sha"],
            "PREVIOUS": plan["previous"],
        }
        with mock.patch.dict(release.os.environ, env), mock.patch.object(release, "Registry", return_value=self.registry):
            release.main("notes")
            with self.assertRaisesRegex(ReleaseError, "Unexpected release action"):
                release.main("publish")
        self.assertEqual([event for event, _ in self.events], ["notes"])
        self.assertEqual(self.registry.writes, [])

    def test_legacy_workflow_must_be_disabled_and_drained(self):
        active = {"state": "active"}
        disabled = {"state": "disabled_manually"}
        nonempty = [{"workflow_runs": [{"status": "queued"}]}]
        with mock.patch.object(release, "gh", return_value=active):
            with self.assertRaisesRegex(ReleaseError, "Disable"):
                release.check_legacy_workflow()
        with mock.patch.object(release, "gh", side_effect=[disabled, nonempty]):
            with self.assertRaisesRegex(ReleaseError, "still pending"):
                release.check_legacy_workflow()
        with mock.patch.object(release, "gh", side_effect=[disabled, [{"workflow_runs": []}]]):
            release.check_legacy_workflow()


class WorkflowTest(TestCase):
    def test_release_and_resume_share_workflow_lock(self):
        root = Path(__file__).resolve().parents[2]
        workflow = (root / ".github/workflows/release.yml").read_text()
        self.assertIn("\nconcurrency:\n  group: sfs-container-release\n  cancel-in-progress: false", workflow)
        self.assertIn("(inputs.mode == 'release' || inputs.mode == 'resume')", workflow)
        self.assertIn("name: Generate GitHub Release Notes preview\n        if: inputs.mode == 'dry-run'", workflow)
        self.assertIn("needs: [plan, verify]", workflow)
        self.assertNotIn("\n  push:", workflow)
        self.assertIn(
            "          artifact-ids: ${{ needs.verify.outputs.artifact }}\n          merge-multiple: true",
            workflow,
        )
        self.assertFalse((root / ".github/workflows/upload.yml").exists())
