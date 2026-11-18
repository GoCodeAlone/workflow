#!/usr/bin/env python3
"""Keep main ancestry and exact, main-approved JWT/checksum maintenance admissions."""

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
    jwt_maintenance_commit: str = "24503c4f7cc268302814a88b7d0c2f13d6d2184d"
    checksum_upstream_commit: str = "5e3648d14b0a6de59ed4765893a13b712c66b641"
    checksum_blobs: tuple = (
        ("cmd/wfctl/plugin_checksum.go", "fac5e9c830f5cf64fef0341dbb28ee14f56fe551"),
        ("cmd/wfctl/plugin_install.go", "9740e355fa6411e15e2477ed824b762a2a86ea0a"),
        (".github/workflows/maintenance-jwt.yml", "b3fa061173a9185e72201f93d022408390f498c3"),
        (".github/workflows/maintenance-wfctl.yml", "dc82397c30bdbdb491c78e4300341b535de9a7ec"),
    )


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


def admission_record(tag="v0.74.8"):
    require(tag in ("v0.74.8", "v0.74.9"), "No exception for this maintenance tag")
    path = f".github/release-admissions/{tag}.json"
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
    if tag == "v0.74.9":
        keys.add("checksum_ci_run")
    require(type(record) is dict and set(record) == keys, "Admission record schema invalid")
    require(type(record["schema"]) is int and record["schema"] == (2 if tag == "v0.74.9" else 1),
            "Admission record version invalid")
    for field in ("release_commit", "source_commit", "release_tree", "base_commit", "upstream_commit"):
        require(oid(record[field]), "Admission record commit or tree invalid")
    identifiers = ("source_pr", "source_ci_run", "upstream_pr")
    if tag == "v0.74.9":
        identifiers += ("checksum_ci_run",)
    for field in identifiers:
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


def verify_checksum_release(tag, commit, api, policy):
    """Additional exact exception; the v0.74.8 JWT contract remains separate."""
    record = admission_record(tag)
    require(record["tag"] == tag and record["release_commit"] == commit
            and record["base_commit"] == policy.base_commit and record["source_pr"] == 1108
            and record["upstream_pr"] == 1042, "Admission does not match this exact release")
    require(git_text("rev-parse", "refs/tags/v0.74.7^{commit}") == policy.base_commit,
            "Maintenance base tag mismatch")
    source = merged_pr(api, 1108, "maintenance/v0.74")
    require(source.get("merge_commit_sha") == commit
            and source.get("head", {}).get("sha") == record["source_commit"],
            "Maintenance merge or reviewed source mismatch")
    upstream = merged_pr(api, 1042, "main")
    require(upstream.get("merge_commit_sha") == record["upstream_commit"], "Upstream merge mismatch")
    require(ancestor(record["upstream_commit"], "refs/remotes/origin/main"),
            "JWT fix is not on protected main")
    require(patch_id(record["upstream_commit"] + "^", record["upstream_commit"])
            == policy.auth_patch_id, "Upstream JWT patch mismatch")
    checksum = merged_pr(api, 960, "main")
    require(checksum.get("merge_commit_sha") == policy.checksum_upstream_commit,
            "Upstream checksum merge mismatch")
    require(ancestor(policy.checksum_upstream_commit, "refs/remotes/origin/main"),
            "Checksum fix is not on protected main")

    git("fetch", "--no-tags", "origin",
        "refs/heads/maintenance/v0.74:refs/remotes/origin/maintenance/v0.74",
        "refs/pull/1108/head:refs/release-source/1108")
    require(git_text("rev-parse", "refs/release-source/1108") == record["source_commit"],
            "Reviewed maintenance source moved")
    require(ancestor(commit, "refs/remotes/origin/maintenance/v0.74")
            and ancestor(policy.base_commit, policy.jwt_maintenance_commit)
            and ancestor(policy.jwt_maintenance_commit, commit)
            and ancestor(policy.jwt_maintenance_commit, record["source_commit"]),
            "Maintenance lineage mismatch")
    require(git_text("rev-parse", f"{commit}^{{tree}}") == record["release_tree"]
            and git_text("rev-parse", f"{record['source_commit']}^{{tree}}") == record["release_tree"],
            "Release tree differs from reviewed source")
    require(patch_id(policy.base_commit, policy.jwt_maintenance_commit) == policy.auth_patch_id
            and patch_id(policy.base_commit, record["source_commit"]) == policy.auth_patch_id,
            "Backported JWT patch mismatch")
    # Stable patch IDs normalize whitespace, including inside string literals.
    pinned_auth = git("ls-tree", "-rz", policy.jwt_maintenance_commit, "--", *AUTH_PATHS)[1]
    source_auth = git("ls-tree", "-rz", record["source_commit"], "--", *AUTH_PATHS)[1]
    require(pinned_auth.count(b"\0") == len(AUTH_PATHS) and source_auth == pinned_auth,
            "Backported JWT source identity mismatch")
    expected = b"".join(
        f"100644 blob {blob}\t{path}\0".encode("ascii")
        for path, blob in sorted(policy.checksum_blobs))
    entries = git("ls-tree", "-rz", record["source_commit"], "--",
                  *(path for path, _ in policy.checksum_blobs))[1]
    require(entries == expected, "Checksum production or workflow identity mismatch")
    for run_id, workflow in ((record["source_ci_run"], "maintenance-jwt.yml"),
                             (record["checksum_ci_run"], "maintenance-wfctl.yml")):
        run = api(f"actions/runs/{run_id}")
        require(type(run) is dict and type(run.get("id")) is int and run["id"] == run_id
                and run.get("repository", {}).get("full_name") == REPOSITORY
                and run.get("path") == ".github/workflows/" + workflow
                and run.get("head_sha") == record["source_commit"]
                and run.get("event") == "pull_request"
                and run.get("status") == "completed" and run.get("conclusion") == "success"
                and any(type(pr) is dict and type(pr.get("number")) is int
                        and pr["number"] == 1108 for pr in run.get("pull_requests", [])),
                "Exact maintenance source CI is not successful")
    return "reviewed-v0.74.9"


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

    if tag == "v0.74.9":
        return verify_checksum_release(tag, commit, api, policy)
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
