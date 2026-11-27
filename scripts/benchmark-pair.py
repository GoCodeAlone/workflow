#!/usr/bin/env python3
"""Collect an immutable base/merge benchmark pair; never decide performance success.

This helper is executable authority: review its complete bytes and stage its hash
with the workflow context before adoption. Same-job execution is not a sandbox
against malicious repository code. Only reviewed sources belong in this lane.
"""

import hashlib
import json
import os
from pathlib import Path
import platform
import re
import signal
import subprocess
import sys
import time


SHA = re.compile(r"[0-9a-f]{40}")
INPUTS = ("go.mod", "go.sum", "ui/package-lock.json")
BENCHMARK = ["go", "test", "-bench=.", "-benchmem", "-count=6", "-run=^$", "-timeout=30m", "./..."]
LISTING = ["go", "test", "-list=^Benchmark", "./..."]
SONIC = "github.com/bytedance/sonic"
# Filename-to-mode semantics were reviewed against these exact module bytes.
# A new dependency version must be reviewed, not guessed to use the same backend.
SONIC_IDENTITY = {"Path": SONIC, "Version": "v1.15.4",
                  "Sum": "h1:FgtV/4aBHpla9AxuMpuuzVUpa/Cf3izufkxNmnEzdI8=",
                  "GoModSum": "h1:8e51yTPdY8M6t+vvGL1c2Y1xL9i+frEeIAQAEl75NUc="}
