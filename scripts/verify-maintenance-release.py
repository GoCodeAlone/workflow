#!/usr/bin/env python3
"""Keep main ancestry, with one main-approved, exact v0.74.8 admission."""

import json
import os
import re
import subprocess
from dataclasses import dataclass


REPOSITORY = "GoCodeAlone/workflow"
AUTH_PATHS = (
    "DOCUMENTATION.md",
    "module/jwt_auth.go",
    "module/jwt_auth_boundary_test.go",
    "plugins/auth/jwt_boundary_test.go",
    "plugins/auth/plugin.go",
    "schema/module_schema.go",
    "schema/module_schema_test.go",
    "schema/testdata/editor-schemas.golden.json",
)


@dataclass(frozen=True)
class Policy:
    base_commit: str = "f03ce511ea6fbc5daab4f9b828bc3c2cb762fa43"
    auth_patch_id: str = "50ca17cb77067ddc36b5583bb54fc62a4f7cc5db"


class Denied(Exception):
    pass


def command(args, data=None, allowed=(0,)):
    try:
        result = subprocess.run(args, input=data, capture_output=True, timeout=45,
                                env=dict(os.environ, GIT_NO_REPLACE_OBJECTS="1"))
    except (OSError, subprocess.TimeoutExpired) as error:
        raise Denied("Provenance command unavailable") from error
    if result.returncode not in allowed:
        raise Denied("Provenance command failed")
    return result.returncode, result.stdout


def git(*args, data=None, allowed=(0,)):
    return command(["git", *args], data=data, allowed=allowed)


def github_api(path):
    _, raw = command(["gh", "api", f"repos/{REPOSITORY}/{path}"])
    try:
        return json.loads(raw)
    except (ValueError, UnicodeError) as error:
        raise Denied("GitHub provenance response invalid") from error


def require(condition, message):
    if not condition:
        raise Denied(message)


def oid(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{40}", value) is not None


def git_text(*args):
    return git(*args)[1].decode("ascii").strip()


def ancestor(commit, reference):
    status, _ = git("merge-base", "--is-ancestor", commit, reference, allowed=(0, 1))
    return status == 0


def patch_id(base, commit):
    _, patch = git("diff", "--no-ext-diff", "--no-textconv", base, commit, "--", *AUTH_PATHS)
    value = git("patch-id", "--stable", data=patch)[1].decode("ascii").split()
    return value[0] if len(value) == 2 else ""


def admission_record():
    path = ".github/release-admissions/v0.74.8.json"
    entry = git("ls-tree", "-z", "refs/remotes/origin/main", "--", path)[1]
    require(re.fullmatch(rb"100644 blob [0-9a-f]{40}\t" + re.escape(path.encode()) + rb"\x00", entry),
            "Protected main has no regular admission record")
    raw = git("show", f"refs/remotes/origin/main:{path}")[1]
    require(len(raw) <= 4096, "Admission record is too large")

    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, "Admission record has duplicate keys")
            result[key] = value
        return result

    try:
        record = json.loads(raw, object_pairs_hook=unique)
    except (ValueError, UnicodeError) as error:
        raise Denied("Admission record JSON invalid") from error
    keys = {"schema", "tag", "release_commit", "source_commit", "release_tree",
            "base_commit", "source_pr", "source_ci_run", "upstream_pr", "upstream_commit"}
    require(type(record) is dict and set(record) == keys, "Admission record schema invalid")
    require(type(record["schema"]) is int and record["schema"] == 1,
            "Admission record version invalid")
    for field in ("release_commit", "source_commit", "release_tree", "base_commit", "upstream_commit"):
        require(oid(record[field]), "Admission record commit or tree invalid")
    for field in ("source_pr", "source_ci_run", "upstream_pr"):
        require(type(record[field]) is int and record[field] > 0, "Admission record identifier invalid")
    return record


def merged_pr(api, number, branch):
    pr = api(f"pulls/{number}")
    require(type(pr) is dict and pr.get("number") == number and pr.get("merged") is True,
            "Required pull request is not merged")
    require(pr.get("base", {}).get("ref") == branch
            and pr.get("base", {}).get("repo", {}).get("full_name") == REPOSITORY
            and pr.get("head", {}).get("repo", {}).get("full_name") == REPOSITORY,
            "Pull request repository or branch mismatch")
    return pr


