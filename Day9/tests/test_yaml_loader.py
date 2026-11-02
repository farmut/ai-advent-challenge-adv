import tempfile
import unittest
from pathlib import Path

import yaml

from core.config import (ConfigError, StrictLoader, TRAIN_SCHEMA, load_teacher_config, load_train_config, load_yaml)
from core.io import ROOT

GOOD = """model: m
train: true
data: data/distill
adapter_path: adapters/x
mask_prompt: true
seed: 42
num_layers: 16
lora_parameters: {rank: 8, scale: 20.0, dropout: 0.05, keys: [self_attn.q_proj]}
batch_size: 4
iters: 60
learning_rate: 5.0e-5
lr_schedule: {name: linear_schedule, warmup: 20, arguments: [5.0e-5, 5.0e-5, 1]}
save_every: 20
"""


def write(text):
    p = Path(tempfile.mkdtemp()) / "c.yaml"
    p.write_text(text, encoding="utf-8")
    return p


class YamlLoaderTest(unittest.TestCase):
    def test_repo_configs_load(self):
        for name in ("smoke", "student", "fallback_F1", "fallback_F2"):
            cfg = load_train_config(ROOT / f"configs/{name}.yaml")
            self.assertIsInstance(cfg["learning_rate"], float)
            self.assertTrue(cfg["mask_prompt"])
            self.assertEqual(cfg["lora_parameters"]["keys"][-1], "self_attn.o_proj")
            self.assertNotIn("max_grad_norm", cfg)
        self.assertEqual(load_train_config(ROOT / "configs/fallback_F1.yaml")["num_layers"], 28)
        self.assertEqual(load_teacher_config(ROOT / "configs/teacher.yaml")["max_retries"], 5)

    def test_yaml11_float_traps(self):
        self.assertIsInstance(load_train_config(write(GOOD))["learning_rate"], float)
        for bad in ("5e-5", "1.0e12", "'5.0e-5'"):
            with self.assertRaises(ConfigError, msg=bad):
                load_train_config(write(GOOD.replace("learning_rate: 5.0e-5", f"learning_rate: {bad}")))
        self.assertEqual(yaml.load("x: 1.0e+12", Loader=StrictLoader)["x"], 1e12)

    def test_strict_loader_ignores_global_patch(self):
        class Patched(yaml.SafeLoader):
            pass
        import re
        Patched.add_implicit_resolver("tag:yaml.org,2002:float", re.compile(r"^\d+e-\d+$"), list("0123456789"))
        self.assertIsInstance(yaml.load("x: 5e-5", Loader=Patched)["x"], float)
        self.assertIsInstance(yaml.load("x: 5e-5", Loader=StrictLoader)["x"], str)

    def test_types_unknown_missing(self):
        cases = {"iters: 60": "iters: 60.0", "mask_prompt: true": "mask_prompt: 1", "seed: 42": "seed: true",
                 "scale: 20.0": "scale: 20"}
        for good, bad in cases.items():
            with self.assertRaises(ConfigError, msg=bad):
                load_train_config(write(GOOD.replace(good, bad)))
        with self.assertRaises(ConfigError):
            load_train_config(write(GOOD + "max_grad_norm: 1.0\n"))
        with self.assertRaises(ConfigError):
            load_train_config(write(GOOD.replace("seed: 42\n", "")))
        with self.assertRaises(ConfigError):
            load_train_config(write(GOOD.replace("arguments: [5.0e-5", "arguments: [2.0e-5")))
        with self.assertRaises(ConfigError):
            load_yaml(write("- 1\n- 2\n"), TRAIN_SCHEMA)


if __name__ == "__main__":
    unittest.main()
