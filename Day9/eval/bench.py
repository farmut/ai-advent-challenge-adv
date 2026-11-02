"""Speed benchmark (plan §6.8). Separate from eval.run: quality numbers never come from here.

usage (from Day9/):
  python -m eval.bench local --model base|student --prompt good|list [--adapter ...|--fused ...]
        batch=1 mlx_lm.stream_generate, 1 warm-up + 57 eval prompts: TTFT, prompt_tps, gen_tps, total p50/p95, peak mem
  python -m eval.bench throughput --model student --prompt list     batch_generate at batch 1/4/8/16
  python -m eval.bench teacher --prompt good --reasoning off        57 cold streaming calls (--no-cache), step D
  python -m eval.bench check-fused [--n 10]                         reference greedy comparison after requantization
  python -m eval.bench check-fused-weights [--n 10]                 deterministic independent weight/tokenizer proof
Results: results/bench/<name>.json. The eval set is read for timing only (no scores are computed here).
"""

from __future__ import annotations

import argparse
import asyncio
import hashlib
import json
import statistics
import time
from collections.abc import Mapping, Sequence
from pathlib import Path
from typing import Any

from core.io import ROOT, read_json, read_jsonl, sha256_text, write_json

BATCH_SIZES = (1, 4, 8, 16)
OUT = ROOT / "results/bench"


def latency_summary(values: Sequence[float]) -> dict[str, float | None]:
    """p50 / p90 / p95 / mean / min / max of a list of seconds (inclusive quantiles)."""
    v = sorted(float(x) for x in values)
    if not v:
        return {"n": 0, "p50": None, "p90": None, "p95": None, "mean": None, "min": None, "max": None}
    if len(v) == 1:
        q = {50: v[0], 90: v[0], 95: v[0]}
    else:
        qs = statistics.quantiles(v, n=100, method="inclusive")
        q = {k: qs[k - 1] for k in (50, 90, 95)}
    return {"n": len(v), "p50": round(q[50], 4), "p90": round(q[90], 4), "p95": round(q[95], 4),
            "mean": round(sum(v) / len(v), 4), "min": round(v[0], 4), "max": round(v[-1], 4)}


def stream_one(model: Any, tok: Any, prompt: list[int], max_tokens: int) -> dict[str, Any]:
    """One batch=1 request: TTFT is the wall time to the first yielded token (prefill + first decode step)."""
    from mlx_lm import stream_generate

    t0 = time.perf_counter()
    ttft = None
    last = None
    text = []
    for resp in stream_generate(model, tok, prompt, max_tokens=max_tokens):
        if ttft is None:
            ttft = time.perf_counter() - t0
        text.append(resp.text)
        last = resp
    total = time.perf_counter() - t0
    return {"ttft_s": ttft, "total_s": total, "prompt_tokens": last.prompt_tokens if last else len(prompt),
            "prompt_tps": last.prompt_tps if last else 0.0, "gen_tokens": last.generation_tokens if last else 0,
            "generation_tps": last.generation_tps if last else 0.0, "peak_memory_gb": last.peak_memory if last else 0.0,
            "text": "".join(text)}


def bench_local(model: Any, tok: Any, systems: Sequence[str], users: Sequence[str], max_tokens: int,
                warmup: int = 1) -> dict[str, Any]:
    from eval.generate import encode

    prompts = [encode(tok, s, u) for s, u in zip(systems, users)]
    for p in prompts[:warmup]:
        stream_one(model, tok, p, max_tokens)
    rows = [stream_one(model, tok, p, max_tokens) for p in prompts]
    return {"n": len(rows), "warmup": warmup, "ttft": latency_summary([r["ttft_s"] for r in rows]),
            "total": latency_summary([r["total_s"] for r in rows]),
            "prompt_tps_median": statistics.median(r["prompt_tps"] for r in rows),
            "generation_tps_median": statistics.median(r["generation_tps"] for r in rows),
            "avg_prompt_tokens": sum(r["prompt_tokens"] for r in rows) / len(rows),
            "avg_gen_tokens": sum(r["gen_tokens"] for r in rows) / len(rows),
            "peak_memory_gb": max(r["peak_memory_gb"] for r in rows),
            "rows": [{k: v for k, v in r.items() if k != "text"} for r in rows]}


