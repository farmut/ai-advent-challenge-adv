"""Run eval cells and score them (local mlx backend or the OpenRouter teacher).

usage (from Day9/):
  python -m eval.run --dry-run [--cells ...]            plan only: no model, no network, no money
  python -m eval.run --cells format:base:good --g0      G0 regression (both 0.439 ± 0.035)
  python -m eval.run --baseline                          block B baseline cells (local)
  python -m eval.run --cells format:teacher:good --reasoning off,on --step B
  python -m eval.run --required [--optional] --step D --adapter adapters/student/best
  python -m eval.run --hashes                            current hashes for results/prereg.json
Writes results/<run>/<cell file>.json (answers, per-row scores, metrics, mlx stats) and merges metrics into
results/<run>/summary.json. Existing cell files are skipped unless --force. Teacher cells need --step (budget).
"""

from __future__ import annotations

import argparse
import asyncio
import json
import statistics
import sys
import time
from collections import defaultdict
from pathlib import Path
from typing import Any

from core.hashes import check_prereg, current_hashes
from core.io import ROOT, read_json, sha256_text, write_json
from eval.registry import (BASELINE, G0_REGRESSION, OPTIONAL, REQUIRED, SETS, TEACHER_G1, Cell,
                           Item, parse_cell, system_prompt)
from eval.scorers import SCORERS

DROP = ("user", "system_train", "_idx")
USAGE_LOG = ROOT / "results/teacher_usage.jsonl"


def make_client(use_cache: bool = True) -> Any:
    from teacher.factory import make_client as real

    return real(use_cache)


def expand_reasoning(cells: list[str], reasoning: str | None) -> list[Cell]:
    """Teacher cells without an explicit '@level' get every level of --reasoning ('off,on')."""
    out: list[Cell] = []
    levels = [lv.strip() for lv in reasoning.split(",")] if reasoning else []
    for name in cells:
        c = parse_cell(name)
        if c.model == "teacher" and "@" not in name and levels:
            out += [parse_cell(f"{name}@{lv}") for lv in levels]
        else:
            out.append(c)
    return list(dict.fromkeys(out))


def p(values: list[float], q: float) -> float | None:
    if not values:
        return None
    if len(values) == 1:
        return round(values[0], 4)
    return round(statistics.quantiles(values, n=100, method="inclusive")[int(q) - 1], 4)


def score_rows(cell: Cell, items: list[Item], gens: list[dict[str, Any]]) -> tuple[list[dict[str, Any]],
                                                                                    dict[str, Any]]:
    es = SETS[cell.set]
    scorer = SCORERS[es.scorer]
    rows = [{**{k: v for k, v in it.items() if k not in DROP}, **g, **scorer.score(it, g["answer"])}
            for it, g in zip(items, gens)]
    metrics = scorer.summarize(rows)
    n = len(rows) or 1
    for k in ("sec", "prompt_tokens", "gen_tokens"):
        vals = [r[k] for r in rows if r.get(k) is not None]
        if vals:
            metrics[f"avg_{k}"] = round(sum(vals) / len(vals), 3)
    metrics["hit_max_tokens"] = round(sum(bool(r.get("hit_max_tokens")) for r in rows) / n, 4)
    return rows, metrics


def aggregate_mlx(stats: list[dict[str, float]]) -> dict[str, float]:
    """Token-weighted throughput over batches (prompt_tps = Σ prompt tokens / Σ prompt time)."""
    pt, ptime = sum(s["prompt_tokens"] for s in stats), sum(s["prompt_time"] for s in stats)
    gt, gtime = sum(s["generation_tokens"] for s in stats), sum(s["generation_time"] for s in stats)
    return {"prompt_tokens": pt, "prompt_tps": round(pt / ptime, 2) if ptime else 0.0,
            "generation_tokens": gt, "generation_tps": round(gt / gtime, 2) if gtime else 0.0,
            "peak_memory_gb": max((s["peak_memory"] for s in stats), default=0.0),
            "wall_time": round(sum(s["wall_time"] for s in stats), 3)}


