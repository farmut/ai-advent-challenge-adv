import json
import shutil
import unittest

from core.io import read_jsonl, write_jsonl
from gates import _count_leaks, gate_g2, gate_g3
from tests._mini import mini_copy


class GatesTest(unittest.TestCase):
    def setUp(self):
        self.root = mini_copy()

    def tearDown(self):
        shutil.rmtree(self.root)

    def test_g2_mini(self):
        r = gate_g2(self.root)["checks"]
        self.assertTrue(r["templates_disjoint"]["pass"])
        # FE-d-001 asks "что длится 13 дней" with key "итерац": no key in the question
        self.assertTrue(r["key_not_in_question"]["pass"])
        self.assertIsNone(r["audit"]["pass"])
        self.assertTrue(r["style_shots_not_in_eval"]["pass"])

    def test_g2_catches_leaks(self):
        p = self.root / "data/facts/eval_gen.jsonl"
        rows = read_jsonl(p)
        rows[0]["question"] = "Сколько рабочих дней длится итерация? (вариант 1)"  # copy of a train question
        rows[1]["question"] = "Is it Crane who owns the backlog?"                   # key inside the question
        rows[2]["template_id"] = "T1"
        write_jsonl(p, rows)
        (self.root / "data/audit.json").write_text(json.dumps([{"id": "x", "verdict": "LEAK"}]))
        g = gate_g2(self.root)
        c = g["checks"]
        self.assertFalse(g["pass"])
        self.assertEqual([v["id"] for v in c["facts_jaccard"]["violations"]], ["FE-b-001"])
        self.assertEqual(c["key_not_in_question"]["violations"], ["FE-b-002"])
        self.assertEqual(c["templates_disjoint"]["overlap"], ["T1"])
        self.assertEqual(c["audit"]["leaks"], 1)

    def test_count_leaks(self):
        self.assertEqual(_count_leaks({"a": ["LEAK", {"b": "ok"}, "leak "]}), 2)

    def test_g3(self):
        rules = ("vy", "no_exclaim", "no_intro", "no_hedge", "short_sentences", "length_plain")
        samples = [{"id": f"G3-{i:02d}", "proxies": {r: True for r in rules}} for i in range(30)]
        write_jsonl(self.root / "results/g3_samples.jsonl", samples)
        labels = [{"id": s["id"], "labels": {r: not (r == "vy" and i < 4) for r in rules}}
                  for i, s in enumerate(samples)]
        write_jsonl(self.root / "labels.jsonl", labels)
        g = gate_g3(self.root / "labels.jsonl", self.root)
        self.assertFalse(g["pass"])
        self.assertEqual(g["rules"]["vy"]["agree"], 26)
        self.assertTrue(g["rules"]["no_hedge"]["pass"])


if __name__ == "__main__":
    unittest.main()
