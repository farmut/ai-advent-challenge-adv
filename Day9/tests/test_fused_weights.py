import tempfile
import unittest
from pathlib import Path

import mlx.core as mx

from eval.bench import TensorStore, exact_tensor, expected_lora_quantized, lora_mismatches


class FusedWeightsTest(unittest.TestCase):
    def layer(self):
        weight = (mx.arange(4 * 64, dtype=mx.float32).reshape(4, 64) / 256) - 0.5
        qw, scales, biases = mx.quantize(weight, group_size=64, bits=4, mode="affine")
        base = {"p.weight": qw, "p.scales": scales, "p.biases": biases,
                "p.bias": mx.arange(4, dtype=mx.float32)}
        adapter = {"p.lora_a": mx.arange(64 * 2, dtype=mx.float32).reshape(64, 2) / 1000,
                   "p.lora_b": mx.arange(2 * 4, dtype=mx.float32).reshape(2, 4) / 1000}
        return base, adapter

    def test_expected_quantized_layer_passes_and_corruption_fails(self):
        base, adapter = self.layer()
        expected = expected_lora_quantized(
            base["p.weight"], base["p.scales"], base["p.biases"], adapter["p.lora_a"], adapter["p.lora_b"],
            20.0, 64, 4, "affine")
        fused = {name: tensor for name, tensor in zip(("p.weight", "p.scales", "p.biases"), expected)}
        fused["p.bias"] = base["p.bias"]
        self.assertEqual(lora_mismatches(base, adapter, fused, "p", 20.0, 64, 4, "affine"), [])
        corrupted = dict(fused)
        corrupted["p.weight"] = corrupted["p.weight"] + 1
        self.assertEqual(lora_mismatches(base, adapter, corrupted, "p", 20.0, 64, 4, "affine"), ["p.weight"])
        corrupted = dict(fused)
        corrupted["p.scales"] = corrupted["p.scales"] + 0.01
        self.assertEqual(lora_mismatches(base, adapter, corrupted, "p", 20.0, 64, 4, "affine"), ["p.scales"])

    def test_streaming_safetensors_preserves_bf16(self):
        path = Path(tempfile.mkdtemp()) / "x.safetensors"
        x = mx.array([0.0, 1.0, -2.0], dtype=mx.bfloat16)
        mx.save_safetensors(str(path), {"x": x})
        store = TensorStore(path)
        self.assertTrue(exact_tensor(store.array("x"), x))
        self.assertEqual(store.raw("x"), path.read_bytes()[-6:])


if __name__ == "__main__":
    unittest.main()
