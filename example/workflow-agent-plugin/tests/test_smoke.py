"""Failure-contract unit tests; doubles here are not MCP integration proof."""
from contextlib import redirect_stderr, redirect_stdout
import importlib.util
import io
import json
from pathlib import Path
import queue
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest.mock import patch

PATH = Path(__file__).with_name("mcp_smoke.py")


class SmokeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        assert PATH.is_file(), "MCP smoke consumer is not implemented"
        spec = importlib.util.spec_from_file_location("mcp_smoke", PATH)
        cls.smoke = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.smoke)

    def test_unknown_and_mismatched_versions_fail(self):
        for value in (None, "", "dev", "v0.85.4", "private-value"):
            with self.subTest(value=value), self.assertRaises(self.smoke.Failure):
                self.smoke.check_version({"name": "wfctl-mcp", "version": value}, "v0.86.2")
        self.smoke.check_version({"name": "wfctl-mcp", "version": "v0.86.2"}, "v0.86.2")

    def test_wrong_server_fails(self):
        with self.assertRaises(self.smoke.Failure):
            self.smoke.check_version({"name": "other", "version": "v0.86.2"}, "v0.86.2")

    def test_missing_tool_or_argument_schema_fails(self):
        with self.assertRaises(self.smoke.Failure):
            self.smoke.check_arguments({}, "validate_config", {"yaml_content": "x"})
        tools = {"validate_config": {"inputSchema": {
            "properties": {"yaml_content": {"type": "string"}}, "required": ["yaml_content"]}}}
        for args in ({}, {"config": "x"}, {"yaml_content": "x", "target_version": "v1.0.0"}):
            with self.subTest(args=args), self.assertRaises(self.smoke.Failure):
                self.smoke.check_arguments(tools, "validate_config", args)
        self.smoke.check_arguments(tools, "validate_config", {"yaml_content": "x"})

    def test_protocol_and_tool_errors_fail_without_echo(self):
        for response in ({}, {"jsonrpc": "2.0", "id": 9, "error": {"message": "SYNTHETIC_CANARY"}},
                         {"jsonrpc": "2.0", "id": 10, "result": {}},
                         {"jsonrpc": "2.0", "id": True, "result": {}}):
            with self.subTest(response=response), self.assertRaises(self.smoke.Failure):
                self.smoke.response_result(response, 9)
        with self.assertRaises(self.smoke.Failure):
            self.smoke.tool_json({"isError": True, "content": [{"type": "text", "text": "SYNTHETIC_CANARY"}]})
        with self.assertRaises(self.smoke.Failure):
            self.smoke.tool_json({"content": [{"type": "text", "text": "bad JSON"}]})

    def test_missing_executable_is_bounded(self):
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            status = self.smoke.main(["--wfctl", "missing-public-fixture-executable"])
        self.assertEqual(status, 1)
        self.assertEqual(json.loads(out.getvalue())["reason"], "missing_executable")
        self.assertEqual(err.getvalue(), "")

    def test_raw_errors_config_and_stderr_are_never_reported(self):
        canary = "SYNTHETIC_CANARY_DO_NOT_REPORT"
        for error in (OSError(canary), ValueError(canary), self.smoke.Failure(canary)):
            out, err = io.StringIO(), io.StringIO()
            with patch.object(self.smoke, "run", side_effect=error), redirect_stdout(out), redirect_stderr(err):
                status = self.smoke.main([])
            self.assertEqual(status, 1)
            self.assertNotIn(canary, out.getvalue() + err.getvalue())
            self.assertEqual(json.loads(out.getvalue())["status"], "failed")

    def test_snapshot_digest_is_exact_bytes(self):
        self.assertNotEqual(self.smoke.digest(b"x\n"), self.smoke.digest(b"x\r\n"))
        self.assertEqual(self.smoke.digest(b"abc"),
                         "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")

    def test_client_shutdown_sends_eof_before_signals(self):
        client = self.smoke.Client.__new__(self.smoke.Client)
        client.stderr = tempfile.TemporaryFile()
        client.process = subprocess.Popen(
            [sys.executable, "-c", "import sys; sys.stdin.buffer.read()"],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=client.stderr)
        client.reader = threading.Thread(target=lambda: None)
        client.reader.start()
        self.assertEqual(client.close(), b"")
        self.assertEqual(client.process.returncode, 0)

    def test_real_child_canary_output_is_suppressed(self):
        # Dependency double only: the real smoke consumer still launches/reads it.
        # This is failure testing, not proof that wfctl behaves this way.
        canary = "SYNTHETIC_PROCESS_CANARY"
        popen = subprocess.Popen
        def child(*args, **kwargs):
            return popen([sys.executable, "-c",
                          f"import sys; print({canary!r},file=sys.stderr); print({canary!r})"], **kwargs)
        out, err = io.StringIO(), io.StringIO()
        with patch.object(self.smoke.subprocess, "Popen", side_effect=child), redirect_stdout(out), redirect_stderr(err):
            status = self.smoke.main(["--wfctl", sys.executable])
        self.assertEqual(status, 1)
        self.assertNotIn(canary, out.getvalue() + err.getvalue())
        self.assertEqual(json.loads(out.getvalue())["reason"], "protocol_error")

    def test_response_timeout_is_bounded(self):
        client = self.smoke.Client.__new__(self.smoke.Client)
        client.request_id = 0
        client.messages = queue.Queue()
        with patch.object(client, "send"), patch.object(client.messages, "get", side_effect=queue.Empty):
            with self.assertRaisesRegex(self.smoke.Failure, "^timeout$"):
                client.request("initialize", {})


if __name__ == "__main__":
    unittest.main()
