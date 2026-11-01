import shutil
import unittest

from build_data import build
from core import prompts
from eval.registry import OPTIONAL, REQUIRED, SETS, parse_cell, system_prompt
from tests._mini import MINI_SIZES, mini_copy


class PromptsTest(unittest.TestCase):
    def setUp(self):
        self.root = mini_copy()

    def tearDown(self):
        shutil.rmtree(self.root)

    def test_prompts(self):
        self.assertEqual(prompts.NEUTRAL, "You are Qwen, created by Alibaba Cloud. You are a helpful assistant.")
        rag = prompts.facts_rag_for(self.root)
        self.assertTrue(rag.startswith(prompts.tag_facts_for(self.root) + "\n\n# Регламент"))
        good = prompts.style_good_for(self.root)
        self.assertTrue(good.startswith(prompts.TAG_STYLE))
        self.assertEqual(prompts.style_shot_ids(self.root), ["ST-001", "ST-002"])
        self.assertEqual(good.count("Вопрос: "), 2)
        self.assertEqual(len(prompts.all_prompts(self.root)), 7)

    def test_lazy_constants_on_real_root(self):
        self.assertIn("Режим: регламент.", prompts.TAG_FACTS)
        self.assertTrue(prompts.FACTS_RAG.startswith(prompts.TAG_FACTS))

    def test_cells(self):
        self.assertEqual(len(REQUIRED), 21)
        self.assertEqual(len(set(REQUIRED + OPTIONAL)), len(REQUIRED + OPTIONAL))
        self.assertEqual(parse_cell("facts__ckpt125__rag"), ("facts", "ckpt125", "rag"))
        for bad in ("facts__best__good", "nope__base__tag", "facts__tuned__tag"):
            with self.assertRaises(ValueError):
                parse_cell(bad)
        self.assertEqual({SETS[s].max_tokens for s in ("format", "facts", "style", "ood", "dissimilar")},
                         {400, 160, 300, 512})

    def test_items_and_train_prompt(self):
        build(self.root, MINI_SIZES)
        vf = SETS["valid_format"].items(self.root)
        self.assertEqual(vf[0]["gold_verdict"] is not None, True)
        self.assertEqual(system_prompt("valid_format", "train", vf[0]), prompts.FORMAT_GOOD)
        facts = SETS["facts"].items(self.root)
        c = next(i for i in facts if i["strata"] == "c")
        self.assertEqual(c["cluster"], "F01+F02")
        self.assertEqual(system_prompt("facts", "neutral"), prompts.NEUTRAL)
        with self.assertRaises(ValueError):
            system_prompt("facts", "train", facts[0])


if __name__ == "__main__":
    unittest.main()
