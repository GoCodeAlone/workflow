#!/usr/bin/env python3
"""Bounded synthetic tests only. Never build or run repository benchmarks."""

import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("benchmark_pair", ROOT / "scripts/benchmark-pair.py")
PAIR = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PAIR)
WORKFLOW = (ROOT / ".github/workflows/benchmark.yml").read_text()
AWK = WORKFLOW.split("        validator=$(cat <<'AWK'\n", 1)[1].split("\n        AWK\n", 1)[0]
AWK = "\n".join(line[8:] for line in AWK.splitlines()) + "\n"
BASE, HEAD, MERGE = "1" * 40, "2" * 40, "3" * 40


def environment():
    return dict(PAIR_BASE=BASE, PAIR_HEAD=HEAD, PAIR_MERGE=MERGE, PAIR_EVENT="pull_request",
                PAIR_REF="refs/pull/42/merge", PAIR_REPOSITORY="example/project", PAIR_RUN_ID="1", PAIR_RUN_ATTEMPT="1")


def selected(native=True):
    return [{"ImportPath": PAIR.SONIC, "GoFiles": ["api.go", "sonic.go" if native else "compat.go"], "Module": dict(PAIR.SONIC_IDENTITY)},
            {"ImportPath": PAIR.SONIC + "/ast", "GoFiles": ["api.go" if native else "api_compat.go", "node.go"], "Module": dict(PAIR.SONIC_IDENTITY)}]


def pair():
    side = {"host": {"cpu": "Synthetic CPU", "architecture": "amd64"}, "compiler": "go version go1.27.2 linux/amd64",
            "goEnv": {"GOVERSION": "go1.27.2", "GOFLAGS": "-mod=readonly", "GOWORK": "off", "GOOS": "linux", "GOARCH": "amd64"},
            "serializer": PAIR.serializer_mode(selected()), "roots": [("example/pkg", "BenchmarkA")],
            "complete": True, "listingStderr": 0, "measurementStderr": 0}
    return {"base": copy.deepcopy(side), "candidate": copy.deepcopy(side)}


