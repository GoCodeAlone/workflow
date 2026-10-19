"""Package contracts; does not prove desktop skill activation."""
import json
from pathlib import Path
import re
import unittest

from jsonschema import Draft202012Validator

ROOT = Path(__file__).resolve().parents[1]
SKILLS = {"workflow-configure", "workflow-diagnose", "workflow-readiness"}


class PackageTests(unittest.TestCase):
    def test_official_manifest_schemas(self):
        for name in ("plugin", "mcp"):
            with self.subTest(name=name):
                schema = json.loads((ROOT / "tests/schemas" / f"{name}.schema.json").read_text())
                Draft202012Validator.check_schema(schema)
                Draft202012Validator(schema).validate(json.loads((ROOT / f"{name}.json").read_text()))

    def test_direct_local_transport(self):
        plugin = json.loads((ROOT / "plugin.json").read_text())
        self.assertEqual(plugin["name"], "workflow-assistant")
        self.assertTrue(plugin["extensions"]["com.openai"]["requires_local_executor"])
        servers = json.loads((ROOT / "mcp.json").read_text())["mcpServers"]
        self.assertEqual(set(servers), {"workflow-config"})
        self.assertEqual(servers["workflow-config"], {
            "type": "stdio", "command": "wfctl",
            "args": ["mcp", "-plugin-dir", "${PLUGIN_DATA}/engine-plugins"],
            "cwd": "${PLUGIN_ROOT}", "env": {"WFCTL_NO_UPDATE_CHECK": "1"},
        })

    def test_skill_discovery_and_shared_contract(self):
        self.assertEqual({p.parent.name for p in (ROOT / "skills").glob("*/SKILL.md")}, SKILLS)
        for name in SKILLS:
            body = (ROOT / "skills" / name / "SKILL.md").read_text()
            front = body.split("---", 2)[1]
            self.assertIn(f"name: {name}\n", front)
            self.assertIn("description: Use when", front)
            self.assertLess(len(front), 1024)
            self.assertIn("../../references/workflow-contract.md", body)

    def test_public_portable_files_and_links(self):
        self.assertTrue((ROOT / "LICENSE").exists())
        forbidden = ["/Users/", "/home/", "BEGIN PRIVATE KEY", "ghp_", "sk-live-", "app://"]
        for path in ROOT.rglob("*"):
            if not path.is_file() or "__pycache__" in path.parts or path == Path(__file__).resolve():
                continue
            self.assertFalse(path.is_symlink(), path)
            if path.suffix not in {".json", ".md", ".yaml", ".txt"}:
                continue
            body = path.read_text()
            for token in forbidden:
                self.assertNotIn(token, body, str(path.relative_to(ROOT)))
            if path.suffix == ".md":
                for target in re.findall(r"\]\(([^)]+)\)", body):
                    if "://" in target or target.startswith("#"):
                        continue
                    dest = (path.parent / target.split("#")[0]).resolve()
                    self.assertTrue(dest.is_relative_to(ROOT), target)
                    self.assertTrue(dest.exists(), target)
        for name in ("hooks", ".app.json", ".mcp.json", ".codex-plugin"):
            self.assertFalse((ROOT / name).exists(), name)


if __name__ == "__main__":
    unittest.main()
