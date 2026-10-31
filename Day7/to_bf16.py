"""Copy the 4-bit MLX model, casting float16 tensors (scales, biases, norms) to bfloat16.

Qwen2.5 activations overflow float16 during LoRA training (loss -> nan); bf16 has fp32 range.
Quantized uint32 weights are untouched, so this is still the same 4-bit QLoRA base.
"""
import json
import shutil
import sys
from pathlib import Path

import mlx.core as mx
from huggingface_hub import snapshot_download

src = Path(snapshot_download("mlx-community/Qwen2.5-7B-Instruct-4bit"))
dst = Path(sys.argv[1])
dst.mkdir(parents=True, exist_ok=True)
for f in src.iterdir():
    if f.suffix == ".safetensors":
        w = mx.load(str(f))
        w = {k: v.astype(mx.bfloat16) if v.dtype == mx.float16 else v for k, v in w.items()}
        mx.save_safetensors(str(dst / f.name), w, metadata={"format": "mlx"})
    elif f.name == "config.json":
        cfg = json.loads(f.read_text())
        cfg["torch_dtype"] = "bfloat16"
        (dst / f.name).write_text(json.dumps(cfg, indent=2))
    else:
        shutil.copy(f, dst / f.name)
print(dst)
