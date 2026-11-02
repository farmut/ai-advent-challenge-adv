"""Local generation path (mlx_lm 0.32.2) used by eval.run, select_ckpt.py and gates.py.

Day8 copy, modified for Day9: the base model path and the adapter / fused model are parameters (not fixed to
Day8/models), the student is "student" (adapter dir or fused model dir), and BatchResponse.stats
(prompt_tps, generation_tps, peak_memory) are kept instead of being thrown away.

Greedy decoding with mlx_lm.batch_generate: prompts are token-id lists, the default sampler of BatchGenerator is
argmax, results come back in input order as BatchResponse.texts / .token_ids.
"""

from __future__ import annotations

import gc
import time
from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from core.io import ROOT

# bf16 copy of mlx-community/Qwen2.5-7B-Instruct-4bit made by Day8/to_bf16.py; sha256 of model.safetensors in G0.
MODEL = (ROOT / "../Day8/models/qwen2.5-7b-instruct-4bit-bf16").resolve()
BASE_SHA256 = "281d68db1c0d537bf1e19b0744ff2c3dc26e7c86ca5b7deae51935266c97ac58"
BATCH = 8


@dataclass(frozen=True)
class ModelSpec:
    """What to load. key: base | student | ckpt<step>. For student either adapter or fused is set."""

    key: str
    base: Path = MODEL
    adapter: Path | None = None
    fused: Path | None = None
    run: str | None = None

    def describe(self) -> dict[str, Any]:
        return {"key": self.key, "base": str(self.base), "adapter": str(self.adapter) if self.adapter else None,
                "fused": str(self.fused) if self.fused else None, "run": self.run}


def checkpoint_file(run: str, step: int) -> Path:
    return ROOT / "adapters" / run / f"{step:07d}_adapters.safetensors"


def load_model(spec: ModelSpec) -> tuple[Any, Any]:
    """Load base, base+adapter, a fused student, or a run checkpoint (LoRA layers from the run dir + weights)."""
    from mlx_lm import load

    if spec.key == "base":
        m, tok = load(str(spec.base))
    elif spec.key == "student":
        if spec.fused is not None:
            m, tok = load(str(spec.fused))
        elif spec.adapter is not None:
            m, tok = load(str(spec.base), adapter_path=str(spec.adapter))
        else:
            raise ValueError("student needs an adapter dir or a fused model dir")
    elif spec.key.startswith("ckpt"):
        if spec.run is None:
            raise ValueError("run id is required for checkpoints")
        m, tok = load(str(spec.base), adapter_path=str(ROOT / "adapters" / spec.run))
        set_checkpoint(m, checkpoint_file(spec.run, int(spec.key[4:])))
    else:
        raise ValueError(f"unknown local model {spec.key!r}")
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


def batch_stats(stats: Any) -> dict[str, float]:
    """The fields of mlx_lm BatchStats we keep (properties are read explicitly; missing ones become 0)."""
    names = ("prompt_tokens", "prompt_time", "prompt_tps", "generation_tokens", "generation_time",
             "generation_tps", "decode_tps", "wall_time", "peak_memory")
    return {n: round(float(getattr(stats, n, 0.0) or 0.0), 4) for n in names}


def generate(model: Any, tok: Any, systems: Sequence[str], users: Sequence[str], max_tokens: int,
             batch_size: int = BATCH) -> tuple[list[dict[str, Any]], list[dict[str, float]]]:
    """Greedy answers in input order and per-batch mlx stats.

    Row: answer, prompt_tokens, gen_tokens, hit_max_tokens, sec (batch wall time / batch size), batch_sec, batch.
    """
    from mlx_lm import batch_generate

    if len(systems) != len(users):
        raise ValueError("systems and users differ in length")
    prompts = [encode(tok, s, u) for s, u in zip(systems, users)]
    out: list[dict[str, Any]] = []
    stats: list[dict[str, float]] = []
    for b, i in enumerate(range(0, len(prompts), batch_size)):
        chunk = prompts[i:i + batch_size]
        t0 = time.perf_counter()
        resp = batch_generate(model, tok, chunk, max_tokens=max_tokens, return_token_ids=True,
                              completion_batch_size=batch_size, prefill_batch_size=batch_size)
        sec = time.perf_counter() - t0
        stats.append({"batch": b, "size": len(chunk), "sec": round(sec, 3), **batch_stats(resp.stats)})
        for p, text, ids in zip(chunk, resp.texts, resp.token_ids):
            out.append({"answer": text.strip(), "prompt_tokens": len(p), "gen_tokens": len(ids),
                        "hit_max_tokens": len(ids) >= max_tokens, "sec": round(sec / len(chunk), 3),
                        "batch_sec": round(sec, 3), "batch": b})
    return out, stats