def throughput(model: Any, tok: Any, systems: Sequence[str], users: Sequence[str], max_tokens: int,
               sizes: Sequence[int] = BATCH_SIZES) -> dict[int, dict[str, float]]:
    from eval.generate import generate

    out = {}
    for b in sizes:
        t0 = time.perf_counter()
        gens, stats = generate(model, tok, systems, users, max_tokens, batch_size=b)
        wall = time.perf_counter() - t0
        out[b] = {"requests_per_s": len(gens) / wall, "gen_tokens_per_s": sum(g["gen_tokens"] for g in gens) / wall,
                  "wall_s": wall, "peak_memory_gb": max(s["peak_memory"] for s in stats)}
    return out


async def bench_teacher(client: Any, reqs: Sequence[Any]) -> dict[str, Any]:
    """Sequential cold streaming calls (no concurrency, so latency is not inflated by our own queue)."""
    rows = []
    async with client:
        for r in reqs:
            res = await client.complete(r, stream=True)
            rows.append({"ttft_s": res.ttft_s, "total_s": res.latency_s, "cost": res.cost,
                         "provider": res.provider, "gen_tokens": res.completion_tokens,
                         "reasoning_tokens": res.reasoning_tokens})
    return {"n": len(rows), "ttft": latency_summary([r["ttft_s"] for r in rows if r["ttft_s"] is not None]),
            "total": latency_summary([r["total_s"] for r in rows if r["total_s"] is not None]),
            "cost_per_request": sum(r["cost"] for r in rows) / len(rows) if rows else 0.0, "rows": rows}


def fused_equal(texts_a: Sequence[str], texts_b: Sequence[str]) -> dict[str, Any]:
    same = [a == b for a, b in zip(texts_a, texts_b)]
    return {"n": len(same), "identical": sum(same), "pass": bool(same) and all(same)}


class TensorStore:
    """Small streaming safetensors reader; BF16 is reinterpreted as raw bytes, never via NumPy."""

    def __init__(self, path: Path):
        self.path = Path(path)
        with self.path.open("rb") as f:
            n = int.from_bytes(f.read(8), "little")
            self.header = json.loads(f.read(n))
        self.data_start = 8 + n

    @property
    def keys(self) -> set[str]:
        return set(self.header) - {"__metadata__"}

    def raw(self, name: str) -> bytes:
        start, end = self.header[name]["data_offsets"]
        with self.path.open("rb") as f:
            f.seek(self.data_start + start)
            return f.read(end - start)

    def array(self, name: str) -> Any:
        import mlx.core as mx

        meta = self.header[name]
        dtypes = {"BF16": mx.bfloat16, "U32": mx.uint32, "F32": mx.float32}
        if meta["dtype"] not in dtypes:
            raise ValueError(f"unsupported dtype {meta['dtype']} for {name}")
        return mx.array(self.raw(name), dtype=mx.uint8).view(dtypes[meta["dtype"]]).reshape(meta["shape"])


def exact_tensor(a: Any, b: Any) -> bool:
    return a.shape == b.shape and a.dtype == b.dtype and bool(mx_all(a == b))


def mx_all(x: Any) -> Any:
    import mlx.core as mx

    return mx.all(x)


def expected_lora_quantized(weight: Any, scales: Any, biases: Any, lora_a: Any, lora_b: Any,
                            scale: float, group_size: int, bits: int, mode: str) -> tuple[Any, Any, Any]:
    """Independent expected tensor: dequantize base, add scale*B.T@A.T, requantize with original parameters."""
    import mlx.core as mx

    full = mx.dequantize(weight, scales, biases, group_size=group_size, bits=bits, mode=mode)
    full = full + ((scale * lora_b.T) @ lora_a.T).astype(full.dtype)
    return mx.quantize(full, group_size=group_size, bits=bits, mode=mode)


