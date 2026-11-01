import json
import shutil
import unittest

from build_data import DataError, build, facts_strata_a
from common import SYSTEM_PROMPTED
from core.io import read_jsonl
from core.prompts import TAG_STYLE, tag_facts_for
from tests._mini import MINI_SIZES, mini_copy


class BuildDataTest(unittest.TestCase):
    def setUp(self):
        self.root = mini_copy()

    def tearDown(self):
        shutil.rmtree(self.root)

    def test_build_ok(self):
        counts = build(self.root, MINI_SIZES)
        self.assertEqual(counts["mix/train"], 2 + 10 + 2)
        self.assertEqual(counts["facts_eval"], 5 + 2)
        self.assertEqual(counts["mix/valid"], 1 + 2 + 1)
        mix = read_jsonl(self.root / "data/mix/train.jsonl")
        systems = {r["task"]: {r["messages"][0]["content"] for r in mix if r["task"] == t}
                   for t in ("format", "facts", "style") for r in mix if r["task"] == t}
        self.assertEqual(systems["format"], {SYSTEM_PROMPTED})
        self.assertEqual(systems["facts"], {tag_facts_for(self.root)})
        self.assertEqual(systems["style"], {TAG_STYLE})
        self.assertIn("Тестовая Контора", tag_facts_for(self.root))
        # shuffled with a fixed seed: rebuilding gives the same order
        first = (self.root / "data/mix/train.jsonl").read_text()
        build(self.root, MINI_SIZES)
        self.assertEqual(first, (self.root / "data/mix/train.jsonl").read_text())
        self.assertNotEqual([r["task"] for r in mix], sorted(r["task"] for r in mix))

    def test_strata_a(self):
        train = read_jsonl(self.root / "data/facts/train.jsonl")
        a = facts_strata_a(train)
        self.assertEqual([r["id"] for r in a], ["FE-a-F01", "FE-a-F02"])
        self.assertEqual(a, facts_strata_a(list(reversed(train))))  # independent of input order
        by_id = {r["id"]: r for r in train}
        for r in a:
            self.assertEqual(r["question"], by_id[r["source_id"]]["question"])
            self.assertEqual(r["strata"], "a")

    def test_size_mismatch_fails(self):
        p = self.root / "data/facts/eval_gen.jsonl"
        p.write_text("\n".join(p.read_text().splitlines()[:-1]) + "\n")
        with self.assertRaises(DataError) as e:
            build(self.root, MINI_SIZES)
        self.assertTrue(any("strata" in m for m in e.exception.args[0]))

    def test_control_fact_in_train_fails(self):
        p = self.root / "data/facts/train.jsonl"
        rows = read_jsonl(p)
        rows[0]["fact_id"] = "F03"
        p.write_text("".join(json.dumps(r, ensure_ascii=False) + "\n" for r in rows))
        with self.assertRaises(DataError):
            build(self.root, MINI_SIZES)

    def test_style_proxy_failure_fails(self):
        p = self.root / "data/style/train.jsonl"
        rows = read_jsonl(p)
        rows[0]["answer"] = "Конечно! " + rows[0]["answer"]
        p.write_text("".join(json.dumps(r, ensure_ascii=False) + "\n" for r in rows))
        with self.assertRaises(DataError) as e:
            build(self.root, MINI_SIZES)
        self.assertTrue(any("style proxies" in m for m in e.exception.args[0]))

    def test_missing_file_fails(self):
        (self.root / "data/ood/eval.jsonl").unlink()
        with self.assertRaises(DataError):
            build(self.root, MINI_SIZES)


if __name__ == "__main__":
    unittest.main()
