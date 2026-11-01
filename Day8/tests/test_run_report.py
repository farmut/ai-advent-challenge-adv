"""eval.run.run_cell and report tables end to end with a fake generator (no model)."""

import inspect
import json
import shutil
import unittest
from pathlib import Path
from unittest import mock

from build_data import build
from core.io import write_json, write_jsonl
from tests._mini import MINI_SIZES, mini_copy

FORMAT_OK = "Вердикт: {v}\nПринцип: {p}\nОбоснование: Так.\nРекомендация: Так."


def fake_generate(answer_fn):
    def gen(model, tok, systems, users, max_tokens, batch_size=8):
        return [{"answer": answer_fn(s, u), "prompt_tokens": len(s) // 4, "gen_tokens": 10,
                 "hit_max_tokens": False, "sec": 0.1 + len(s) / 1e4, "batch_sec": 0.8} for s, u in zip(systems, users)]
    return gen


class RunReportTest(unittest.TestCase):
    def setUp(self):
        self.root = mini_copy()
        build(self.root, MINI_SIZES)
        self.res = self.root / "results/t"
        self.res.mkdir(parents=True)

    def tearDown(self):
        shutil.rmtree(self.root)

    def _run(self, cell, answer_fn):
        from eval import run
        with mock.patch("eval.generate.generate", fake_generate(answer_fn)):
            r = run.run_cell(None, None, cell, 8, self.root)
        write_json(self.res / f"eval_{cell}.json", r)
        return r

    def test_pipeline(self):
        from common import NAME_BY_ID
        from core.io import read_jsonl
        gold = read_jsonl(self.root / "data/format/eval.jsonl")[0]
        good = FORMAT_OK.format(v=gold["verdict"], p=NAME_BY_ID[gold["principle"]])
        r = self._run("format__base__good", lambda s, u: good)
        self.assertEqual(r["metrics"]["both_acc"], 1.0)
        self.assertNotIn("user", r["rows"][0])
        self._run("format__base__weak", lambda s, u: "не знаю")
        self._run("format__best__weak", lambda s, u: good)
        self._run("format__best__good", lambda s, u: good)
        rag = self._run("facts__base__rag", lambda s, u: "13 дней, Журавлёва, итерация, crane")
        self.assertEqual(rag["metrics"]["acc_bcd"], 1.0)
        self.assertEqual(rag["metrics"]["control_refusal"], 0.0)
        self._run("facts__base__tag", lambda s, u: "Не указано.")
        self._run("facts__best__tag", lambda s, u: "13")
        self._run("ood__base__neutral", lambda s, u: "ok")
        self._run("ood__best__neutral", lambda s, u: good)
        ds = self._run("dissimilar__best__neutral", lambda s, u: good)
        self.assertEqual(ds["metrics"]["contaminated"], 1.0)
        self.assertEqual(len(r["system_sha256"]), 1)

        import report
        delta = report.delta_table(self.res)
        self.assertIn("format", delta)
        self.assertIn("+1.000", delta)   # format: Δtune on the weak prompt = 1.0
        ood = report.ood_tables(self.res)
        self.assertIn("NI", ood)
        write_json(self.res / "summary.json", {c: json.loads((self.res / f"eval_{c}.json").read_text())["metrics"]
                                               for c in ("format__base__good", "facts__base__rag")})
        self.assertIn("### facts", report.summary_tables(json.loads((self.res / "summary.json").read_text())))
        cost = report.cost_table(self.res, {"train_minutes": 1.0, "minutes": 2.0, "peak_mem_gb": 1,
                                            "trainable_params": 10})
        self.assertIn("facts__base__rag", cost)

        log = [{"type": "train", "step": s, "loss": 2 - s / 100} for s in range(5, 105, 5)]
        log += [{"type": "val", "step": s, "task": t, "loss": 1.5 - s / 200 + (s > 60) * 0.3}
                for s in range(0, 125, 25) for t in ("format", "facts", "style", "macro", "pooled")]
        write_jsonl(self.res / "log.jsonl", log)
        self.assertEqual(report.plots(self.res, {"best": 50, "S": 75}), ["loss_macro.png", "loss_tasks.png"])
        self.assertTrue((self.res / "loss_macro.png").stat().st_size > 1000)


class MlxApiTest(unittest.TestCase):
    """Guards the mlx-lm 0.32.2 API this code relies on."""

    def test_signatures(self):
        from mlx_lm import batch_generate
        from mlx_lm.generate import BatchGenerator, BatchResponse
        from mlx_lm.tuner.trainer import TrainingArgs, evaluate, train

        p = inspect.signature(batch_generate).parameters
        self.assertTrue({"prompts", "max_tokens", "return_token_ids"} <= p.keys())
        g = inspect.signature(BatchGenerator.__init__).parameters
        self.assertTrue({"completion_batch_size", "prefill_batch_size", "sampler"} <= g.keys())
        self.assertTrue({"texts", "token_ids", "stats"} <= set(BatchResponse.__dataclass_fields__))
        self.assertIn("training_callback", inspect.signature(train).parameters)
        self.assertIn("iterate_batches", inspect.signature(evaluate).parameters)
        self.assertTrue({"steps_per_save", "adapter_file", "grad_checkpoint"} <= set(TrainingArgs.__dataclass_fields__))

    def test_chat_template_tokens(self):
        model = Path(__file__).resolve().parents[1] / "models/qwen2.5-7b-instruct-4bit-bf16"
        if not (model / "tokenizer.json").exists():
            self.skipTest("base model not present")
        from mlx_lm.utils import load_tokenizer
        from eval.generate import encode
        tok = load_tokenizer(model)
        ids = encode(tok, "S", "hi")
        self.assertEqual(tok.decode(ids), "<|im_start|>system\nS<|im_end|>\n<|im_start|>user\nhi<|im_end|>\n"
                                          "<|im_start|>assistant\n")
        with self.assertRaises(ValueError):
            encode(tok, "", "hi")


if __name__ == "__main__":
    unittest.main()