class PureContractTests(unittest.TestCase):
    def setUp(self):
        self.no_workload = patch.object(PAIR.subprocess, "Popen", side_effect=AssertionError("real workload forbidden in synthetic test"))
        self.no_workload.start()
        self.addCleanup(self.no_workload.stop)

    def test_exact_merge_binding(self):
        PAIR.bind(PAIR.expected_identity(environment()), {"sha": BASE}, {"sha": MERGE, "parents": [BASE, HEAD]})

    def test_identity_rejects_event_tip_and_invalid_values(self):
        for key, value in [("PAIR_BASE", "main"), ("PAIR_HEAD", HEAD[:7]), ("PAIR_MERGE", "0"),
                           ("PAIR_EVENT", "push"), ("PAIR_REF", "refs/heads/main"),
                           ("PAIR_RUN_ID", "1\nother"), ("PAIR_REPOSITORY", "../../other")]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                env = environment(); env[key] = value
                PAIR.expected_identity(env)

    def test_binding_rejects_head_base_and_wrong_parents(self):
        expected = PAIR.expected_identity(environment())
        cases = [({"sha": HEAD}, {"sha": MERGE, "parents": [BASE, HEAD]}),
                 ({"sha": BASE}, {"sha": HEAD, "parents": [BASE, HEAD]}),
                 ({"sha": BASE}, {"sha": MERGE, "parents": [HEAD, BASE]}),
                 ({"sha": BASE}, {"sha": MERGE, "parents": [BASE]}),
                 ({"sha": BASE}, {"sha": MERGE, "parents": [BASE, HEAD, HEAD]})]
        for base, candidate in cases:
            with self.subTest(candidate=candidate), self.assertRaises(ValueError):
                PAIR.bind(expected, base, candidate)

    def test_commit_metadata_preserves_shallow_parent_headers(self):
        raw = ("tree " + "4" * 40 + "\nparent " + BASE + "\nparent " + HEAD + "\nauthor Test <test@example.invalid> 1 +0000\n\nSynthetic\n").encode()
        with patch.object(PAIR, "metadata", side_effect=[MERGE.encode() + b"\n", raw]):
            actual = PAIR.commit_identity(Path("unused"))
        self.assertEqual(actual["parents"], [BASE, HEAD])
        self.assertEqual(actual["commitObjectSHA256"], hashlib.sha256(raw).hexdigest())

    def test_json_stream_is_complete(self):
        self.assertEqual(PAIR.streams(' {"a":1}\n {"b":2} '), [{"a": 1}, {"b": 2}])
        with self.assertRaises(ValueError): PAIR.streams('{"a":1}\ntruncated')

    def test_source_cleanliness_and_module_replacement_confinement(self):
        with patch.object(PAIR, "metadata", return_value=b" M source.go\n"), self.assertRaises(ValueError):
            PAIR.source_clean(Path("unused"))
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            PAIR.module_graph_bound([{"Main": True}, {"Replace": {"Dir": str(root / "internal")}}], root)
            for records in ([], [{"Main": True}, {"Replace": {"Dir": str(root.parent / "other")}}], [{"Main": True}, {"Error": "missing"}]):
                with self.subTest(records=records), self.assertRaises(ValueError): PAIR.module_graph_bound(records, root)

    def test_serializer_records_real_selection(self):
        self.assertEqual(PAIR.serializer_mode(selected())[PAIR.SONIC]["mode"], "native")
        self.assertEqual(PAIR.serializer_mode(selected(False))[PAIR.SONIC]["mode"], "encoding/json-fallback")

    def test_serializer_rejects_missing_ambiguous_duplicate_or_replaced_sources(self):
        cases = [selected()[:1], selected() + selected()[:1], [selected()[0], selected()[0]]]
        for files in ([], ["sonic.go", "compat.go"], ["unknown.go"]):
            item = selected(); item[0]["GoFiles"] = files; cases.append(item)
        item = selected(); item[0]["Module"] = {"Replace": {"Dir": "/unreviewed"}}; cases.append(item)
        for key in PAIR.SONIC_IDENTITY:
            item = selected(); item[0]["Module"][key] = "unreviewed"; cases.append(item)
        item = selected(); del item[0]["Module"]; cases.append(item)
        for case in cases:
            with self.subTest(case=case), self.assertRaises(ValueError): PAIR.serializer_mode(case)

    def test_listing_requires_complete_matching_root_sets(self):
        actual = PAIR.listing_roots("BenchmarkA\nBenchmarkB\nok example/pkg 0.01s\n? example/empty [no test files]\n")
        self.assertEqual(actual, {("example/pkg", "BenchmarkA"), ("example/pkg", "BenchmarkB")})
        for raw in ["", "BenchmarkA\n", "WARNING: fallback\n", "BenchmarkA;bad\n", "BenchmarkA\nBenchmarkA\nok example/pkg 1s\n"]:
            with self.subTest(raw=raw), self.assertRaises(ValueError): PAIR.listing_roots(raw)

    def test_pair_accepts_consistent_complete_evidence(self):
        PAIR.validate_pair(pair())

    def test_pair_rejects_cpu_compiler_flags_mode_completeness_and_diagnostics(self):
        changes = [("host", {"cpu": "Different"}), ("compiler", "go version go1.26.1 linux/amd64"),
                   ("goEnv", {"GOVERSION": "go1.27.2", "GOFLAGS": "-tags=other"}),
                   ("serializer", PAIR.serializer_mode(selected(False))), ("serializer", {}),
                   ("roots", []), ("complete", False), ("listingStderr", 1), ("measurementStderr", 1)]
        for field, value in changes:
            with self.subTest(field=field), self.assertRaises(ValueError):
                data = pair(); data["candidate"][field] = value; PAIR.validate_pair(data)

    def test_prepare_creates_new_separate_outputs_and_caches(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp); (root / "base").mkdir(); (root / "candidate").mkdir(); (root / "temp").mkdir()
            for side in ("base", "candidate"):
                (root / side / "ui").mkdir()
                for name in PAIR.INPUTS: (root / side / name).write_text("synthetic\n")
            for name in ("scripts/benchmark-pair.py", ".github/workflows/benchmark.yml"):
                file = root / "candidate" / name
                file.parent.mkdir(parents=True, exist_ok=True)
                file.write_text("synthetic source")
            env = environment(); env["RUNNER_TEMP"] = str(root / "temp")
            with patch.object(PAIR, "source_clean"), patch.object(PAIR, "commit_identity", side_effect=[{"sha": BASE}, {"sha": MERGE, "parents": [BASE, HEAD]}, {"sha": HEAD}]), patch.object(PAIR, "input_hashes", return_value={"go.mod": "synthetic"}):
                PAIR.prepare(root, env)
            receipt = PAIR.read_json(root / "benchmark-pair/prepared.json")
            self.assertTrue((Path(receipt["cacheRoot"]) / "base").is_dir())
            self.assertTrue((Path(receipt["cacheRoot"]) / "candidate").is_dir())
            with patch.object(PAIR, "source_clean"), patch.object(PAIR, "commit_identity", side_effect=[{"sha": BASE}, {"sha": MERGE, "parents": [BASE, HEAD]}, {"sha": HEAD}]), self.assertRaises(FileExistsError):
                PAIR.prepare(root, env)

    def test_measure_fails_before_commands_when_metadata_drifted(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp); (root / "benchmark-pair").mkdir()
            PAIR.save_json(root / "benchmark-pair/prepared.json", {"expected": {"base": HEAD}})
            with self.assertRaisesRegex(ValueError, "metadata drift"):
                PAIR.measure(root, environment())

    def test_full_collector_with_only_mocked_commands(self):
        for failure in (None, "diagnostic", "changed-input", "timeout", "changed-source"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                for side in ("base", "candidate"):
                    (root / side / "ui").mkdir(parents=True)
                    for name in PAIR.INPUTS: (root / side / name).write_text("synthetic\n")
                for name in ("scripts/benchmark-pair.py", ".github/workflows/benchmark.yml"):
                    file = root / "candidate" / name; file.parent.mkdir(parents=True, exist_ok=True); file.write_text("synthetic source")
                (root / "temp").mkdir()
                env = environment(); env["RUNNER_TEMP"] = str(root / "temp")
                def identity(directory, ref="HEAD"):
                    return {"sha": HEAD, "tree": "6" * 40, "parents": [BASE]} if ref == HEAD else ({"sha": BASE, "tree": "4" * 40, "parents": []} if directory.name == "base" else {"sha": MERGE, "tree": "5" * 40, "parents": [BASE, HEAD]})
                calls = []
                def fake_phase(command, directory, config, output, name, seconds):
                    calls.append((command, directory, dict(config), output))
                    text = {"compiler": "go version go1.27.2 linux/amd64\n",
                            "go-env": json.dumps(pair()["base"]["goEnv"]),
                            "module-list": json.dumps({"Main": True, "Path": "example/project"}),
                            "serializer": "\n".join(json.dumps(item) for item in selected()),
                            "listing": "BenchmarkA\nok example/pkg 0.1s\n",
                            "measurement": "goos: linux\ngoarch: amd64\npkg: example/pkg\ncpu: Synthetic CPU\n" + "BenchmarkA-2 100 100 ns/op 8 B/op 1 allocs/op\n" * 6 + "PASS\nok example/pkg 0.1s\n"}.get(name, "synthetic metadata\n")
                    (output / (name + ".stdout")).write_text(text)
                    (output / (name + ".stderr")).write_text("diagnostic\n" if failure == "diagnostic" and directory.name == "candidate" and name == "measurement" else "")
                    PAIR.save_json(output / (name + ".receipt.json"), {"complete": failure != "timeout"})
                    if failure == "timeout" and directory.name == "candidate" and name == "measurement": raise ValueError("synthetic timeout")
                    return text
                with patch.object(PAIR, "source_clean") as clean, patch.object(PAIR, "commit_identity", side_effect=identity), patch.object(PAIR, "input_hashes", return_value={"go.mod": "synthetic"}) as inputs, patch.object(PAIR, "host_identity", return_value=({"cpu": "Synthetic CPU"}, b"synthetic raw CPU")), patch.object(PAIR, "phase", side_effect=fake_phase):
                    PAIR.prepare(root, env)
                    if failure == "changed-input": inputs.return_value = {"go.mod": "changed"}
                    if failure == "changed-source": clean.side_effect = ValueError("synthetic source drift")
                    if failure:
                        with self.assertRaises(ValueError): PAIR.measure(root, env)
                        self.assertFalse((root / "benchmark-pair/pair.json").exists())
                        self.assertFalse((root / "benchmark-results.txt").exists())
                    else:
                        PAIR.measure(root, env)
                        self.assertEqual(len(calls), 18)
                        for command in (PAIR.LISTING, PAIR.BENCHMARK):
                            self.assertEqual([call[1].name for call in calls if call[0] == command], ["base", "candidate"])
                        for variable in ("GOCACHE", "GOMODCACHE", "GOPATH"):
                            self.assertEqual(len({call[2][variable] for call in calls}), 2)
                        for command, directory, config, output in calls:
                            self.assertEqual(directory.name, output.name)
                            self.assertEqual(config["GOWORK"], "off"); self.assertEqual(config["GOFLAGS"], "-mod=readonly")
                            self.assertEqual(config["GOTOOLCHAIN"], "local"); self.assertEqual(config["GOENV"], "off")
                        receipt = PAIR.read_json(root / "benchmark-pair/pair.json")
                        self.assertEqual(receipt["status"], "inputs-complete-not-performance-verdict")
                        self.assertEqual(receipt["identities"]["head"]["sha"], HEAD)
                        for name, digest in receipt["exports"].items():
                            self.assertEqual(hashlib.sha256((root / name).read_bytes()).hexdigest(), digest)
                        self.assertEqual((root / "baseline-bench.txt").read_bytes(), (root / "benchmark-pair/base/measurement.stdout").read_bytes())

    def test_phase_records_failed_and_timed_out_synthetic_process(self):
        for timeout in (False, True):
            with self.subTest(timeout=timeout), tempfile.TemporaryDirectory() as temp:
                fake = unittest.mock.Mock(pid=12345)
                fake.wait.side_effect = [subprocess.TimeoutExpired("synthetic", 1), 0] if timeout else None
                fake.wait.return_value = 1
                with patch.object(PAIR.subprocess, "Popen", return_value=fake), patch.object(PAIR.os, "killpg") as kill, self.assertRaises(ValueError):
                    PAIR.phase(["never-executed"], Path(temp), {}, Path(temp), "synthetic", 1)
                receipt = PAIR.read_json(Path(temp) / "synthetic.receipt.json")
                self.assertFalse(receipt["complete"])
                self.assertEqual(kill.called, timeout)


class WorkflowContractTests(unittest.TestCase):
    def test_strict_validator_is_byte_identical_to_reviewed_base(self):
        original = WORKFLOW.split("        validator=$(cat <<'AWK'\n", 1)[1].split("\n        AWK\n", 1)[0]
        self.assertEqual(hashlib.sha256(original.encode()).hexdigest(), "289317ef6cd79ba65fd6523c2b73185ab9e226c39a140a15b0d059f0ac0a7997")

    def test_native_identity_and_read_only_permissions(self):
        for required in ("name: Benchmark\n", "  benchmark:\n", "    name: Performance Benchmarks\n", "    runs-on: ubuntu-latest\n"):
            self.assertIn(required, WORKFLOW)
        for forbidden in ("pull-requests: write", "secrets.", "actions/github-script@", "actions/cache/", "  push:", "container:", "services:"):
            self.assertNotIn(forbidden, WORKFLOW)

    def test_exact_checkouts_and_disjoint_npm_caches(self):
        self.assertEqual(WORKFLOW.count("persist-credentials: false"), 2)
        self.assertIn("ref: ${{ github.event.pull_request.base.sha }}", WORKFLOW)
        self.assertIn("ref: ${{ github.sha }}", WORKFLOW)
        self.assertIn("fetch-depth: 2", WORKFLOW)
        self.assertEqual(WORKFLOW.count("npm ci"), 2)
        self.assertEqual(WORKFLOW.count("NODE_AUTH_TOKEN: ${{ github.token }}"), 2)
        self.assertIn("/base/npm", WORKFLOW); self.assertIn("/candidate/npm", WORKFLOW)
        self.assertNotIn("cache: 'npm'", WORKFLOW)

    def test_preserves_pinned_install_six_samples_and_unconditional_pair(self):
        self.assertIn("run: go install golang.org/x/perf/cmd/benchstat@v0.0.0-20260709024250-82a0b07e230d", WORKFLOW)
        self.assertIn("BASELINE_AVAILABLE: 'true'", WORKFLOW)
        self.assertEqual(PAIR.BENCHMARK, ["go", "test", "-bench=.", "-benchmem", "-count=6", "-run=^$", "-timeout=30m", "./..."])
        self.assertIn("benchmark-pair/", WORKFLOW)
        self.assertIn("timeout-minutes: 180", WORKFLOW)


class StrictValidatorTests(unittest.TestCase):
    def run_validator(self, samples=6, cpu="Synthetic CPU", delta="+0.00%", malformed=False, extra=False, report=True):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            listing = "BenchmarkA\nok example/pkg 0.01s\n"
            header = "goos: linux\ngoarch: amd64\npkg: example/pkg\ncpu: Synthetic CPU\n"
            measure = "BenchmarkA-2 100 100 ns/op 8 B/op 1 allocs/op\n"
            (root / "benchmark-list.txt").write_text(listing)
            (root / "benchmark-results.txt").write_text(header + measure * 6 + "PASS\nok example/pkg 0.1s\n")
            (root / "baseline-bench.txt").write_text(header.replace("Synthetic CPU", cpu) + measure * samples + ("WARNING: arbitrary diagnostic\n" if malformed else "") + "PASS\nok example/pkg 0.1s\n")
            text = header
            for metric in ("sec/op", "B/op", "allocs/op"):
                text += "             │ baseline-bench.txt │ benchmark-results.txt │\n"
                text += f"             │ {metric} │ {metric} vs base │\n"
                text += f"A-2 100 ± 0% 100 ± 0% {delta} (p=0.001 n=6)\n"
            if extra: text += "UNSUPPORTED\n"
            (root / "benchstat-output.txt").write_text(text)
            files = ["benchmark-list.txt", "benchmark-results.txt", "baseline-bench.txt"]
            if report: files.append("benchstat-output.txt")
            result = subprocess.run(["awk", "-v", "mode=" + ("report" if report else "raw"), "-v", "baseline=true", "-v", "output=output.txt", AWK, *files], cwd=root, capture_output=True, text=True, timeout=5)
            return result.returncode, json.loads((root / "benchmark-validation.json").read_text())

    def test_complete_six_sample_pair(self):
        code, verdict = self.run_validator()
        self.assertEqual(code, 0); self.assertEqual(verdict["comparison"], "complete"); self.assertFalse(verdict["regression"])

    def test_strict_regression_boundary_and_retained_sqlite_finding(self):
        for delta, regression in [("+19.999999%", False), ("+20.00%", True), ("+104.53%", True), ("-20.00%", False)]:
            with self.subTest(delta=delta):
                code, verdict = self.run_validator(delta=delta)
                self.assertEqual(code, 0); self.assertEqual(verdict["regression"], regression)

    def test_incomplete_cpu_mismatch_diagnostics_and_report_errors_fail(self):
        for options in ({"samples": 5}, {"samples": 7}, {"cpu": "Different CPU"}, {"malformed": True}, {"extra": True}):
            with self.subTest(options=options):
                code, verdict = self.run_validator(**options)
                self.assertNotEqual(code, 0); self.assertEqual(verdict["comparison"], "not-run")


if __name__ == "__main__":
    unittest.main(verbosity=2)
