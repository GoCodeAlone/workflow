"""Exercise the package's real wfctl stdio entry with public fixtures only.

This is a maintainer test consumer, not a plugin tool or a YAML validator.
No user configuration, ambient environment values or raw server output is emitted.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import queue
import re
import shutil
import subprocess
import tempfile
import threading
import time

ROOT = Path(__file__).resolve().parents[1]
REASONS = {"missing_executable", "version_unknown", "version_mismatch", "wrong_server",
           "missing_tool", "schema_changed", "protocol_error", "tool_error", "timeout",
           "check_failed", "unexpected_stderr", "internal_error"}
REQUIRED = {"list_module_types", "list_step_types", "list_trigger_types", "list_workflow_types",
            "get_module_schema", "get_config_skeleton", "validate_config", "inspect_config",
            "compat_check", "template_validate_config", "list_plugins"}
DEPRECATION = ("[DEPRECATED] requires.plugins[].version and requires.plugins[].source in app.yaml "
               "are deprecated. Run 'wfctl migrate plugins' to migrate to wfctl.yaml + .wfctl-lock.yaml.")


class Failure(Exception):
    """Only allowlisted categories reach public output; exception text never does."""


def digest(data):
    return hashlib.sha256(data).hexdigest()


def check_version(info, expected):
    if not isinstance(info, dict) or info.get("name") != "wfctl-mcp":
        raise Failure("wrong_server")
    version = info.get("version")
    if not isinstance(version, str) or not re.fullmatch(r"v\d+\.\d+\.\d+", version):
        raise Failure("version_unknown")
    if version != expected:
        raise Failure("version_mismatch")


def check_arguments(tools, name, arguments):
    if name not in tools:
        raise Failure("missing_tool")
    schema = tools[name].get("inputSchema", {})
    if not isinstance(schema, dict) or "properties" not in schema:
        raise Failure("schema_changed")
    if not set(schema.get("required", [])) <= arguments.keys():
        raise Failure("schema_changed")
    if not arguments.keys() <= schema["properties"].keys():
        raise Failure("schema_changed")


def response_result(message, request_id):
    if (not isinstance(message, dict) or message.get("jsonrpc") != "2.0"
            or type(message.get("id")) is not int or message["id"] != request_id
            or "error" in message or not isinstance(message.get("result"), dict)):
        raise Failure("protocol_error")
    return message["result"]


def tool_json(result):
    if result.get("isError") or not isinstance(result.get("content"), list):
        raise Failure("tool_error")
    try:
        contents = result["content"]
        if len(contents) != 1 or contents[0].get("type") != "text":
            raise Failure("tool_error")
        value = json.loads(contents[0]["text"])
        if not isinstance(value, dict):
            raise Failure("tool_error")
        return value
    except (KeyError, TypeError, ValueError):
        raise Failure("tool_error") from None


class Client:
    def __init__(self, binary, data_dir):
        settings = json.loads((ROOT / "mcp.json").read_text(encoding="utf-8"))["mcpServers"]["workflow-config"]
        if settings["command"] != "wfctl" or settings["type"] != "stdio":
            raise Failure("schema_changed")
        def expand(value):
            return value.replace("${PLUGIN_ROOT}", str(ROOT)).replace("${PLUGIN_DATA}", str(data_dir))
        self.stderr = tempfile.TemporaryFile()
        self.process = subprocess.Popen(
            [str(binary), *map(expand, settings["args"])], cwd=expand(settings["cwd"]),
            env={"PATH": os.defpath, "TMPDIR": str(data_dir), **settings.get("env", {})},
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.stderr,
        )
        self.messages = queue.Queue(maxsize=32)
        self.stopping = threading.Event()
        self.request_id = 0
        self.tools = {}
        self.reader = threading.Thread(target=self._read, daemon=True)
        self.reader.start()

    def _read(self):
        try:
            while not self.stopping.is_set() and (line := self.process.stdout.readline(4 * 1024 * 1024)):
                if not line.endswith(b"\n"):
                    break
                self._enqueue(json.loads(line))
        except (OSError, ValueError):
            pass  # The consumer receives a protocol failure, without raw output.
        self._enqueue(None)

    def _enqueue(self, message):
        while not self.stopping.is_set():
            try:
                self.messages.put(message, timeout=0.1)
                return
            except queue.Full:
                continue

    def send(self, message):
        try:
            self.process.stdin.write((json.dumps({"jsonrpc": "2.0", **message}) + "\n").encode("utf-8"))
            self.process.stdin.flush()
        except (BrokenPipeError, OSError):
            raise Failure("protocol_error") from None

    def request(self, method, params):
        deadline = time.monotonic() + 10
        self.request_id += 1
        self.send({"id": self.request_id, "method": method, "params": params})
        for _ in range(100):
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise Failure("timeout")
            try:
                message = self.messages.get(timeout=remaining)
            except queue.Empty:
                raise Failure("timeout") from None
            if isinstance(message, dict) and "method" in message and "id" not in message:
                continue  # Server notification, not the requested result.
            return response_result(message, self.request_id)
        raise Failure("protocol_error")

    def call(self, name, arguments):
        check_arguments(self.tools, name, arguments)
        return self.request("tools/call", {"name": name, "arguments": arguments})

    def json(self, name, arguments):
        return tool_json(self.call(name, arguments))

    def close(self):
        self.stopping.set()
        try:
            self.process.stdin.close()  # Normal stdio shutdown is EOF, not cancellation.
        except OSError:
            pass  # A closed peer must not prevent termination/reaping below.
        try:
            self.process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            self.process.terminate()
            try:
                self.process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=3)
        self.reader.join(timeout=1)
        self.process.stdout.close()
        self.stderr.seek(0)
        stderr = self.stderr.read(1024 * 1024)
        self.stderr.close()
        return stderr


def require(condition):
    if not condition:
        raise Failure("check_failed")


def run(executable, expected):
    binary = shutil.which(executable)
    if not binary:
        raise Failure("missing_executable")
    binary = Path(binary).resolve()
    fixtures = {p.stem: p.read_bytes() for p in (ROOT / "fixtures").glob("*.yaml")}
    text = {name: data.decode("utf-8") for name, data in fixtures.items()}
    with tempfile.TemporaryDirectory(prefix="workflow-mcp-test-") as temp:
        client = Client(binary, Path(temp))
        try:
            initialized = client.request("initialize", {
                "protocolVersion": "2024-11-05", "capabilities": {},
                "clientInfo": {"name": "workflow-package-smoke", "version": "0.1.0"},
            })
            info = initialized.get("serverInfo", {})
            check_version(info, expected)
            require(initialized.get("protocolVersion") == "2024-11-05")
            client.send({"method": "notifications/initialized"})
            discovery = client.request("tools/list", {})
            require(not discovery.get("nextCursor"))  # Never treat a partial list as complete.
            tools = discovery.get("tools", [])
            client.tools = {t["name"]: t for t in tools}
            require(len(tools) == len(client.tools))
            if not REQUIRED <= client.tools.keys():
                raise Failure("missing_tool")
            modules = client.json("list_module_types", {})["module_types"]
            steps = client.json("list_step_types", {})["step_types"]
            triggers = client.json("list_trigger_types", {})["trigger_types"]
            workflows = client.json("list_workflow_types", {})["workflow_types"]
            require("http.server" in modules and "example.uninstalled-module" not in modules)
            require("step.example_uninstalled" not in {s["type"] for s in steps})
            require("example-unknown-trigger" not in triggers and "example-unknown-workflow" not in workflows)
            schema = client.json("get_module_schema", {"module_type": "http.server"})
            require(schema.get("type") == "http.server")
            skeleton = client.call("get_config_skeleton", {"module_types": ["http.server"]})
            require(not skeleton.get("isError") and "http.server" in skeleton["content"][0]["text"])
            def basic(name):
                return client.json("validate_config", {"yaml_content": text[name], "strict": True,
                                                       "skip_unknown_types": False})
            def deep(name):
                return client.json("template_validate_config", {"yaml_content": text[name], "strict": True})
            require(basic("minimal").get("valid") is True)
            require(deep("minimal").get("valid") is True)
            inspection = client.json("inspect_config", {"yaml_content": text["minimal"]})
            require(inspection.get("module_count") == 1)
            compatible = client.json("compat_check", {"yaml_content": text["minimal"]})
            require(compatible.get("compatible") is True and compatible.get("engineVersion") == expected)
            require(client.call("validate_config", {"yaml_content": text["invalid"]}).get("isError") is True)
            require(basic("missing-plugin").get("valid") is False)
            require(client.json("compat_check", {"yaml_content": text["missing-plugin"]}).get("compatible") is False)
            plugins = client.json("list_plugins", {})
            require(plugins.get("count") == 0)
            require(basic("unknown-types").get("valid") is True)
            require(deep("unknown-types").get("valid") is False)
            require(basic("unresolved-import").get("valid") is True)
            require(basic("missing-prerequisite").get("valid") is True)
            require(client.json("compat_check", {"yaml_content": text["missing-prerequisite"]}).get("compatible") is True)
        finally:
            stderr = client.close()
    if any(line != DEPRECATION for line in stderr.decode("utf-8", errors="replace").splitlines()):
        raise Failure("unexpected_stderr")
    return {
        "status": "passed", "scope": "bundled_public_fixtures_only",
        "captured_at": datetime.now(timezone.utc).isoformat(),
        "server": {"name": info["name"], "version": info["version"]},
        "release_source": f"https://github.com/GoCodeAlone/workflow/tree/{info['version']}",
        "binary_sha256": digest(binary.read_bytes()), "tool_count": len(tools),
        "tool_inventory_sha256": digest(json.dumps(tools, sort_keys=True).encode("utf-8")),
        "fixture_sha256": {name: digest(data) for name, data in sorted(fixtures.items())},
        "checks": {"basic_and_deep_minimal": "accepted", "inspection": "one_module",
                   "current_registry_compatibility": "compatible", "malformed_yaml": "tool_error",
                   "missing_module": "rejected", "default_plugin_inventory": "empty",
                   "unknown_type_coverage": "basic_accepted_deep_rejected_inventory_absent",
                   "unresolved_import": "basic_accepted_coverage_incomplete",
                   "missing_declared_plugin": "basic_and_compat_accepted_prerequisite_unmet"},
        "stderr": "known_deprecation_only" if stderr else "empty",
        "fixture_source_commit": "not_recorded_by_smoke", "redacted": False,
        "runtime_acceptance": "not_checked", "desktop_activation": "not_checked",
        "plugin_binary_health": "not_checked", "original_input_equivalence": "not_checked",
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--wfctl", default="wfctl", help="reviewed executable; never downloaded")
    parser.add_argument("--expect-version", default="v0.86.2")
    args = parser.parse_args(argv)
    try:
        report = run(args.wfctl, args.expect_version)
        status = 0
    except Exception as error:
        reason = str(error) if isinstance(error, Failure) and str(error) in REASONS else "internal_error"
        report = {"status": "failed", "reason": reason, "runtime_acceptance": "not_checked",
                  "desktop_activation": "not_checked"}
        status = 1
    print(json.dumps(report, indent=2, sort_keys=True))
    return status


if __name__ == "__main__":
    raise SystemExit(main())