def cell_header(cell: Cell, items: list[Item], systems: list[str]) -> dict[str, Any]:
    return {"cell": cell.name, "set": cell.set, "model": cell.model, "prompt": cell.prompt,
            "reasoning": cell.reasoning, "backend": cell.backend, "path": SETS[cell.set].path,
            "max_tokens": cell.max_tokens(), "decoding": "greedy",
            "system_sha256": sorted({sha256_text(s) for s in systems}), "n": len(items)}


# ---------- local backend ----------

def run_local_cell(model: Any, tok: Any, cell: Cell, batch: int, spec: Any, root: Path = ROOT,
                   limit: int | None = None) -> dict[str, Any]:
    from eval.generate import generate

    items = SETS[cell.set].items(root)[:limit]
    systems = [system_prompt(cell, it) for it in items]
    t0 = time.perf_counter()
    gens, stats = generate(model, tok, systems, [it["user"] for it in items], cell.max_tokens(), batch)
    rows, metrics = score_rows(cell, items, gens)
    metrics["wall_sec"] = round(time.perf_counter() - t0, 1)
    agg = aggregate_mlx(stats)
    metrics |= {"prompt_tps": agg["prompt_tps"], "generation_tps": agg["generation_tps"],
                "peak_memory_gb": agg["peak_memory_gb"]}
    return {**cell_header(cell, items, systems), "batch": batch, "model_spec": spec.describe(),
            "mlx_stats": {"batches": stats, "total": agg}, "metrics": metrics, "rows": rows}


# ---------- teacher backend ----------

def teacher_requests(cell: Cell, items: list[Item], step: str) -> list[Any]:
    from teacher.client import TeacherRequest

    return [TeacherRequest.chat(system_prompt(cell, it), it["user"], step=step, temperature=0.0,
                                max_tokens=cell.max_tokens(), reasoning=cell.reasoning or "off",
                                tag=f"{cell.name}/{it['id']}") for it in items]


def teacher_gens(results: list[Any]) -> list[dict[str, Any]]:
    """Answer rows from client results; the scored answer is the text without <think> (markdown is kept)."""
    from teacher.parse import parse_teacher, strip_think

    out = []
    for r in results:
        if isinstance(r, BaseException):
            out.append({"answer": "", "error": f"{type(r).__name__}: {r}", "cost": 0.0})
            continue
        answer, _ = strip_think(r.text)
        pt = parse_teacher(r.text)
        out.append({"answer": answer, "raw": r.text, "cost": r.cost, "from_cache": r.from_cache,
                    "prompt_tokens": r.prompt_tokens, "gen_tokens": r.completion_tokens,
                    "reasoning_tokens": r.reasoning_tokens, "provider": r.provider, "sec": r.latency_s,
                    "ttft_s": r.ttft_s, "response_model": r.response_model,
                    "system_fingerprint": r.system_fingerprint, "attempts": r.attempts, "finish_reason": r.finish_reason,
                    "hit_max_tokens": r.finish_reason == "length", "had_markdown": pt.had_markdown,
                    "had_think": pt.had_think, "has_cjk": pt.has_cjk})
    return out


def run_teacher_cell(client: Any, cell: Cell, step: str, root: Path = ROOT, limit: int | None = None,
                     stream: bool = False) -> dict[str, Any]:
    items = SETS[cell.set].items(root)[:limit]
    reqs = teacher_requests(cell, items, step)
    t0 = time.perf_counter()

    async def go() -> list[Any]:
        async with client:
            return await client.complete_many(reqs, stream=stream)

    gens = teacher_gens(asyncio.run(go()))
    rows, metrics = score_rows(cell, items, gens)
    lat = [g["sec"] for g in gens if g.get("sec") is not None and not g.get("from_cache")]
    fresh = [g for g in gens if "error" not in g and not g.get("from_cache")]
    ttft = [g["ttft_s"] for g in fresh if g.get("ttft_s") is not None]

    def mean(key: str) -> float | None:
        vals = [g[key] for g in gens if g.get(key) is not None]
        return round(statistics.fmean(vals), 1) if vals else None

    metrics |= {"wall_sec": round(time.perf_counter() - t0, 1), "n_errors": sum("error" in g for g in gens),
                "cost_usd": round(sum(g["cost"] for g in gens), 6),
                "from_cache": sum(bool(g.get("from_cache")) for g in gens),
                "latency_p50": p(lat, 50), "latency_p95": p(lat, 95),
                "ttft_p50": p(ttft, 50), "ttft_p95": p(ttft, 95), "prompt_tokens_mean": mean("prompt_tokens"),
                "completion_tokens_mean": mean("gen_tokens"), "reasoning_tokens_mean": mean("reasoning_tokens"),
                "reasoning_tokens": sum(g.get("reasoning_tokens") or 0 for g in gens),
                "providers": sorted({g["provider"] for g in gens if g.get("provider")})}
    return {**cell_header(cell, items, [r.messages[0]["content"] for r in reqs]), "step": step,
            "teacher_model": client.cfg.model, "provider_order": list(client.cfg.provider_order),
            "teacher_backend": client.cfg.backend, "teacher_endpoint": client.cfg.endpoint(),
            "teacher_extra_body": client.cfg.extra_body(cell.reasoning or "off"),
            "response_models": sorted({g["response_model"] for g in gens if g.get("response_model")}),
            "system_fingerprints": sorted({g["system_fingerprint"] for g in gens if g.get("system_fingerprint")}),
            "metrics": metrics, "rows": rows}


