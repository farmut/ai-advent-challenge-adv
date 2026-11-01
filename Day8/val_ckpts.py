"""Per-task valid loss for every saved checkpoint of a stock `mlx_lm.lora` run -> results/<run>/log.jsonl.

Why: our train.py diverged in the smoke tests while the stock trainer with the Day7 4b recipe was stable on the
same data (see swarm-report/day8-finetune-progress.md), so the main run uses the stock trainer. It reports only one
pooled valid loss; this script adds the per-task losses offline, in the log format select_ckpt.py/report.py read.

usage: python val_ckpts.py --run main --stdout results/main/train.stdout
  train records are parsed from the stock trainer's stdout (iter, train_loss, lr);
  val records: step 0 = base model, then every adapters/<run>/00NNNNN_adapters.safetensors.
"""

from __future__ import annotations

import argparse
import json
import re
import time
from pathlib import Path

import mlx.core as mx

from core.io import ROOT, read_jsonl
from train import TASKS, evaluate_tasks

ANSI = re.compile(r"\x1b\[[0-9;]*m")
# stock mlx_lm 0.32.2 report row: "    25    2.044 ▼    1.2e-05 ..." (columns vary; we take iter and train_loss)
ROW = re.compile(r"^\s*(\d+)\s+([0-9]+\.[0-9]+)\s")


def parse_train(stdout: Path) -> list[dict]:
    rows = []
    for line in ANSI.sub("", stdout.read_text(encoding="utf-8", errors="replace")).splitlines():
        if "val" in line:
            continue
        if m := ROW.match(line):
            rows.append({"type": "train", "step": int(m.group(1)), "loss": float(m.group(2))})
    return rows


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--run", required=True)
    ap.add_argument("--stdout", type=Path, required=True)
    ap.add_argument("--config", type=Path, default=ROOT / "configs/main_stock.yaml")
    ap.add_argument("--tasks", nargs="+", default=list(TASKS), choices=list(TASKS),
                    help="valid sets to score (data/mix/valid_<task>.jsonl); the fs run has no format task")
    ap.add_argument("--train-sample", type=int, default=0,
                    help="also score a fixed sample of N train rows per task (first N in file order) -> task 'train_<t>'")
    args = ap.parse_args()

    from mlx_lm import load
    from mlx_lm.tuner.datasets import CacheDataset, ChatDataset

    raw = __import__("yaml").safe_load(args.config.read_text(encoding="utf-8"))
    adapters = ROOT / "adapters" / args.run
    out = ROOT / "results" / args.run / "log.jsonl"
    if out.exists():
        raise SystemExit(f"{out} exists")
    out.parent.mkdir(parents=True, exist_ok=True)
    recs = parse_train(args.stdout)

    class C:  # the fields evaluate_tasks uses
        batch_size = raw["batch_size"]
        max_seq_length = raw["max_seq_length"]

    base_model, tok = load(str(ROOT / raw["model"]))
    valid = {t: CacheDataset(ChatDataset(read_jsonl(ROOT / f"data/mix/valid_{t}.jsonl"), tok, mask_prompt=True))
             for t in args.tasks}
    train_sets: dict = {}
    if args.train_sample:
        tags = {"format": None, "facts": "Режим: регламент", "style": "Режим: стиль"}
        train_rows = read_jsonl(ROOT / raw["data"] / "train.jsonl")
        for t in args.tasks:
            rows = [r for r in train_rows if (tags[t] in r["messages"][0]["content"] if tags[t]
                                              else "Режим:" not in r["messages"][0]["content"])][:args.train_sample]
            train_sets[f"train_{t}"] = CacheDataset(ChatDataset(rows, tok, mask_prompt=True))

    def val(model, step: int) -> None:
        t0 = time.perf_counter()
        losses = evaluate_tasks(model, valid, C)
        if train_sets:  # separate call: macro/pooled above stay valid-only
            losses |= {k: v for k, v in evaluate_tasks(model, train_sets, C).items() if k.startswith("train_")}
        sec = round(time.perf_counter() - t0, 1)
        recs.extend({"type": "val", "step": step, "task": k, "loss": v, "sec": sec} for k, v in losses.items())
        print(step, {k: round(v, 4) for k, v in losses.items()}, f"{sec}s", flush=True)

    val(base_model, 0)
    del base_model
    model, _ = load(str(ROOT / raw["model"]), adapter_path=str(adapters))
    for ck in sorted(adapters.glob("0*_adapters.safetensors")):
        model.load_weights(str(ck), strict=False)
        mx.eval(model.parameters())
        val(model, int(ck.name.split("_")[0]))
    recs.sort(key=lambda r: (r["step"], r["type"] != "train"))
    out.write_text("".join(json.dumps(r, ensure_ascii=False) + "\n" for r in recs), encoding="utf-8")
    print(f"wrote {out}: {sum(r['type'] == 'train' for r in recs)} train, {sum(r['type'] == 'val' for r in recs)} val")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
