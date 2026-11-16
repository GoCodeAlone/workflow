#!/usr/bin/env python3
"""Exercise real Git lineage with a local origin and modeled read-only GitHub API."""

import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
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
        (self.repo / "module/jwt_auth.go").write_text("baseline\nissuer and audience boundary\n")
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
        self.run_git("tag", "v0.74.9", self.release)
        self.env.update(TAG_NAME="v0.74.9", GITHUB_REF="refs/tags/v0.74.9")
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


if __name__ == "__main__":
    unittest.main(verbosity=2)
