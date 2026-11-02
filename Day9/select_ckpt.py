"""Checkpoint selection on valid only (plan §6.3) -> results/<run>/select_ckpt.json, adapters/<run>/best/.

usage: python select_ckpt.py --run student [--stdout results/student/train.stdout] [--force]
best: the saved checkpoint with the highest both_acc on the current distillation valid set (49 rows for prereg v2,
      greedy, data/distill/valid.jsonl, system = LIST_PROMPT); ties -> lower pooled valid loss -> earlier step.
The eval set is never opened here (tests/test_isolation.py); select_ckpt.json records read_eval=false.

Day8 copy, modified: Day8 chose among the K lowest-loss checkpoints by a 3-task score; Day9 has one task, so
every saved checkpoint is generated on valid (13 for the main run) and loss is only the tie-break.
"""

from __future__ import annotations

import argparse
import math
import shutil
import sys
from collections.abc import Callable
from pathlib import Path
from typing import Any

from core.io import ROOT, write_json
from train_log import parse_train, parse_val

VALID_CELL = "valid:ckpt{step}:train"
RULE = ("max both_acc on current data/distill/valid.jsonl (49 rows for prereg v2, greedy, LIST_PROMPT) over all "
        "saved checkpoints; ties -> lower pooled valid loss -> earlier step; eval is not read")


def saved_steps(run: str, root: Path = ROOT) -> list[int]:
    return sorted(int(p.name[:7]) for p in (root / "adapters" / run).glob("0*_adapters.safetensors"))


def pick_best(scores: dict[int, float], val_loss: dict[int, float]) -> int:
    """max both_acc; tie -> lower valid loss (missing/NaN = +inf) -> earlier step."""
    if not scores:
        raise ValueError("no checkpoint scores")

    def loss(s: int) -> float:
        v = val_loss.get(s, math.inf)
        return math.inf if math.isnan(v) else v

    return min(scores, key=lambda s: (-scores[s], loss(s), s))


Runner = Callable[[int], dict[str, Any]]


def generation_runner(run: str, out_dir: Path) -> tuple[Runner, Callable[[], None]]:
    """One model load; every call swaps the LoRA weights and generates the valid cell."""
    from eval.generate import ModelSpec, checkpoint_file, load_model, set_checkpoint, unload
    from eval.registry import parse_cell
    from eval.run import run_local_cell

    steps = saved_steps(run)
    spec = ModelSpec(f"ckpt{steps[0]}", run=run)
    model, tok = load_model(spec)

    def runner(step: int) -> dict[str, Any]:
        set_checkpoint(model, checkpoint_file(run, step))
        cell = parse_cell(VALID_CELL.format(step=step))
        res = run_local_cell(model, tok, cell, 8, ModelSpec(f"ckpt{step}", run=run))
        write_json(out_dir / f"{cell.file_stem}.json", res)
        return res["metrics"]

    return runner, lambda: unload(model)


def select(run: str, steps: list[int], runner: Runner, val_loss: dict[int, float]) -> dict[str, Any]:
    metrics = {s: runner(s) for s in steps}
    scores = {s: m["both_acc"] for s, m in metrics.items()}
    best = pick_best(scores, val_loss)
    return {"run": run, "best": best, "scores": scores,
            "principle_acc": {s: m.get("principle_acc") for s, m in metrics.items()},
            "verdict_acc": {s: m.get("verdict_acc") for s, m in metrics.items()},
            "format_ok": {s: m.get("format_ok") for s, m in metrics.items()},
            "val_loss": {s: val_loss.get(s) for s in steps}, "valid_set": "data/distill/valid.jsonl",
            "read_eval": False, "rule": RULE}


def copy_adapter(run: str, step: int, name: str, force: bool, root: Path = ROOT) -> Path:
    src_dir = root / "adapters" / run
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
    ap.add_argument("--stdout", type=Path, default=None)
    ap.add_argument("--force", action="store_true")
    args = ap.parse_args()
    results = ROOT / "results" / args.run
    stdout = args.stdout or results / "train.stdout"
    steps = saved_steps(args.run)
    if not steps:
        print("no checkpoints", file=sys.stderr)
        return 1
    val_loss = parse_val(stdout) if stdout.exists() else {}
    runner, close = generation_runner(args.run, results / "select")
    try:
        out = select(args.run, steps, runner, val_loss)
    finally:
        close()
    out["train_log_rows"] = len(parse_train(stdout)) if stdout.exists() else 0
    copy_adapter(args.run, out["best"], "best", args.force)
    write_json(results / "select_ckpt.json", out)
    print({k: v for k, v in out.items() if k != "rule"})
    return 0


if __name__ == "__main__":
    sys.exit(main())
