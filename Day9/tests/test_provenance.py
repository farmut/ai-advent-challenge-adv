import hashlib
import re
import shutil
import subprocess
import unittest

from core.io import ROOT, read_json, sha256_file

PROV = read_json(ROOT / "PROVENANCE.json")
HAS_GIT = shutil.which("git") is not None


class ProvenanceTest(unittest.TestCase):
    def test_destinations_match(self):
        self.assertEqual(PROV["commit"][:7], "5db71bd")
        for e in PROV["files"]:
            self.assertEqual(sha256_file(ROOT / e["dst"]), e["sha256_dst"], e["dst"])
            if not e["modified"]:
                self.assertEqual(e["sha256_src"], e["sha256_dst"], e["dst"])
            else:
                self.assertTrue(e["note"], e["dst"])

    @unittest.skipUnless(HAS_GIT, "git not available")
    def test_sources_match_commit(self):
        for e in PROV["files"]:
            blob = subprocess.run(["git", "show", f"{e['commit']}:{e['src']}"], cwd=ROOT, capture_output=True,
                                  check=True).stdout
            self.assertEqual(hashlib.sha256(blob).hexdigest(), e["sha256_src"], e["src"])

    def test_data_sizes(self):
        sizes = {"data/format/train.jsonl": 249, "data/format/valid.jsonl": 30, "data/format/eval.jsonl": 57,
                 "data/ood/eval.jsonl": 100, "data/dissimilar/eval.jsonl": 32}
        for rel, n in sizes.items():
            with (ROOT / rel).open(encoding="utf-8") as f:
                self.assertEqual(sum(1 for ln in f if ln.strip()), n, rel)

    def test_no_day8_imports(self):
        """Day8 code is copied, never imported: no sys.path tricks, no `import Day8...`."""
        for p in ROOT.rglob("*.py"):
            if ".venv" in p.parts or p.name == "test_provenance.py":
                continue
            src = p.read_text(encoding="utf-8")
            self.assertIsNone(re.search(r"sys\.path\.(insert|append)", src), str(p))
            self.assertIsNone(re.search(r"^\s*(from|import)\s+Day8", src, re.M), str(p))

if __name__ == "__main__":
    unittest.main()