# ---------- dry run ----------

def dry_run(cells: list[Cell], args: argparse.Namespace, root: Path = ROOT) -> dict[str, Any]:
    """Everything that can be checked without a model and without the network."""
    from teacher.budget import Budget, Prices, worst_case_cost

    budget = Budget.from_usage_log(USAGE_LOG)
    plan: list[dict[str, Any]] = []
    teacher_worst = 0.0
    for c in cells:
        es = SETS[c.set]
        entry: dict[str, Any] = {"cell": c.name, "backend": c.backend, "file": f"{c.file_stem}.json",
                                 "max_tokens": c.max_tokens()}
        if not (root / es.path).exists():
            entry["status"] = f"missing {es.path}"
            plan.append(entry)
            continue
        items = es.items(root)
        systems = [system_prompt(c, it) for it in items]
        entry |= {"n": len(items), "system_sha256": sorted({sha256_text(s)[:12] for s in systems}),
                  "avg_prompt_chars": round(sum(len(s) + len(it["user"]) for s, it in zip(systems, items))
                                            / max(1, len(items)), 1)}
        if c.backend == "teacher":
            worst = sum(worst_case_cost(len(s) + len(it["user"]), c.max_tokens(), Prices())
                        for s, it in zip(systems, items))
            teacher_worst += worst
            entry["worst_case_usd"] = round(worst, 4)
        else:
            spec = model_spec(c, args)
            entry["model_spec"] = spec.describe()
            needed = [spec.fused or spec.adapter] if c.model == "student" else []
            entry["paths_exist"] = all(Path(x).exists() for x in [spec.base, *needed] if x is not None)
        entry["status"] = "ok"
        plan.append(entry)
    step = args.step or "B"
    return {"cells": plan, "prereg_mismatch": check_prereg(root), "teacher_worst_case_usd": round(teacher_worst, 4),
            "budget_spent_usd": round(budget.spent, 6), "budget_remaining_usd": round(budget.remaining(step), 6),
            "fits_budget": teacher_worst <= budget.remaining(step), "step": step}


def model_spec(c: Cell, args: argparse.Namespace) -> Any:
    from eval.generate import MODEL, ModelSpec

    base = Path(args.base_model) if args.base_model else MODEL
    if c.model == "student":
        return ModelSpec("student", base, adapter=None if args.fused else ROOT / args.adapter,
                         fused=ROOT / args.fused if args.fused else None)
    if c.model.startswith("ckpt"):
        return ModelSpec(c.model, base, run=args.ckpt_run)
    return ModelSpec("base", base)


# ---------- main ----------