GO_ENV = ("GOVERSION", "GOOS", "GOARCH", "GOAMD64", "GOARM64", "CGO_ENABLED", "GOFLAGS", "GOWORK", "GOEXPERIMENT",
          "CC", "CXX", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_FFLAGS", "CGO_LDFLAGS")


def demand(condition, message):
    if not condition:
        raise ValueError(message)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def save_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


def metadata(argv, directory):
    # Fixed metadata commands only. No shell, Git hooks, or repository executable.
    result = subprocess.run(["git", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", *argv], cwd=directory,
                            check=True, capture_output=True, timeout=30)
    return result.stdout


def commit_identity(directory, ref="HEAD"):
    demand(ref == "HEAD" or SHA.fullmatch(ref), "invalid commit reference")
    sha = metadata(["rev-parse", ref], directory).decode().strip()
    raw = metadata(["cat-file", "commit", ref], directory)
    headers = raw.split(b"\n\n", 1)[0].decode("utf-8")
    trees = [line[5:] for line in headers.splitlines() if line.startswith("tree ")]
    parents = [line[7:] for line in headers.splitlines() if line.startswith("parent ")]
    demand(SHA.fullmatch(sha) and len(trees) == 1 and SHA.fullmatch(trees[0]) and
           all(SHA.fullmatch(parent) for parent in parents), "invalid Git identity")
    return {"sha": sha, "tree": trees[0], "parents": parents, "commitObjectSHA256": digest(raw)}


def input_hashes(directory):
    result = {}
    for name in INPUTS:
        file = directory / name
        demand(file.is_file() and not file.is_symlink(), "missing or symbolic input " + name)
        data = file.read_bytes()
        demand(data == metadata(["show", "HEAD:" + name], directory), "input differs from commit: " + name)
        result[name] = digest(data)
    return result


def source_clean(directory):
    demand(not metadata(["status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=none"], directory),
           "checkout contains modified or untracked source")


def module_graph_bound(modules, directory):
    demand(modules and sum(item.get("Main") is True for item in modules) == 1, "module graph lacks one root")
    for item in modules:
        demand(not item.get("Error"), "incomplete module graph")
        replacement = item.get("Replace")
        if replacement and not replacement.get("Version"):
            path = replacement.get("Dir", "")
            demand(path and Path(path).resolve().is_relative_to(directory.resolve()), "local module replacement escapes checkout")


def expected_identity(environment):
    values = {key: environment.get("PAIR_" + key.upper(), "") for key in
              ("base", "head", "merge", "event", "ref", "repository", "run_id", "run_attempt")}
    demand(values["event"] == "pull_request" and re.fullmatch(r"refs/pull/[1-9][0-9]*/merge", values["ref"]),
           "paired measurements require the pull-request synthetic merge event")
    demand(all(SHA.fullmatch(values[key]) for key in ("base", "head", "merge")), "full immutable SHAs required")
    demand(re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", values["repository"]), "invalid repository")
    demand(all(re.fullmatch(r"[1-9][0-9]*", values[key]) for key in ("run_id", "run_attempt")), "invalid run identity")
    return values


def bind(expected, base, candidate):
    demand(base["sha"] == expected["base"], "base checkout drift")
    demand(candidate["sha"] == expected["merge"], "candidate is not the event merge SHA")
    demand(candidate["parents"] == [expected["base"], expected["head"]], "merge parents do not bind exact base and head")
    demand(expected["merge"] not in (expected["base"], expected["head"]), "bare head/base is not a synthetic merge")


def host_identity():
    raw = Path("/proc/cpuinfo").read_bytes()
    fields = {}
    for line in raw.decode().splitlines():
        key, separator, value = line.partition(":")
        key = key.strip()
        if separator and key in ("vendor_id", "model name", "cpu family", "model", "stepping", "microcode", "flags", "Features", "CPU implementer", "CPU architecture", "CPU part", "CPU revision"):
            fields.setdefault(key, set()).add(value.strip())
    demand(fields, "CPU identity unavailable")
    return {"system": platform.system(), "architecture": platform.machine(), "kernel": platform.release(),
            "cpu": {key: sorted(value) for key, value in fields.items()}}, raw


def streams(text):
    decoder, values = json.JSONDecoder(), []
    while text.strip():
        text = text.lstrip()
        value, end = decoder.raw_decode(text)
        values.append(value)
        text = text[end:]
    return values


def serializer_mode(packages):
    expected = {SONIC: ("sonic.go", "compat.go"), SONIC + "/ast": ("api.go", "api_compat.go")}
    result = {}
    demand(len(packages) == 2, "serializer package inventory incomplete")
    for package in packages:
        name = package.get("ImportPath")
        demand(name in expected and name not in result and not package.get("Error"), "unexpected serializer package")
        module = package.get("Module", {})
        demand(not module.get("Replace"), "serializer replacement requires separate review")
        demand(all(module.get(key) == value for key, value in SONIC_IDENTITY.items()), "serializer module identity requires separate review")
        files = package.get("GoFiles", [])
        native, fallback = expected[name]
        demand((native in files) != (fallback in files), "unrecognized serializer implementation selection")
        result[name] = {"mode": "native" if native in files else "encoding/json-fallback", "goFiles": sorted(files),
                        "module": module}
    return result


def listing_roots(text):
    pending, roots = [], set()
    status = re.compile(r"ok\s+(\S+)\s+(?:[0-9]+(?:[.][0-9]+)?s|\(cached\))(?:\s+\[no tests to run\])?")
    for raw in text.splitlines():
        line = raw.strip()
        if line.startswith("Benchmark"):
            demand(re.fullmatch(r"Benchmark[A-Za-z0-9_]+", line), "invalid benchmark listing")
            pending.append(line)
        elif match := status.fullmatch(line):
            for name in pending:
                key = (match[1], name)
                demand(key not in roots, "duplicate benchmark root")
                roots.add(key)
            pending = []
        else:
            demand(line in ("", "PASS") or re.fullmatch(r"\?\s+\S+\s+\[no test files\]", line), "unsupported listing diagnostic")
    demand(roots and not pending, "incomplete benchmark listing")
    return roots


def validate_pair(sides):
    base, candidate = sides["base"], sides["candidate"]
    demand(base["host"] == candidate["host"], "host/CPU identity mismatch")
    demand(base["compiler"] == candidate["compiler"], "compiler mismatch")
    demand(base["goEnv"] == candidate["goEnv"], "compiler/flags/OS/architecture mismatch")
    demand(base["goEnv"].get("GOVERSION") == "go1.27.2", "compiler differs from pinned workflow")
    demand(base["goEnv"].get("GOFLAGS") == "-mod=readonly" and base["goEnv"].get("GOWORK") == "off", "unreviewed build flags")
    demand(set(base["serializer"]) == {SONIC, SONIC + "/ast"} == set(candidate["serializer"]), "missing serializer evidence")
    for name in base["serializer"]:
        demand(base["serializer"][name]["mode"] == candidate["serializer"][name]["mode"], "serializer mode mismatch")
    demand(base["roots"] == candidate["roots"], "base/candidate benchmark listings differ")
    for side in sides.values():
        demand(side["complete"] is True, "incomplete measurement")
        demand(not side["listingStderr"] and not side["measurementStderr"], "raw benchmark diagnostics require review")


def phase(command, directory, environment, output, name, seconds):
    stdout, stderr = output / (name + ".stdout"), output / (name + ".stderr")
    started = time.time()
    receipt = {"command": command, "timeoutSeconds": seconds, "startedUnix": started, "complete": False}
    try:
        with stdout.open("xb") as out, stderr.open("xb") as err:
            process = subprocess.Popen(command, cwd=directory, env=environment, stdout=out, stderr=err, start_new_session=True)
            try:
                status = process.wait(timeout=seconds)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait()
                raise ValueError("phase timed out: " + name)
        receipt["exitCode"] = status
        demand(status == 0, "phase failed: " + name)
        receipt["complete"] = True
        return stdout.read_text(encoding="utf-8")
    finally:
        receipt["endedUnix"] = time.time()
        receipt["files"] = {file.name: digest(file.read_bytes()) for file in (stdout, stderr) if file.exists()}
        save_json(output / (name + ".receipt.json"), receipt)


def prepare(workspace, environment):
    expected = expected_identity(environment)
    directories = {side: workspace / side for side in ("base", "candidate")}
    demand(all(path.is_dir() and not path.is_symlink() for path in directories.values()), "invalid checkout directory")
    for path in directories.values():
        demand((path / "ui").is_dir() and (path / "ui").resolve().is_relative_to(path.resolve()), "UI directory escapes checkout")
        source_clean(path)
    identities = {side: commit_identity(path) for side, path in directories.items()}
    bind(expected, identities["base"], identities["candidate"])
    identities["head"] = commit_identity(directories["candidate"], expected["head"])
    demand(identities["head"]["sha"] == expected["head"], "head object identity mismatch")
    output = workspace / "benchmark-pair"
    output.mkdir()  # Refuse prior results: neither measurements nor receipts are cached.
    cache = Path(environment["RUNNER_TEMP"]) / ("benchmark-pair-" + expected["run_id"] + "-" + expected["run_attempt"])
    demand(cache.is_absolute() and not cache.exists(), "cache root must be new and absolute")
    cache.mkdir()
    receipt = {"expected": expected, "identities": identities, "cacheRoot": str(cache), "inputs": {},
               "collectorSHA256": digest((directories["candidate"] / "scripts/benchmark-pair.py").read_bytes()),
               "workflowSHA256": digest((directories["candidate"] / ".github/workflows/benchmark.yml").read_bytes())}
    for side, directory in directories.items():
        (output / side).mkdir()
        (cache / side).mkdir()
        receipt["inputs"][side] = input_hashes(directory)
        for name in INPUTS:
            target = output / side / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes((directory / name).read_bytes())
    save_json(output / "prepared.json", receipt)


def measure(workspace, environment):
    output = workspace / "benchmark-pair"
    receipt = read_json(output / "prepared.json")
    expected = expected_identity(environment)
    demand(receipt["expected"] == expected, "run metadata drift")
    identities = {side: commit_identity(workspace / side) for side in ("base", "candidate")}
    bind(expected, identities["base"], identities["candidate"])
    identities["head"] = commit_identity(workspace / "candidate", expected["head"])
    demand(identities == receipt["identities"], "checkout identity changed after preparation")
    demand(digest((workspace / "candidate/scripts/benchmark-pair.py").read_bytes()) == receipt["collectorSHA256"], "collector changed after preparation")
    demand(digest((workspace / "candidate/.github/workflows/benchmark.yml").read_bytes()) == receipt["workflowSHA256"], "workflow changed after preparation")
    sides = {}
    for side in ("base", "candidate"):
        directory, side_output = workspace / side, output / side
        source_clean(directory)
        demand(input_hashes(directory) == receipt["inputs"][side], "module or UI lockfile changed")
        cache = Path(receipt["cacheRoot"]) / side
        env = dict(environment, GOWORK="off", GOENV="off", GOTOOLCHAIN="local", GOFLAGS="-mod=readonly", LC_ALL="C",
                   GOCACHE=str(cache / "build"), GOMODCACHE=str(cache / "modules"), GOPATH=str(cache / "gopath"))
        before, raw = host_identity()
        (side_output / "cpu-before.txt").write_bytes(raw)
        compiler = phase(["go", "version"], directory, env, side_output, "compiler", 30).strip()
        config = json.loads(phase(["go", "env", "-json", *GO_ENV], directory, env, side_output, "go-env", 30))
        demand(config.get("GOVERSION") == "go1.27.2", "compiler differs from pinned workflow")
        config["GOMAXPROCS_environment"] = environment.get("GOMAXPROCS", "")
        phase(["go", "mod", "download", "-json"], directory, env, side_output, "module-download", 600)
        phase(["go", "mod", "verify"], directory, env, side_output, "module-verify", 120)
        phase(["go", "mod", "graph"], directory, env, side_output, "module-graph", 120)
        modules = streams(phase(["go", "list", "-m", "-json", "all"], directory, env, side_output, "module-list", 120))
        module_graph_bound(modules, directory)
        selection = streams(phase(["go", "list", "-json", SONIC, SONIC + "/ast"], directory, env, side_output, "serializer", 120))
        mode = serializer_mode(selection)
        listing = phase(LISTING, directory, env, side_output, "listing", 600)
        phase(BENCHMARK, directory, env, side_output, "measurement", 1860)
        after, raw = host_identity()
        (side_output / "cpu-after.txt").write_bytes(raw)
        demand(before == after, "host identity changed during measurement")
        source_clean(directory)
        demand(commit_identity(directory) == identities[side], "commit identity changed during measurement")
        demand(input_hashes(directory) == receipt["inputs"][side], "module or lockfile changed during measurement")
        sides[side] = {"host": before, "compiler": compiler, "goEnv": config, "serializer": mode,
                       "roots": sorted(listing_roots(listing)), "complete": True, "sourceCleanBeforeAndAfter": True,
                       "listingStderr": (side_output / "listing.stderr").stat().st_size,
                       "measurementStderr": (side_output / "measurement.stderr").stat().st_size}
        save_json(side_output / "summary.json", sides[side])
    validate_pair(sides)
    exports = {"benchmark-list.txt": output / "candidate/listing.stdout",
               "benchmark-results.txt": output / "candidate/measurement.stdout",
               "baseline-bench.txt": output / "base/measurement.stdout"}
    for name, source in exports.items():
        with (workspace / name).open("xb") as target:
            target.write(source.read_bytes())
    save_json(output / "pair.json", {"status": "inputs-complete-not-performance-verdict", "expected": expected,
                                    "identities": identities, "sides": sides,
                                    "exports": {name: digest(source.read_bytes()) for name, source in exports.items()}})


def main():
    workspace = Path(__file__).resolve().parents[2]
    demand(len(sys.argv) == 2 and sys.argv[1] in ("prepare", "measure"), "expected prepare or measure")
    try:
        {"prepare": prepare, "measure": measure}[sys.argv[1]](workspace, dict(os.environ))
    except Exception as error:
        output = workspace / "benchmark-pair"
        if output.is_dir():
            save_json(output / "failure.json", {"status": "unassessed", "error": str(error)})
        raise


if __name__ == "__main__":
    main()
