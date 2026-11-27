#!/usr/bin/env python3
"""Exercise real Git lineage with a local origin and modeled read-only GitHub API."""

import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest


class ReleaseAdmissionTests(unittest.TestCase):
    def setUp(self):
        spec = importlib.util.spec_from_file_location(
            "maintenance_guard_fixture", Path(__file__).with_name("verify-maintenance-release.py"))
        self.guard = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = self.guard
        spec.loader.exec_module(self.guard)
        self.temp = tempfile.TemporaryDirectory(prefix="maintenance-release-guard-")
        self.addCleanup(self.temp.cleanup)
        self.repo = Path(self.temp.name) / "work"
        self.origin = Path(self.temp.name) / "origin.git"
        subprocess.run(["git", "init", "--bare", "-q", str(self.origin)], check=True)
        subprocess.run(["git", "init", "-q", "-b", "main", str(self.repo)], check=True)
        self.run_git("config", "user.name", "Guard fixture")
        self.run_git("config", "user.email", "guard@example.invalid")
        self.run_git("remote", "add", "origin", str(self.origin))
        original_command = self.guard.command

        def fixture_command(args, **kwargs):
            self.assertEqual(args[0], "git")
            return original_command(["git", "-C", str(self.repo), *args[1:]], **kwargs)

        self.guard.command = fixture_command
        for name in self.guard.AUTH_PATHS:
            file = self.repo / name
            file.parent.mkdir(parents=True, exist_ok=True)
            file.write_text("baseline\n")
        self.commit("base")
        self.base = self.rev("HEAD")
        self.run_git("tag", "v0.74.7")
        self.run_git("checkout", "-q", "-b", "source")
        (self.repo / "module/jwt_auth.go").write_text('baseline\nconst issuer = "issuer and audience boundary"\n')
        self.commit("reviewed backport")
        self.source = self.rev("HEAD")
        self.tree = self.rev("HEAD^{tree}")
        self.patch = self.guard.patch_id(self.base, self.source)
        self.policy = self.guard.Policy(base_commit=self.base, auth_patch_id=self.patch)
        self.run_git("checkout", "-q", "-b", "maintenance/v0.74", self.base)
        self.run_git("merge", "--squash", "source")
        self.commit("maintenance squash")
        self.release = self.rev("HEAD")
        self.assertEqual(self.rev("HEAD^{tree}"), self.tree)
        self.run_git("tag", "v0.74.8")
        self.run_git("checkout", "-q", "main")
        self.run_git("merge", "--squash", "source")
        self.commit("upstream JWT fix")
        self.upstream = self.rev("HEAD")
        self.record = {
            "schema": 1, "tag": "v0.74.8", "release_commit": self.release,
            "source_commit": self.source, "release_tree": self.tree,
            "base_commit": self.base, "source_pr": 1041, "source_ci_run": 42,
            "upstream_pr": 1042, "upstream_commit": self.upstream,
        }
        self.write_record(self.record)
        self.run_git("push", "-q", "origin", "main", "maintenance/v0.74",
                     "source:refs/pull/1041/head", "refs/tags/v0.74.7", "refs/tags/v0.74.8")
        repository = {"full_name": self.guard.REPOSITORY}
        self.responses = {
            "pulls/1041": {"number": 1041, "merged": True, "merge_commit_sha": self.release,
                           "base": {"ref": "maintenance/v0.74", "repo": repository},
                           "head": {"sha": self.source, "repo": repository}},
            "pulls/1042": {"number": 1042, "merged": True, "merge_commit_sha": self.upstream,
                           "base": {"ref": "main", "repo": repository},
                           "head": {"sha": self.source, "repo": repository}},
            "actions/runs/42": {"id": 42, "repository": repository,
                                "path": ".github/workflows/maintenance-jwt.yml",
                                "head_sha": self.source, "event": "pull_request",
                                "status": "completed", "conclusion": "success",
                                "pull_requests": [{"number": 1041}]},
        }
        self.env = {"TAG_NAME": "v0.74.8", "GITHUB_SHA": self.release,
                    "GITHUB_REPOSITORY": self.guard.REPOSITORY,
                    "GITHUB_EVENT_NAME": "push", "GITHUB_REF": "refs/tags/v0.74.8"}
        self.run_git("checkout", "-q", "--detach", self.release)

    def run_git(self, *args):
        result = subprocess.run(["git", "-C", str(self.repo), *args], capture_output=True, check=True)
        return result.stdout.decode().strip()

    def rev(self, ref):
        return self.run_git("rev-parse", ref)

    def commit(self, message):
        self.run_git("add", "-A")
        self.run_git("commit", "-q", "-m", message)

    def write_record(self, record, raw=None):
        self.run_git("checkout", "-q", "main")
        path = self.repo / ".github/release-admissions/v0.74.8.json"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(raw if raw is not None else json.dumps(record))
        self.commit("reviewed main admission")

    def update_record(self, record, raw=None):
        self.write_record(record, raw)
        self.run_git("push", "-q", "origin", "main")
        self.run_git("checkout", "-q", "--detach", self.release)

    def api(self, path):
        return copy.deepcopy(self.responses[path])

    def verify(self):
        return self.guard.verify_release(self.env, self.api, self.policy)

    def test_reviewed_squash_release_with_exact_source_ci(self):
        self.assertNotEqual(self.release, self.source)
        self.assertEqual(self.verify(), "reviewed-v0.74.8")

    def test_main_ancestor_keeps_existing_admission_without_api(self):
        self.run_git("checkout", "-q", "--detach", self.upstream)
        self.run_git("tag", "-f", "v0.74.8")
        self.env["GITHUB_SHA"] = self.upstream
        self.assertEqual(self.guard.verify_release(
            self.env, lambda _: self.fail("main ancestry must not need API"), self.policy), "protected-main")

    def test_record_in_tagged_source_cannot_self_authorize(self):
        self.run_git("checkout", "-q", "main")
        self.run_git("rm", "-q", ".github/release-admissions/v0.74.8.json")
        self.commit("remove main admission")
        self.run_git("push", "-q", "origin", "main")
        self.run_git("checkout", "-q", "--detach", self.release)
        path = self.repo / ".github/release-admissions/v0.74.8.json"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(self.record))
        self.commit("self-authorizing tag record")
        self.env["GITHUB_SHA"] = self.rev("HEAD")
        self.run_git("tag", "-f", "v0.74.8")
        with self.assertRaisesRegex(self.guard.Denied, "no regular admission"):
            self.verify()

    def test_missing_main_record_denies(self):
        self.run_git("checkout", "-q", "main")
        self.run_git("rm", "-q", ".github/release-admissions/v0.74.8.json")
        self.commit("remove admission")
        self.run_git("push", "-q", "origin", "main")
        self.run_git("checkout", "-q", "--detach", self.release)
        with self.assertRaises(self.guard.Denied):
            self.verify()

    def test_record_changes_cannot_admit_other_commit_base_or_pr(self):
        for field, value in [("tag", "v0.74.9"), ("release_commit", self.source),
                             ("base_commit", self.upstream), ("source_pr", 1043),
                             ("release_tree", self.base), ("source_commit", self.upstream)]:
            with self.subTest(field=field):
                record = dict(self.record, **{field: value})
                self.update_record(record)
                with self.assertRaises(self.guard.Denied):
                    self.verify()

    def test_other_tag_has_no_exception(self):
        self.run_git("tag", "v0.74.11", self.release)
        self.env.update(TAG_NAME="v0.74.11", GITHUB_REF="refs/tags/v0.74.11")
        with self.assertRaisesRegex(self.guard.Denied, "No exception"):
            self.verify()

    def test_bad_event_tag_head_and_repository_deny(self):
        original = dict(self.env)
        for field, value in [("GITHUB_EVENT_NAME", "workflow_dispatch"),
                             ("GITHUB_REPOSITORY", "attacker/workflow"),
                             ("GITHUB_REF", "refs/heads/main"),
                             ("TAG_NAME", "v0.74.08"), ("GITHUB_SHA", self.source)]:
            with self.subTest(field=field):
                self.env = dict(original, **{field: value})
                with self.assertRaises(self.guard.Denied):
                    self.verify()

    def test_source_and_upstream_must_be_merged_into_exact_branches(self):
        original = copy.deepcopy(self.responses)
        for key in ["pulls/1041", "pulls/1042"]:
            for change in ["unmerged", "wrong-branch", "wrong-repository", "wrong-merge"]:
                with self.subTest(key=key, change=change):
                    self.responses = copy.deepcopy(original)
                    pr = self.responses[key]
                    if change == "unmerged":
                        pr["merged"] = False
                    elif change == "wrong-branch":
                        pr["base"]["ref"] = "unreviewed"
                    elif change == "wrong-repository":
                        pr["base"]["repo"]["full_name"] = "attacker/workflow"
                    else:
                        pr["merge_commit_sha"] = self.base
                    with self.assertRaises(self.guard.Denied):
                        self.verify()

    def test_ci_must_be_successful_exact_source_and_workflow(self):
        original = copy.deepcopy(self.responses["actions/runs/42"])
        for field, value in [("conclusion", "failure"), ("status", "in_progress"),
                             ("head_sha", self.release), ("event", "push"),
                             ("path", ".github/workflows/unrelated.yml"),
                             ("pull_requests", [{"number": 1042}]), ("id", 43),
                             ("repository", {"full_name": "attacker/workflow"})]:
            with self.subTest(field=field):
                self.responses["actions/runs/42"] = dict(original, **{field: value})
                with self.assertRaises(self.guard.Denied):
                    self.verify()

    def test_duplicate_unknown_boolean_and_symlink_records_deny(self):
        for record, raw in [(dict(self.record, source_pr=True), None),
                            (dict(self.record, extra="unexpected"), None),
                            (self.record, '{"schema":1,"schema":1}')]:
            self.update_record(record, raw)
            with self.assertRaises(self.guard.Denied):
                self.verify()
        self.run_git("checkout", "-q", "main")
        path = self.repo / ".github/release-admissions/v0.74.8.json"
        path.unlink()
        path.symlink_to("../../record.json")
        self.commit("symlink admission")
        self.run_git("push", "-q", "origin", "main")
        self.run_git("checkout", "-q", "--detach", self.release)
        with self.assertRaisesRegex(self.guard.Denied, "no regular admission"):
            self.verify()

    def test_git_or_api_failure_cannot_fall_back_to_acceptance(self):
        def unavailable(_):
            raise self.guard.Denied("GitHub provenance read unavailable")
        with self.assertRaises(self.guard.Denied):
            self.guard.verify_release(self.env, unavailable, self.policy)
        self.run_git("remote", "set-url", "origin", str(self.repo / "missing-origin"))
        with self.assertRaises(self.guard.Denied):
            self.verify()

    def test_wrong_auth_patch_fails_even_with_main_admission(self):
        wrong = self.guard.Policy(base_commit=self.base, auth_patch_id="0" * 40)
        with self.assertRaisesRegex(self.guard.Denied, "JWT patch mismatch"):
            self.guard.verify_release(self.env, self.api, wrong)

    def test_release_content_cannot_change_after_reviewed_source(self):
        (self.repo / "unreviewed.txt").write_text("changed after source CI\n")
        self.commit("unreviewed release change")
        changed = self.rev("HEAD")
        self.run_git("update-ref", "refs/heads/maintenance/v0.74", changed)
        self.run_git("push", "-q", "origin", "maintenance/v0.74")
        self.run_git("tag", "-f", "v0.74.8", changed)
        self.release = changed
        self.env["GITHUB_SHA"] = changed
        self.responses["pulls/1041"]["merge_commit_sha"] = changed
        self.update_record(dict(self.record, release_commit=changed))
        with self.assertRaisesRegex(self.guard.Denied, "tree differs"):
            self.verify()

    def test_source_ref_cannot_move_after_main_admission(self):
        self.run_git("push", "-q", "--force", "origin", self.release + ":refs/pull/1041/head")
        with self.assertRaisesRegex(self.guard.Denied, "source moved"):
            self.verify()

    def test_upstream_must_actually_be_reachable_from_main(self):
        self.responses["pulls/1042"]["merge_commit_sha"] = self.release
        self.update_record(dict(self.record, upstream_commit=self.release))
        with self.assertRaisesRegex(self.guard.Denied, "not on protected main"):
            self.verify()

    def test_release_must_actually_be_on_maintained_branch(self):
        self.run_git("push", "-q", "--force", "origin", self.base + ":refs/heads/maintenance/v0.74")
        with self.assertRaisesRegex(self.guard.Denied, "lineage mismatch"):
            self.verify()

    def test_local_replace_refs_cannot_fabricate_main_ancestry(self):
        main = self.rev("refs/heads/main")
        fake = self.run_git("commit-tree", self.rev(main + "^{tree}"), "-p", self.release,
                            "-m", "fabricated ancestry fixture")
        self.run_git("replace", main, fake)
        self.run_git("merge-base", "--is-ancestor", self.release, main)
        self.assertEqual(self.verify(), "reviewed-v0.74.8")

    def checksum_fixture(self):
        # Keep the legacy JWT fixture and its separate v0.74.8 record intact.
        self.jwt_release = self.release
        self.run_git("checkout", "-q", "-b", "checksum-source", self.jwt_release)
        paths = ("cmd/wfctl/plugin_checksum.go", "cmd/wfctl/plugin_install.go",
                 ".github/workflows/maintenance-jwt.yml", ".github/workflows/maintenance-wfctl.yml")
        for name in paths:
            path = self.repo / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("fixed synthetic content for " + name + "\n")
        self.commit("checksum source with preserved JWT")
        self.source = self.rev("HEAD")
        self.tree = self.rev("HEAD^{tree}")
        blobs = tuple((name, self.rev(self.source + ":" + name)) for name in paths)
        self.run_git("checkout", "-q", "maintenance/v0.74")
        self.run_git("merge", "--squash", "checksum-source")
        self.commit("checksum maintenance squash")
        self.release = self.rev("HEAD")
        self.run_git("tag", "v0.74.9")
        self.run_git("checkout", "-q", "main")
        (self.repo / "checksum-upstream.txt").write_text("modeled upstream checksum change\n")
        self.commit("upstream checksum merge")
        checksum_upstream = self.rev("HEAD")
        self.policy = SimpleNamespace(base_commit=self.base, auth_patch_id=self.patch,
                                      jwt_maintenance_commit=self.jwt_release,
                                      checksum_upstream_commit=checksum_upstream,
                                      checksum_blobs=blobs)
        self.record = dict(self.record, schema=2, tag="v0.74.9", release_commit=self.release,
                           source_commit=self.source, release_tree=self.tree, source_pr=1108,
                           source_ci_run=43, checksum_ci_run=44)
        self.checksum_record(self.record)
        self.run_git("push", "-q", "origin", "main", "maintenance/v0.74",
                     "checksum-source:refs/pull/1108/head", "refs/tags/v0.74.9")
        repository = {"full_name": self.guard.REPOSITORY}
        self.responses["pulls/1108"] = {
            "number": 1108, "merged": True, "merge_commit_sha": self.release,
            "base": {"ref": "maintenance/v0.74", "repo": repository},
            "head": {"sha": self.source, "repo": repository}}
        self.responses["pulls/960"] = {
            "number": 960, "merged": True, "merge_commit_sha": checksum_upstream,
            "base": {"ref": "main", "repo": repository}, "head": {"repo": repository}}
        for number, workflow in ((43, "maintenance-jwt.yml"), (44, "maintenance-wfctl.yml")):
            self.responses[f"actions/runs/{number}"] = {
                "id": number, "repository": repository, "path": ".github/workflows/" + workflow,
                "head_sha": self.source, "event": "pull_request", "status": "completed",
                "conclusion": "success", "pull_requests": [{"number": 1108}]}
        self.env.update(TAG_NAME="v0.74.9", GITHUB_REF="refs/tags/v0.74.9", GITHUB_SHA=self.release)
        self.run_git("checkout", "-q", "--detach", self.release)

    def checksum_record(self, record, raw=None):
        self.run_git("checkout", "-q", "main")
        path = self.repo / ".github/release-admissions/v0.74.9.json"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(raw if raw is not None else json.dumps(record))
        self.commit("proposed checksum admission")

    def update_checksum_record(self, record, raw=None):
        self.checksum_record(record, raw)
        self.run_git("push", "-q", "origin", "main")
        self.run_git("checkout", "-q", "--detach", self.release)

    def test_checksum_release_requires_both_exact_head_checks_and_preserves_jwt(self):
        self.checksum_fixture()
        self.assertNotEqual(self.source, self.release)
        try:
            result = self.verify()
        except self.guard.Denied as error:
            self.fail("Exact checksum/JWT tuple rejected: " + str(error))
        self.assertEqual(result, "reviewed-v0.74.9")

    def test_checksum_ci_rejects_stale_wrong_workflow_failed_or_foreign_runs(self):
        self.checksum_fixture()
        original = copy.deepcopy(self.responses)
        for run_id in (43, 44):
            for field, value in (("head_sha", self.jwt_release), ("path", ".github/workflows/other.yml"),
                                 ("status", "in_progress"), ("conclusion", "failure"),
                                 ("event", "push"), ("id", run_id + 1),
                                 ("pull_requests", [{"number": 1041}]),
                                 ("repository", {"full_name": "attacker/workflow"})):
                with self.subTest(run=run_id, field=field):
                    self.responses = copy.deepcopy(original)
                    self.responses[f"actions/runs/{run_id}"][field] = value
                    with self.assertRaisesRegex(self.guard.Denied, "Exact maintenance source CI"):
                        self.verify()

    def test_checksum_source_and_upstreams_must_be_merged_same_repository(self):
        self.checksum_fixture()
        original = copy.deepcopy(self.responses)
        for key in ("pulls/1108", "pulls/1042", "pulls/960"):
            for field in ("merged", "base", "head", "merge_commit_sha"):
                with self.subTest(pr=key, field=field):
                    self.responses = copy.deepcopy(original)
                    value = {"merged": False, "base": {"ref": "wrong"},
                             "head": {"repo": {"full_name": "attacker/workflow"}},
                             "merge_commit_sha": self.base}[field]
                    self.responses[key][field] = value
                    with self.assertRaises(self.guard.Denied):
                        self.verify()

    def test_checksum_record_rejects_wrong_tuple_schema_and_incomplete_proposal(self):
        self.checksum_fixture()
        for field, value in (("tag", "v0.74.8"), ("release_commit", self.source),
                             ("source_commit", self.jwt_release), ("release_tree", self.base),
                             ("base_commit", self.upstream), ("source_pr", 1041),
                             ("checksum_ci_run", True), ("source_ci_run", 0),
                             ("schema", 1), ("release_commit", None), ("extra", "unexpected")):
            with self.subTest(field=field, value=value):
                self.update_checksum_record(dict(self.record, **{field: value}))
                with self.assertRaises(self.guard.Denied):
                    self.verify()

    def test_checksum_record_rejects_duplicate_oversized_and_symlink(self):
        self.checksum_fixture()
        for raw in ('{"schema":2,"schema":2}', ' ' * 4097):
            with self.subTest(kind="duplicate" if len(raw) < 4096 else "oversized"):
                self.update_checksum_record(self.record, raw)
                with self.assertRaises(self.guard.Denied):
                    self.verify()
        self.run_git("checkout", "-q", "main")
        path = self.repo / ".github/release-admissions/v0.74.9.json"
        path.unlink()
        path.symlink_to("../../record.json")
        self.commit("symlink checksum admission")
        self.run_git("push", "-q", "origin", "main")
        self.run_git("checkout", "-q", "--detach", self.release)
        with self.assertRaisesRegex(self.guard.Denied, "no regular admission"):
            self.verify()

    def test_checksum_source_ref_cannot_move(self):
        self.checksum_fixture()
        self.run_git("push", "-q", "--force", "origin", self.release + ":refs/pull/1108/head")
        with self.assertRaisesRegex(self.guard.Denied, "source moved"):
            self.verify()

    def test_checksum_requires_fixed_production_and_workflow_blobs(self):
        self.checksum_fixture()
        original = self.policy.checksum_blobs
        for path, blob in original:
            with self.subTest(path=path):
                self.policy.checksum_blobs = tuple((p, "0" * 40 if p == path else b) for p, b in original)
                with self.assertRaisesRegex(self.guard.Denied, "Checksum production or workflow identity"):
                    self.verify()

    def promote_checksum_change(self, message):
        # Model a fully re-reviewed tuple so only immutable policy checks reject it.
        self.commit(message)
        changed = self.rev("HEAD")
        changed_tree = self.rev("HEAD^{tree}")
        self.run_git("update-ref", "refs/heads/maintenance/v0.74", changed)
        self.run_git("push", "-q", "origin", "maintenance/v0.74", changed + ":refs/pull/1108/head", "--force")
        self.source = self.release = changed
        self.run_git("tag", "-f", "v0.74.9", changed)
        self.env["GITHUB_SHA"] = changed
        self.responses["pulls/1108"].update(merge_commit_sha=changed, head={
            "sha": changed, "repo": {"full_name": self.guard.REPOSITORY}})
        for run_id in (43, 44):
            self.responses[f"actions/runs/{run_id}"]["head_sha"] = changed
        self.record.update(release_commit=changed, source_commit=changed, release_tree=changed_tree)
        self.update_checksum_record(self.record)

    def test_checksum_release_requires_preserved_jwt_patch(self):
        self.checksum_fixture()
        (self.repo / "module/jwt_auth.go").write_text("baseline\n")
        self.promote_checksum_change("remove JWT from release")
        with self.assertRaisesRegex(self.guard.Denied, "Backported JWT patch mismatch"):
            self.verify()

    def test_checksum_rejects_changed_jwt_literal_even_when_stable_patch_id_matches(self):
        self.checksum_fixture()
        path = self.repo / "module/jwt_auth.go"
        path.write_text(path.read_text().replace('"issuer and audience', '"issuer  and audience'))
        self.promote_checksum_change("change JWT issuer literal whitespace")
        self.assertEqual(self.guard.patch_id(self.base, self.source), self.patch)
        with self.assertRaisesRegex(self.guard.Denied, "Backported JWT source identity mismatch"):
            self.verify()

    def test_checksum_rejects_missing_and_nonregular_production_or_workflow_entries(self):
        self.checksum_fixture()
        original_source = self.source
        for name, _ in self.policy.checksum_blobs:
            for change in ("removed", "symlink", "executable"):
                with self.subTest(path=name, change=change):
                    self.run_git("checkout", "-q", "--detach", self.release)
                    # Restore contents while advancing history: the production
                    # fetch correctly refuses a rewritten previously fetched ref.
                    self.run_git("checkout", "-q", original_source, "--",
                                 *(p for p, _ in self.policy.checksum_blobs))
                    path = self.repo / name
                    if change == "executable":
                        path.chmod(0o755)
                    else:
                        path.unlink()
                        if change == "symlink":
                            path.symlink_to("unexpected-target")
                    self.promote_checksum_change("change exact checksum entry " + change)
                    with self.assertRaisesRegex(self.guard.Denied, "Checksum production or workflow identity"):
                        self.verify()

    def test_checksum_upstream_must_be_on_main(self):
        self.checksum_fixture()
        self.run_git("checkout", "-q", "main")
        # A record can name a real checksum commit that has been removed from main.
        self.run_git("reset", "--hard", self.upstream)
        self.checksum_record(self.record)
        self.run_git("push", "-q", "--force", "origin", "main")
        self.run_git("checkout", "-q", "--detach", self.release)
        with self.assertRaisesRegex(self.guard.Denied, "Checksum fix is not on protected main"):
            self.verify()


    def followup_checksum_fixture(self):
        # The original checksum source/merge, tag model and v0.74.9 admission
        # remain separate. A follow-up gets its own source, merge, CI and record.
        self.checksum_fixture()
        self.checksum_anchor = self.release
        self.checksum_old_source = self.source
        self.policy.checksum_maintenance_commit = self.checksum_anchor
        self.followup_pr = 1200
        self.run_git("checkout", "-q", "-b", "checksum-followup-source", self.checksum_anchor)
        (self.repo / "reviewed-guard-followup.txt").write_text("modeled reviewed verifier update\n")
        self.commit("reviewed follow-up source")
        self.source = self.rev("HEAD")
        self.tree = self.rev("HEAD^{tree}")
        self.run_git("checkout", "-q", "maintenance/v0.74")
        self.run_git("merge", "--squash", "checksum-followup-source")
        self.commit("follow-up maintenance squash")
        self.release = self.rev("HEAD")
        self.run_git("tag", "v0.74.10")
        self.record = dict(self.record, tag="v0.74.10", source_pr=self.followup_pr,
                           source_commit=self.source, release_commit=self.release,
                           release_tree=self.tree, source_ci_run=45, checksum_ci_run=46)
        self.followup_checksum_record(self.record)
        self.run_git("push", "-q", "origin", "main", "maintenance/v0.74",
                     "checksum-followup-source:refs/pull/1200/head", "refs/tags/v0.74.10")
        repository = {"full_name": self.guard.REPOSITORY}
        self.responses["pulls/1200"] = {
            "number": 1200, "merged": True, "merge_commit_sha": self.release,
            "base": {"ref": "maintenance/v0.74", "repo": repository},
            "head": {"sha": self.source, "ref": "checksum-followup-source", "repo": repository}}
        for number, workflow in ((45, "maintenance-jwt.yml"), (46, "maintenance-wfctl.yml")):
            self.responses[f"actions/runs/{number}"] = {
                "id": number, "repository": repository, "path": ".github/workflows/" + workflow,
                "head_sha": self.source, "event": "pull_request", "status": "completed",
                "conclusion": "success", "pull_requests": [{"number": self.followup_pr}]}
        self.env.update(TAG_NAME="v0.74.10", GITHUB_REF="refs/tags/v0.74.10", GITHUB_SHA=self.release)
        self.run_git("checkout", "-q", "--detach", self.release)

    def followup_checksum_record(self, record):
        self.run_git("checkout", "-q", "main")
        path = self.repo / ".github/release-admissions/v0.74.10.json"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(record))
        self.commit("proposed follow-up admission")

    def update_followup_checksum_record(self, record):
        self.followup_checksum_record(record)
        self.run_git("push", "-q", "origin", "main")
        self.run_git("checkout", "-q", "--detach", self.release)

    def test_followup_checksum_release_requires_fresh_reviewed_tuple(self):
        self.followup_checksum_fixture()
        self.assertNotEqual(self.source, self.release)
        self.assertNotEqual(self.release, self.checksum_anchor)
        self.assertEqual(self.rev(self.source + "^{tree}"), self.rev(self.release + "^{tree}"))
        self.assertTrue(self.guard.ancestor(self.checksum_anchor, self.source))
        self.assertTrue(self.guard.ancestor(self.checksum_anchor, self.release))
        try:
            result = self.verify()
        except self.guard.Denied as error:
            self.fail("Exact follow-up checksum tuple rejected: " + str(error))
        self.assertEqual(result, "reviewed-v0.74.10")

    def test_followup_missing_wrong_or_old_pr_ci_is_denied(self):
        self.followup_checksum_fixture()
        original = copy.deepcopy(self.responses)
        for run_id in (45, 46):
            for field, value in (("pull_requests", []),
                                 ("pull_requests", [{"number": 1108}]),
                                 ("head_sha", self.checksum_old_source),
                                 ("head_sha", self.checksum_anchor),
                                 ("event", "push"), ("event", "workflow_dispatch"),
                                 ("conclusion", "failure"), ("status", "in_progress"),
                                 ("path", ".github/workflows/other.yml"), ("id", run_id + 100),
                                 ("repository", {"full_name": "foreign/workflow"})):
                with self.subTest(run=run_id, field=field, value=value):
                    self.responses = copy.deepcopy(original)
                    self.responses[f"actions/runs/{run_id}"][field] = value
                    with self.assertRaisesRegex(self.guard.Denied, "Exact maintenance source CI"):
                        self.verify()

    def test_followup_absent_null_or_malformed_association_is_denied(self):
        self.followup_checksum_fixture()
        original = copy.deepcopy(self.responses)
        cases = (("absent", None), ("null", None), ("object", {}),
                 ("string", "not-an-array"), ("boolean", True), ("number", 1200),
                 ("null entry", [None]), ("string entry", ["1200"]),
                 ("missing number", [{}]), ("boolean number", [{"number": True}]),
                 ("string number", [{"number": "1200"}]),
                 ("floating number", [{"number": 1200.0}]))
        for run_id in (45, 46):
            for name, value in cases:
                with self.subTest(run=run_id, association=name):
                    self.responses = copy.deepcopy(original)
                    run = self.responses[f"actions/runs/{run_id}"]
                    if name == "absent":
                        run.pop("pull_requests")
                    else:
                        run["pull_requests"] = value
                    with self.assertRaisesRegex(self.guard.Denied, "Exact maintenance source CI"):
                        self.verify()

    def test_followup_requires_new_source_pr_and_exact_merged_head(self):
        self.followup_checksum_fixture()
        original = copy.deepcopy(self.responses)
        for field, value in (("merged", False), ("number", 1201),
                             ("merge_commit_sha", self.checksum_anchor),
                             ("base", {"ref": "other", "repo": {"full_name": self.guard.REPOSITORY}}),
                             ("head", {"sha": self.source, "repo": {"full_name": "foreign/workflow"}})):
            with self.subTest(field=field):
                self.responses = copy.deepcopy(original)
                self.responses["pulls/1200"][field] = value
                with self.assertRaises(self.guard.Denied):
                    self.verify()
        self.responses = original
        self.update_followup_checksum_record(dict(self.record, source_pr=1108))
        with self.assertRaises(self.guard.Denied):
            self.verify()

    def test_followup_requires_strict_checksum_anchor_descendants(self):
        self.followup_checksum_fixture()
        for anchor in (self.source, self.release, self.upstream):
            with self.subTest(anchor=anchor):
                self.policy.checksum_maintenance_commit = anchor
                with self.assertRaisesRegex(self.guard.Denied, "Follow-up checksum lineage"):
                    self.verify()

    def test_followup_record_refuses_old_runs_bad_schema_tree_and_tag(self):
        self.followup_checksum_fixture()
        for field, value in (("source_ci_run", 43), ("checksum_ci_run", 44),
                             ("schema", 1), ("tag", "v0.74.9"),
                             ("release_commit", self.checksum_anchor),
                             ("source_commit", self.checksum_old_source),
                             ("release_tree", self.base), ("checksum_ci_run", True)):
            with self.subTest(field=field):
                self.update_followup_checksum_record(dict(self.record, **{field: value}))
                with self.assertRaises(self.guard.Denied):
                    self.verify()

    def test_followup_source_ref_cannot_move_after_admission(self):
        self.followup_checksum_fixture()
        self.run_git("push", "-q", "--force", "origin", self.release + ":refs/pull/1200/head")
        with self.assertRaisesRegex(self.guard.Denied, "source moved"):
            self.verify()

    def test_followup_without_main_record_is_denied(self):
        self.run_git("tag", "v0.74.10", self.release)
        self.env.update(TAG_NAME="v0.74.10", GITHUB_REF="refs/tags/v0.74.10")
        with self.assertRaisesRegex(self.guard.Denied, "no regular admission"):
            self.verify()

    def test_original_checksum_empty_ci_associations_remain_denied(self):
        self.checksum_fixture()
        for run_id in (43, 44):
            with self.subTest(run=run_id):
                self.responses[f"actions/runs/{run_id}"]["pull_requests"] = []
                with self.assertRaisesRegex(self.guard.Denied, "Exact maintenance source CI"):
                    self.verify()
                self.responses[f"actions/runs/{run_id}"]["pull_requests"] = [{"number": 1108}]


if __name__ == "__main__":
    unittest.main(verbosity=2)