def build_parser() -> argparse.ArgumentParser:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--run", default="main", help="results/<run>/")
    ap.add_argument("--cells", nargs="+", default=[])
    ap.add_argument("--baseline", action="store_true", help="block B baseline cells")
    ap.add_argument("--g1", action="store_true", help="teacher+good on eval with reasoning off and on")
    ap.add_argument("--required", action="store_true", help="block D required cells")
    ap.add_argument("--optional", action="store_true", help="block D reference cells")
    ap.add_argument("--g0", action="store_true", help="check the Day8 regression after the run")
    ap.add_argument("--reasoning", default=None, help="teacher reasoning levels, e.g. off,on (on=low)")
    ap.add_argument("--step", choices=("B", "C4", "D"), help="budget step for teacher cells")
    ap.add_argument("--adapter", default="adapters/student/best")
    ap.add_argument("--fused", default=None, help="fused student dir (instead of --adapter)")
    ap.add_argument("--base-model", default=None)
    ap.add_argument("--ckpt-run", default=None)
    ap.add_argument("--batch", type=int, default=8)
    ap.add_argument("--limit", type=int, default=None, help="debug only: first N items (not for reports)")
    ap.add_argument("--no-cache", action="store_true", help="teacher: do not read or write the cache")
    ap.add_argument("--stream", action="store_true", help="teacher: SSE streaming (TTFT)")
    ap.add_argument("--force", action="store_true")
    ap.add_argument("--allow-hash-mismatch", action="store_true")
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--hashes", action="store_true")
    return ap


def selected_cells(args: argparse.Namespace) -> list[str]:
    cells = list(args.cells)
    if args.g0:
        cells = [G0_REGRESSION["cell"], *cells]
    if args.baseline:
        cells += BASELINE
    if args.g1:
        cells += TEACHER_G1
    if args.required:
        cells += REQUIRED
    if args.optional:
        cells += OPTIONAL
    return list(dict.fromkeys(cells))


def main(argv: list[str] | None = None) -> int:
    ap = build_parser()
    args = ap.parse_args(argv)
    if args.hashes:
        print(json.dumps(current_hashes(), ensure_ascii=False, indent=1))
        return 0
    names = selected_cells(args)
    if args.dry_run and not names:
        names = list(dict.fromkeys(BASELINE + TEACHER_G1 + REQUIRED + OPTIONAL))
    if not names:
        ap.error("no cells given")
    cells = expand_reasoning(names, args.reasoning)
    if args.dry_run:
        print(json.dumps(dry_run(cells, args), ensure_ascii=False, indent=1))
        return 0

    if bad := check_prereg():
        print(f"hash mismatch with results/prereg.json: {bad}", file=sys.stderr)
        if not args.allow_hash_mismatch:
            return 1
    if any(c.backend == "teacher" for c in cells) and not args.step:
        ap.error("teacher cells need --step (budget accounting)")

    out = ROOT / "results" / args.run
    out.mkdir(parents=True, exist_ok=True)
    todo = [c for c in cells if args.force or not (out / f"{c.file_stem}.json").exists()]
    by_spec: dict[str, list[Cell]] = defaultdict(list)
    for c in todo:
        by_spec["teacher" if c.backend == "teacher" else json.dumps(model_spec(c, args).describe())].append(c)

    def save(res: dict[str, Any], c: Cell) -> None:
        write_json(out / f"{c.file_stem}.json", res)
        summary_path = out / "summary.json"
        summary = read_json(summary_path) if summary_path.exists() else {}
        summary[c.name] = res["metrics"]
        write_json(summary_path, summary)
        print(c.name, json.dumps(res["metrics"], ensure_ascii=False), flush=True)

    for key, group in sorted(by_spec.items(), key=lambda kv: kv[0] == "teacher"):
        if key == "teacher":
            for c in group:
                save(run_teacher_cell(make_client(not args.no_cache), c, args.step, limit=args.limit,
                                      stream=args.stream), c)
            continue
        from eval.generate import load_model, unload

        spec = model_spec(group[0], args)
        model, tok = load_model(spec)
        for c in group:
            save(run_local_cell(model, tok, c, args.batch, spec, limit=args.limit), c)
        unload(model)

    if args.g0:
        reg = G0_REGRESSION
        got = read_json(out / f"{parse_cell(reg['cell']).file_stem}.json")["metrics"][reg["metric"]]
        ok = abs(got - reg["expected"]) <= reg["tol"]
        print(f"G0 regression {reg['metric']}={got} expected {reg['expected']}±{reg['tol']}: "
              f"{'OK' if ok else 'FAIL'}")
        if not ok:
            return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
