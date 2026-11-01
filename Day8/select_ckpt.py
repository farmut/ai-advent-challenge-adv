"""Checkpoint selection on valid only (plan step 7) -> results/<run>/select_ckpt.json, adapters/<run>/{best,overfit}/.

usage: python select_ckpt.py --run main [--no-gen] [--force]
best:    among the K=3 saved checkpoints with the lowest macro val loss, the one with the highest generation score
         on valid = mean(both_acc format, acc facts, style_all style); ties -> lower macro loss, then earlier step.
         --no-gen: the lowest macro loss.
S:       first eval i with macro(i) and macro(i+1) >= min(macro[..i]) + 0.05 while the train-loss EMA at i+1 is
         below the EMA at the valid minimum (train still falls, valid does not).
overfit: the last saved checkpoint; no_overfit_by_macro if macro(last) < min + 0.1.
"""

from __future__ import annotations

import argparse
import shutil
import sys
from pathlib import Path
from typing import Any

from core.io import ROOT, read_jsonl, write_json

K = 3
S_MARGIN = 0.05
OVERFIT_MARGIN = 0.1
EMA_ALPHA = 0.3
GEN_SETS = (("valid_format", "both_acc"), ("valid_facts", "acc"), ("valid_style", "style_all"))


def read_log(path: Path) -> tuple[list[dict[str, Any]], dict[int, dict[str, float]]]:
    """Train reports in order and {step: {task: loss}} for val records."""
    rows = read_jsonl(path)
    train = [r for r in rows if r["type"] == "train"]
    val: dict[int, dict[str, float]] = {}
    for r in rows:
        if r["type"] == "val":
            val.setdefault(r["step"], {})[r["task"]] = r["loss"]
    return train, val


def ema_by_step(train: list[dict[str, Any]], alpha: float = EMA_ALPHA) -> dict[int, float]:
    out, e = {}, None
    for r in train:
        e = r["loss"] if e is None else alpha * r["loss"] + (1 - alpha) * e
        out[r["step"]] = e
    return out


def overfit_point(steps: list[int], macro: list[float], ema: dict[int, float],
                  margin: float = S_MARGIN) -> int | None:
    """Plan step 7 definition of S (see module docstring); None if never reached."""
    for i in range(len(steps) - 1):
        j_min = min(range(i + 1), key=macro.__getitem__)
        m = macro[j_min]
        if macro[i] >= m + margin and macro[i + 1] >= m + margin:
            e_now, e_min = ema.get(steps[i + 1]), ema.get(steps[j_min])
            if e_now is not None and (e_min is None or e_now < e_min):
                return steps[i]
    return None


def loss_candidates(val: dict[int, dict[str, float]], saved: set[int], k: int = K) -> list[int]:
    steps = sorted((s for s in val if s in saved and s > 0), key=lambda s: (val[s]["macro"], s))
    return steps[:k]


def pick_best(cands: list[int], val: dict[int, dict[str, float]], gen_score: dict[int, float] | None) -> int:
    if not gen_score:
        return cands[0]
    return max(cands, key=lambda s: (gen_score[s], -val[s]["macro"], -s))


def gen_scores(run: str, cands: list[int], out_dir: Path,
               sets: tuple[tuple[str, str], ...] = GEN_SETS) -> dict[int, dict[str, Any]]:
    """Generate on the three valid sets with each candidate checkpoint (one model load)."""
    from eval.generate import checkpoint_file, load_model, set_checkpoint, unload
    from eval.run import run_cell

    model, tok = load_model(f"ckpt{cands[0]}", run)
    res: dict[int, dict[str, Any]] = {}
    for step in cands:
        set_checkpoint(model, checkpoint_file(run, step))
        metrics = {}
        for set_name, metric in sets:
            r = run_cell(model, tok, f"{set_name}__ckpt{step}__train", batch=8)
            write_json(out_dir / f"eval_{r['cell']}.json", r)
            metrics[set_name] = r["metrics"][metric]
        res[step] = {"metrics": metrics, "score": sum(metrics.values()) / len(metrics)}
    unload(model)
    return res


def copy_adapter(run: str, step: int, name: str, force: bool) -> Path:
    src_dir = ROOT / "adapters" / run
    dst = src_dir / name
    if dst.exists():
        if not force:
            raise FileExistsError(f"{dst} exists (use --force)")
        shutil.rmtree(dst)
    dst.mkdir()
    shutil.copy2(src_dir / "adapter_config.json", dst / "adapter_config.json")
    shutil.copy2(src_dir / f"{step:07d}_adapters.safetensors", dst / "adapters.safetensors")
    return dst


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--run", required=True)
    ap.add_argument("--no-gen", action="store_true")
    ap.add_argument("--force", action="store_true")
    args = ap.parse_args()
    results = ROOT / "results" / args.run
    train, val = read_log(results / "log.jsonl")
    saved = {int(p.name[:7]) for p in (ROOT / "adapters" / args.run).glob("0*_adapters.safetensors")}
    if not saved:
        print("no checkpoints", file=sys.stderr)
        return 1
    steps = sorted(s for s in val if s > 0)
    macro = [val[s]["macro"] for s in steps]
    ema = ema_by_step(train)
    cands = loss_candidates(val, saved)
    # only the tasks this run was validated on (the fs run has no format task)
    present = set(val[steps[0]])
    sets = tuple(g for g in GEN_SETS if g[0].removeprefix("valid_") in present)
    gen = None if args.no_gen else gen_scores(args.run, cands, results / "select", sets)
    best = pick_best(cands, val, {s: g["score"] for s, g in gen.items()} if gen else None)
    last = max(saved)
    vmin = min(macro)
    out = {
        "candidates": cands, "gen": gen, "best": best, "best_macro": val[best]["macro"],
        "valid_min_step": steps[macro.index(vmin)], "valid_min_macro": vmin,
        "S": overfit_point(steps, macro, ema), "overfit": last,
        "overfit_macro": val.get(last, {}).get("macro"),
        "no_overfit_by_macro": val.get(last, {}).get("macro", vmin) < vmin + OVERFIT_MARGIN,
        "rule": __doc__.split("\n", 3)[3].strip(),
    }
    copy_adapter(args.run, best, "best", args.force)
    copy_adapter(args.run, last, "overfit", args.force)
    write_json(results / "select_ckpt.json", out)
    print({k: v for k, v in out.items() if k != "rule"})
    return 0


if __name__ == "__main__":
    sys.exit(main())
