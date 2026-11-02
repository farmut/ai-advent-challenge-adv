import contextlib
import io
import json
import socket
import sys
import unittest
from unittest import mock

import prompts
from common import PRINCIPLES, SYSTEM, SYSTEM_PROMPTED
from core.io import sha256_text
from eval import registry
from eval.registry import SETS, parse_cell, system_prompt
from eval.run import expand_reasoning, main, score_rows

# sha256 of Day8 FORMAT_GOOD / FORMAT_WEAK (results of the 0.439 regression): base+good must be byte-identical
DAY8_GOOD_SHA = sha256_text(SYSTEM_PROMPTED)


class PromptsTest(unittest.TestCase):
    def test_list_prompt(self):
        self.assertTrue(prompts.LIST_PROMPT.startswith(SYSTEM))
        for p in PRINCIPLES:
            self.assertIn(p["name"], prompts.LIST_PROMPT)
            self.assertNotIn(p["gist"][:40], prompts.LIST_PROMPT)
            self.assertIn(p["name"], prompts.LIST_PROMPT_GIST)
        self.assertLess(len(prompts.LIST_PROMPT), len(prompts.LIST_PROMPT_GIST))
        self.assertLess(len(prompts.LIST_PROMPT_GIST), len(SYSTEM_PROMPTED))
        self.assertEqual(prompts.BY_KEY["good"], SYSTEM_PROMPTED)
        self.assertEqual(set(prompts.prompt_hashes()), set(prompts.NAMES))
        self.assertEqual(prompts.prompt_hashes()["SYSTEM_PROMPTED"], DAY8_GOOD_SHA)


class RegistryTest(unittest.TestCase):
    def test_sizes(self):
        self.assertEqual(len(SETS["format"].items()), 57)
        self.assertEqual(len(SETS["valid_orig"].items()), 30)
        self.assertEqual(len(SETS["ood"].items()), 100)
        self.assertEqual(len(SETS["dissimilar"].items()), 32)
        self.assertEqual(sum(1 for i in SETS["format"].items() if i["gold_verdict"] == "Нарушение"), 30)

    def test_matrix(self):
        for m in ("base", "student", "teacher"):
            for p in ("list", "good", "short"):
                for s in ("format", "ood", "dissimilar"):
                    c = parse_cell(f"{s}:{m}:{p}")
                    self.assertEqual(c.backend, "teacher" if m == "teacher" else "local")
        self.assertEqual(parse_cell("format:teacher:good").reasoning, "off")
        self.assertEqual(parse_cell("format:teacher:good@on").reasoning, registry.REASONING_ON)
        self.assertEqual(parse_cell("format:teacher:good@low").file_stem, "eval_format__teacher__good__r-low")
        self.assertEqual(parse_cell("valid:ckpt150:train").file_stem, "eval_valid__ckpt150__train")
        for bad in ("format:base", "format:base:train", "nope:base:good", "format:big:good", "format:base:good@low",
                    "format:teacher:good@max"):
            with self.assertRaises(ValueError, msg=bad):
                parse_cell(bad)

    def test_system_prompts(self):
        it = SETS["valid_orig"].items()[0]
        self.assertEqual(system_prompt(parse_cell("format:base:list")), prompts.LIST_PROMPT)
        self.assertEqual(system_prompt(parse_cell("ood:student:neutral")), prompts.NEUTRAL)
        self.assertEqual(system_prompt(parse_cell("valid:base:train"), it), it["system_train"])
        with self.assertRaises(ValueError):
            system_prompt(parse_cell("valid:base:train"), None)

    def test_expand_reasoning(self):
        cells = expand_reasoning(["format:teacher:good", "format:base:good", "format:teacher:list@off"], "off,on")
        self.assertEqual([c.name for c in cells], ["format:teacher:good@off", "format:teacher:good@low",
                                                  "format:base:good", "format:teacher:list@off"])

    def test_score_rows(self):
        c = parse_cell("format:base:good")
        items = SETS["format"].items()[:2]
        ans = (f"Вердикт: {items[0]['gold_verdict']}\nПринцип: "
               f"{next(p['name'] for p in PRINCIPLES if p['id'] == items[0]['gold_principle'])}\nОбоснование: а\n"
               "Рекомендация: б")
        rows, m = score_rows(c, items, [{"answer": ans, "sec": 1.0, "prompt_tokens": 10, "gen_tokens": 5},
                                        {"answer": "нет", "sec": 3.0, "prompt_tokens": 10, "gen_tokens": 5}])
        self.assertEqual((m["both_acc"], m["avg_sec"]), (0.5, 2.0))
        self.assertNotIn("user", rows[0])


class DryRunTest(unittest.TestCase):
    def test_dry_run_without_model_and_network(self):
        def no_net(*a, **kw):
            raise AssertionError("network used in dry run")

        out = io.StringIO()
        with mock.patch.object(socket, "socket", no_net), mock.patch.dict(sys.modules, {"mlx": None, "mlx_lm": None}), \
                contextlib.redirect_stdout(out):
            rc = main(["--dry-run"])
        self.assertEqual(rc, 0)
        res = json.loads(out.getvalue())
        names = [c["cell"] for c in res["cells"]]
        self.assertIn("format:student:list", names)
        self.assertIn("format:teacher:good@low", names)
        self.assertGreater(res["teacher_worst_case_usd"], 0)
        good = next(c for c in res["cells"] if c["cell"] == "format:base:good")
        self.assertEqual(good["system_sha256"], [DAY8_GOOD_SHA[:12]])
        self.assertEqual(good["n"], 57)


if __name__ == "__main__":
    unittest.main()
