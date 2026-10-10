"""Validate the SRC discovery catalog and its compatibility routes with local fixtures."""
import re
import os
import unittest
from pathlib import Path
import yaml

ROOT = Path(__file__).resolve().parents[2]

SKILLS = Path(os.environ.get("SKILLS_VALIDATION_DIR", ROOT / "skills"))

@unittest.skipUnless((SKILLS / "pentest-agent-os/SKILL.md").exists(), "runtime skills are not shipped in Git; set SKILLS_VALIDATION_DIR")
class CatalogTests(unittest.TestCase):
    def test_catalog_has_eighteen_valid_packages(self):
        source = (ROOT / "internal/skillcatalog/catalog.go").read_text()
        names = re.findall(r'"([a-z][a-z0-9-]+)"', source.split("var srcNames = []string{")[1].split("}")[0])
        self.assertEqual(len(names), 18)
        self.assertEqual(len(set(names)), 18)
        for name in names:
            text = (SKILLS / name / "SKILL.md").read_text()
            metadata = yaml.safe_load(text.split("---", 2)[1])
            self.assertEqual(metadata["name"], name)
            self.assertTrue(metadata["description"].strip())

    def test_legacy_routes_and_reference(self):
        for name in ["bug-bounty", "web-attack-methods", "unlimited-attack-scope", "burp-mcp-vuln-check"]:
            text = (SKILLS / name / "SKILL.md").read_text()
            self.assertIn("pentest-agent-os", text)
        bug = (SKILLS / "bug-bounty/SKILL.md").read_text()
        self.assertLess(len(bug), 2000)
        self.assertTrue((SKILLS / "bug-bounty/references/legacy-workflow.md").is_file())
        self.assertNotIn("install_ctf_tools.sh", (SKILLS / "solve-challenge/SKILL.md").read_text())
        self.assertNotIn("cairn-collaborative-exploration", (SKILLS / "burp-mcp-vuln-check/SKILL.md").read_text())

    def test_scope_and_negative_triggers(self):
        router = (SKILLS / "pentest-agent-os/SKILL.md").read_text()
        self.assertIn("教育", router)
        self.assertIn("企业 SRC", router)
        self.assertIn("不用于普通开发", router)
        self.assertIn("已测试", (SKILLS / "pentest-blackboard/SKILL.md").read_text())
        config = yaml.safe_load((ROOT / "config.example.yaml").read_text())
        self.assertEqual(config["multi_agent"]["eino_skills"]["catalog_profile"], "src")

if __name__ == "__main__":
    unittest.main()