def lora_mismatches(base: Mapping[str, Any], adapter: Mapping[str, Any], fused: Mapping[str, Any], prefix: str,
                    scale: float, group_size: int, bits: int, mode: str) -> list[str]:
    expected = expected_lora_quantized(base[prefix + ".weight"], base[prefix + ".scales"], base[prefix + ".biases"],
                                       adapter[prefix + ".lora_a"], adapter[prefix + ".lora_b"],
                                       scale, group_size, bits, mode)
    bad = []
    for suffix, want in zip(("weight", "scales", "biases"), expected):
        if not exact_tensor(want, fused[prefix + "." + suffix]):
            bad.append(prefix + "." + suffix)
    if prefix + ".bias" in base and not exact_tensor(base[prefix + ".bias"], fused[prefix + ".bias"]):
        bad.append(prefix + ".bias")
    return bad


def check_fused_weights(base_dir: Path, adapter_dir: Path, fused_dir: Path, n_prompts: int = 10) -> dict[str, Any]:
    """Deterministic weight/tokenizer proof for a fused QLoRA model; no model generation and no second fuse call."""
    started = time.perf_counter()
    base_cfg = read_json(base_dir / "config.json")["quantization"]
    adapter_cfg = read_json(adapter_dir / "adapter_config.json")
    group_size, bits, mode = base_cfg["group_size"], base_cfg["bits"], base_cfg.get("mode", "affine")
    scale = float(adapter_cfg["lora_parameters"]["scale"])
    base = TensorStore(base_dir / "model.safetensors")
    adapter = TensorStore(adapter_dir / "adapters.safetensors")
    fused = TensorStore(fused_dir / "model.safetensors")
    prefixes = sorted(k[:-len(".lora_a")] for k in adapter.keys if k.endswith(".lora_a"))
    lora_tensors = {p + suffix for p in prefixes for suffix in (".weight", ".scales", ".biases")}
    mismatches: list[dict[str, str]] = []
    if base.keys != fused.keys:
        mismatches.append({"tensor": "<keyset>", "kind": "base/fused key sets differ"})
    for prefix in prefixes:
        suffixes = (".weight", ".scales", ".biases", ".bias") if prefix + ".bias" in base.keys else (".weight", ".scales", ".biases")
        layer_base = {prefix + s: base.array(prefix + s) for s in suffixes}
        layer_adapter = {prefix + s: adapter.array(prefix + s) for s in (".lora_a", ".lora_b")}
        layer_fused = {prefix + s: fused.array(prefix + s) for s in suffixes}
        for name in lora_mismatches(layer_base, layer_adapter, layer_fused, prefix, scale, group_size, bits, mode):
            mismatches.append({"tensor": name, "kind": "independent_expected_mismatch"})
    non_lora = sorted(base.keys - lora_tensors)
    for name in non_lora:
        if name in fused.keys and base.raw(name) != fused.raw(name):
            mismatches.append({"tensor": name, "kind": "non_lora_not_byte_identical"})

    from transformers import AutoTokenizer

    base_tok = AutoTokenizer.from_pretrained(str(base_dir))
    fused_tok = AutoTokenizer.from_pretrained(str(fused_dir))
    rows = read_jsonl(ROOT / "data/distill_gist/valid.jsonl")[:n_prompts]
    ids = [base_tok.apply_chat_template(r["messages"], add_generation_prompt=True, return_dict=False) for r in rows]
    fused_ids = [fused_tok.apply_chat_template(r["messages"], add_generation_prompt=True, return_dict=False) for r in rows]
    prompt_ids_equal = ids == fused_ids
    if not prompt_ids_equal:
        mismatches.append({"tensor": "<tokenizer>", "kind": "prompt_ids_differ"})
    old_path = OUT / "check_fused.json"
    return {"pass": not mismatches, "method": "independent dequantize(base)+scale*B.T@A.T then requantize; exact tensors and raw non-LoRA bytes",
            "base": str(base_dir), "adapter": str(adapter_dir), "fused": str(fused_dir),
            "quantization": {"group_size": group_size, "bits": bits, "mode": mode, "lora_scale": scale},
            "lora_layers": len(prefixes), "lora_weight_tensors_checked": 3 * len(prefixes),
            "lora_bias_tensors_checked": len(prefixes), "non_lora_tensors_byte_compared": len(non_lora),
            "prompt_ids": {"n": len(rows), "equal": prompt_ids_equal,
                           "sha256": sha256_text(json.dumps(ids, ensure_ascii=False))},
            "mismatches": mismatches, "seconds": round(time.perf_counter() - started, 3),
            "text_equality_reference": read_json(old_path) if old_path.exists() else None}