def verify_release(env, api=github_api, policy=Policy()):
    tag, commit = env.get("TAG_NAME", ""), env.get("GITHUB_SHA", "")
    require(env.get("GITHUB_REPOSITORY") == REPOSITORY
            and env.get("GITHUB_EVENT_NAME") == "push"
            and env.get("GITHUB_REF") == f"refs/tags/{tag}", "Release event context invalid")
    require(re.fullmatch(r"v0\.74\.(0|[1-9][0-9]*)", tag) and oid(commit),
            "Maintenance tag or commit invalid")
    require(git_text("rev-parse", "--verify", f"refs/tags/{tag}^{{commit}}") == commit
            and git_text("rev-parse", "HEAD") == commit, "Tag and checked-out source mismatch")
    git("fetch", "--no-tags", "origin", "refs/heads/main:refs/remotes/origin/main")
    if ancestor(commit, "refs/remotes/origin/main"):
        return "protected-main"

    require(tag == "v0.74.8", "No exception for this maintenance tag")
    record = admission_record()
    require(record["tag"] == tag and record["release_commit"] == commit
            and record["base_commit"] == policy.base_commit and record["source_pr"] == 1041,
            "Admission does not match this exact release")
    require(git_text("rev-parse", "refs/tags/v0.74.7^{commit}") == policy.base_commit,
            "Maintenance base tag mismatch")

    source = merged_pr(api, 1041, "maintenance/v0.74")
    require(source.get("merge_commit_sha") == commit
            and source.get("head", {}).get("sha") == record["source_commit"],
            "Maintenance merge or reviewed source mismatch")
    upstream = merged_pr(api, record["upstream_pr"], "main")
    require(upstream.get("merge_commit_sha") == record["upstream_commit"],
            "Upstream merge mismatch")
    require(ancestor(record["upstream_commit"], "refs/remotes/origin/main"),
            "JWT fix is not on protected main")
    require(patch_id(record["upstream_commit"] + "^", record["upstream_commit"])
            == policy.auth_patch_id, "Upstream JWT patch mismatch")

    git("fetch", "--no-tags", "origin", "refs/heads/maintenance/v0.74:refs/remotes/origin/maintenance/v0.74",
        "refs/pull/1041/head:refs/release-source/1041")
    require(git_text("rev-parse", "refs/release-source/1041") == record["source_commit"],
            "Reviewed maintenance source moved")
    require(ancestor(commit, "refs/remotes/origin/maintenance/v0.74")
            and ancestor(policy.base_commit, commit)
            and ancestor(policy.base_commit, record["source_commit"]), "Maintenance lineage mismatch")
    require(git_text("rev-parse", f"{commit}^{{tree}}") == record["release_tree"]
            and git_text("rev-parse", f"{record['source_commit']}^{{tree}}") == record["release_tree"],
            "Release tree differs from reviewed source")
    require(patch_id(policy.base_commit, record["source_commit"]) == policy.auth_patch_id,
            "Backported JWT patch mismatch")
    run = api(f"actions/runs/{record['source_ci_run']}")
    require(type(run) is dict and run.get("id") == record["source_ci_run"]
            and run.get("repository", {}).get("full_name") == REPOSITORY
            and run.get("path") == ".github/workflows/maintenance-jwt.yml"
            and run.get("head_sha") == record["source_commit"]
            and run.get("event") == "pull_request"
            and run.get("status") == "completed" and run.get("conclusion") == "success"
            and any(pr.get("number") == 1041 for pr in run.get("pull_requests", [])),
            "Exact maintenance source CI is not successful")
    return "reviewed-v0.74.8"


if __name__ == "__main__":
    try:
        print("Release provenance verified: " + verify_release(os.environ))
    except (Denied, UnicodeError, KeyError, TypeError, AttributeError) as error:
        # Never print remote responses, commit messages, or API stderr.
        print("Release provenance denied: " + (str(error) if isinstance(error, Denied) else "Invalid provenance data"))
        raise SystemExit(1)
