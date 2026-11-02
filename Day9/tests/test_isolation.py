"""The eval set is read only by eval.run / report.py (and leaks.py / eval.bench, which never select anything).
Generator, labelling/filter, checkpoint selection and τ selection must not open it (plan §0)."""

import re
import unittest

from core.io import ROOT
from tests._helpers import record_opens

EVAL = str((ROOT / "data/format/eval.jsonl").resolve())
NO_EVAL_MODULES = ("gen/gen_situations.py", "gen/make_cells.py", "distill.py", "select_ckpt.py", "eval/cascade.py",
                   "eval/confidence.py", "train_log.py", "teacher/client.py", "teacher/parse.py")
EVAL_REF = re.compile(r"format/eval|eval\.jsonl|SETS\[\"format\"\]|\"format:")


class StaticIsolationTest(unittest.TestCase):
    def test_sources_do_not_reference_eval(self):
        for rel in NO_EVAL_MODULES:
            src = (ROOT / rel).read_text(encoding="utf-8")
            self.assertIsNone(EVAL_REF.search(src), rel)

    def test_generator_inputs_only_book_principles_cells(self):
        src = (ROOT / "gen/gen_situations.py").read_text(encoding="utf-8")
        paths = set(re.findall(r'ROOT / "([^"]+)"', src))
        self.assertEqual(paths, {"gen/GEN_SITUATIONS.md", "book/pages.jsonl", "data/distill/cells.jsonl",
                                 "data/distill/situations_raw.jsonl", "data/distill/situations_rejected.jsonl"})


class RuntimeIsolationTest(unittest.TestCase):
    def test_generator_prompt_build(self):
        from gen import gen_situations as g
        from gen.make_cells import make_cells

        g.templates.cache_clear()
        g.book_pages.cache_clear()
        with record_opens() as seen:
            cells = make_cells()
            msgs = g.build_messages(g.calls(cells)[0], 0)
        self.assertNotIn(EVAL, seen)
        allowed = {str((ROOT / p).resolve()) for p in ("gen/GEN_SITUATIONS.md", "book/pages.jsonl")}
        self.assertTrue(set(seen) <= allowed, set(seen) - allowed)
        self.assertIn("Практика:", msgs[1]["content"])

    def test_distill_build_and_gold(self):
        import distill

        with record_opens() as seen:
            rows = distill.gold_rows()
            distill.build([{**r, "answer": None, "decision": {"reason": "x"}} for r in rows], [], set())
        self.assertNotIn(EVAL, seen)

    def test_select_and_tau(self):
        import select_ckpt
        from eval import cascade

        with record_opens() as seen:
            res = select_ckpt.select("r", [50, 100], lambda s: {"both_acc": 0.5}, {50: 1.0, 100: 0.9})
            cascade.choose_tau([{"conf": 0.9, "format_ok": True, "student_correct": True, "teacher_correct": True}])
        self.assertEqual(seen, [])
        self.assertFalse(res["read_eval"])
        self.assertEqual(res["best"], 100)


if __name__ == "__main__":
    unittest.main()