def main(argv: list[str] | None = None) -> int:
    from eval.generate import MODEL, ModelSpec, generate, load_model, unload
    from eval.registry import SETS, parse_cell, system_prompt

    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("mode", choices=("local", "throughput", "teacher", "check-fused", "check-fused-weights"))
    ap.add_argument("--model", choices=("base", "student"), default="student")
    ap.add_argument("--prompt", default="list")
    ap.add_argument("--reasoning", default="off")
    ap.add_argument("--adapter", default="adapters/student/best")
    ap.add_argument("--fused", default=None)
    ap.add_argument("--n", type=int, default=10)
    args = ap.parse_args(argv)

    def spec() -> ModelSpec:
        if args.model == "base":
            return ModelSpec("base")
        return ModelSpec("student", MODEL, adapter=None if args.fused else ROOT / args.adapter,
                         fused=ROOT / args.fused if args.fused else None)

    if args.mode == "check-fused-weights":
        res = check_fused_weights(MODEL, ROOT / args.adapter, ROOT / (args.fused or "models/student-fused"), args.n)
        write_json(OUT / "check_fused_weights.json", res)
        print({k: v for k, v in res.items() if k != "mismatches"})
        return 0 if res["pass"] else 1

    if args.mode == "check-fused":
        items = SETS["valid"].items()[:args.n]
        cell = parse_cell("valid:student:train")
        sys_ = [system_prompt(cell, it) for it in items]
        users = [it["user"] for it in items]
        texts = []
        for s in (ModelSpec("student", MODEL, adapter=ROOT / args.adapter),
                  ModelSpec("student", MODEL, fused=ROOT / (args.fused or "models/student-fused"))):
            m, tok = load_model(s)
            texts.append([g["answer"] for g in generate(m, tok, sys_, users, 400)[0]])
            unload(m)
        res = fused_equal(*texts)
        write_json(OUT / "check_fused.json", res)
        print(res)
        return 0 if res["pass"] else 1

    cell = parse_cell(f"format:{'teacher' if args.mode == 'teacher' else args.model}:{args.prompt}"
                      + (f"@{args.reasoning}" if args.mode == "teacher" else ""))
    items = SETS["format"].items()
    systems = [system_prompt(cell, it) for it in items]
    users = [it["user"] for it in items]
    name = f"{args.mode}_{cell.file_stem.removeprefix('eval_format__')}"
    if args.mode == "teacher":
        from eval.run import make_client, teacher_requests

        res: dict[str, Any] = asyncio.run(bench_teacher(make_client(use_cache=False),
                                                        teacher_requests(cell, items, "D")))
    else:
        m, tok = load_model(spec())
        if args.mode == "local":
            res = bench_local(m, tok, systems, users, cell.max_tokens())
        else:
            res = {"throughput": throughput(m, tok, systems, users, cell.max_tokens())}
        unload(m)
    res["cell"] = cell.name
    write_json(OUT / f"{name}.json", res)
    print({k: v for k, v in res.items() if k != "rows"})
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
