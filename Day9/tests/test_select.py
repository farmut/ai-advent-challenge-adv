import math
import tempfile
import unittest
from pathlib import Path

from select_ckpt import copy_adapter, pick_best, saved_steps, select
from train_log import parse_train, parse_val

STDOUT = """Trainable parameters: 0.038%
  iter   train_loss     tok/s     tokens
     1    val 2.800    11.89s
     5    2.987 ▼      127      1.1k
    50    val 2.114    11.83s
    50    1.914 ▼      129     10.8k
  save  0000050_adapters.safetensors
   100    \x1b[32mval 1.950\x1b[0m    11.80s
   100    1.700 ▼      129     10.8k
   150    val nan    11.80s
"""


class SelectTest(unittest.TestCase):
    def test_pick_best_rules(self):
        self.assertEqual(pick_best({50: 0.5, 100: 0.6, 150: 0.55}, {}), 100)
        self.assertEqual(pick_best({50: 0.6, 100: 0.6}, {50: 1.2, 100: 1.1}), 100)    # tie -> lower loss
        self.assertEqual(pick_best({50: 0.6, 100: 0.6}, {50: 1.1, 100: 1.1}), 50)     # tie -> earlier step
        self.assertEqual(pick_best({50: 0.6, 100: 0.6}, {100: 1.1}), 100)             # missing loss = +inf
        self.assertEqual(pick_best({50: 0.6, 100: 0.6}, {50: math.nan, 100: 2.0}), 100)
        with self.assertRaises(ValueError):
            pick_best({}, {})

    def test_select_uses_runner_only(self):
        calls = []

        def runner(step):
            calls.append(step)
            return {"both_acc": {50: 0.4, 100: 0.6, 150: 0.6}[step], "principle_acc": 0.7}

        res = select("student", [50, 100, 150], runner, {100: 1.3, 150: 1.2})
        self.assertEqual(calls, [50, 100, 150])
        self.assertEqual(res["best"], 150)
        self.assertFalse(res["read_eval"])
        self.assertEqual(res["valid_set"], "data/distill/valid.jsonl")

    def test_train_log_parsing(self):
        p = Path(tempfile.mkdtemp()) / "train.stdout"
        p.write_text(STDOUT, encoding="utf-8")
        self.assertEqual([r["step"] for r in parse_train(p)], [5, 50, 100])
        v = parse_val(p)
        self.assertEqual({k: v[k] for k in (1, 50, 100)}, {1: 2.8, 50: 2.114, 100: 1.95})
        self.assertTrue(math.isnan(v[150]))

    def test_saved_steps_and_copy(self):
        root = Path(tempfile.mkdtemp())
        run = root / "adapters/student"
        run.mkdir(parents=True)
        (run / "adapter_config.json").write_text("{}")
        for s in (100, 50):
            (run / f"{s:07d}_adapters.safetensors").write_bytes(b"w%d" % s)
        self.assertEqual(saved_steps("student", root), [50, 100])
        dst = copy_adapter("student", 100, "best", False, root)
        self.assertEqual((dst / "adapters.safetensors").read_bytes(), b"w100")
        with self.assertRaises(FileExistsError):
            copy_adapter("student", 50, "best", False, root)


if __name__ == "__main__":
    unittest.main()
