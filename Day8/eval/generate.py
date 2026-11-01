"""The single generation path used by eval.run, gates.py and select_ckpt.py.

Greedy decoding with mlx_lm.batch_generate (mlx-lm 0.32.2): prompts are token-id lists, the default sampler of
BatchGenerator is argmax, results come back in input order as BatchResponse.texts / .token_ids.
"""

from __future__ import annotations

import gc
import time
from collections.abc import Sequence
from pathlib import Path
from typing import Any

from core.io import ROOT

MODEL = ROOT / "models/qwen2.5-7b-instruct-4bit-bf16"
BATCH = 8


def adapter_dir(run: str, model: str) -> Path:
    return ROOT / "adapters" / run / model


def checkpoint_file(run: str, step: int) -> Path:
    return ROOT / "adapters" / run / f"{step:07d}_adapters.safetensors"


def load_model(model: str, run: str | None = None) -> tuple[Any, Any]:
    """base | best | overfit | ckpt<step>. Checkpoints: load(run dir) builds the LoRA layers, then load_weights."""
    from mlx_lm import load

    if model == "base":
        m, tok = load(str(MODEL))
    elif model in ("best", "overfit"):
        if run is None:
            raise ValueError("run id is required for tuned models")
        m, tok = load(str(MODEL), adapter_path=str(adapter_dir(run, model)))
    elif model.startswith("ckpt"):
        if run is None:
            raise ValueError("run id is required for checkpoints")
        m, tok = load(str(MODEL), adapter_path=str(ROOT / "adapters" / run))
        set_checkpoint(m, checkpoint_file(run, int(model[4:])))
    else:
        raise ValueError(f"unknown model {model!r}")
    m.eval()
    return m, tok


def set_checkpoint(model: Any, path: Path) -> None:
    """Swap LoRA weights in place (same structure as the run's adapter_config.json)."""
    if not path.exists():
        raise FileNotFoundError(path)
    model.load_weights(str(path), strict=False)
    model.eval()


def unload(model: Any) -> None:
    import mlx.core as mx

    del model
    gc.collect()
    mx.clear_cache()


def encode(tok: Any, system: str, user: str) -> list[int]:
    """Chat-template token ids with an explicit system message and the generation prompt."""
    if not system:
        raise ValueError("system prompt must be explicit (Qwen inserts its default otherwise)")
    msgs = [{"role": "system", "content": system}, {"role": "user", "content": user}]
    return list(tok.apply_chat_template(msgs, add_generation_prompt=True, return_dict=False))


def generate(model: Any, tok: Any, systems: Sequence[str], users: Sequence[str], max_tokens: int,
             batch_size: int = BATCH) -> list[dict[str, Any]]:
    """Greedy answers in input order: text, prompt_tokens, gen_tokens, sec (batch wall time / batch size)."""
    from mlx_lm import batch_generate

    if len(systems) != len(users):
        raise ValueError("systems and users differ in length")
    prompts = [encode(tok, s, u) for s, u in zip(systems, users)]
    out: list[dict[str, Any]] = []
    for i in range(0, len(prompts), batch_size):
        chunk = prompts[i:i + batch_size]
        t0 = time.perf_counter()
        resp = batch_generate(model, tok, chunk, max_tokens=max_tokens, return_token_ids=True,
                              completion_batch_size=batch_size, prefill_batch_size=batch_size)
        sec = time.perf_counter() - t0
        for p, text, ids in zip(chunk, resp.texts, resp.token_ids):
            out.append({"answer": text.strip(), "prompt_tokens": len(p), "gen_tokens": len(ids),
                        "hit_max_tokens": len(ids) >= max_tokens, "sec": round(sec / len(chunk), 3),
                        "batch_sec": round(sec, 3)})
    return out
