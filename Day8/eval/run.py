"""Run eval cells and score them.

usage:
  python -m eval.run --run main --cells facts__base__tag ood__best__neutral
  python -m eval.run --run main --all [--optional]
  python -m eval.run --run main --day7-regression     # base + FORMAT_GOOD on data/format/eval.jsonl
  python -m eval.run --hashes                          # print hashes for results/prereg.json
Writes results/<run>/eval_<cell>.json (answers + per-row scores) and merges metrics into summary.json.
Each model is loaded once; cells are grouped by model. Existing cell files are skipped unless --force.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from collections import defaultdict
from pathlib import Path
from typing import Any

from core.hashes import check_prereg, current_hashes
from core.io import ROOT, read_json, sha256_text, write_json
from eval.registry import DAY7_REGRESSION, OPTIONAL, REQUIRED, SETS, Item, parse_cell, system_prompt
from eval.scorers import SCORERS

DROP = ("user", "system_train")


def run_cell(model: Any, tok: Any, cell_name: str, batch: int, root: Path = ROOT) -> dict[str, Any]:
    """Generate and score one cell with an already loaded model."""
    from eval.generate import generate

    set_name, model_key, prompt = parse_cell(cell_name)
    es = SETS[set_name]
    items: list[Item] = es.items(root)
    systems = [system_prompt(set_name, prompt, it) for it in items]
    t0 = time.perf_counter()
    gens = generate(model, tok, systems, [it["user"] for it in items], es.max_tokens, batch)
    scorer = SCORERS[es.scorer]
    rows = [{**{k: v for k, v in it.items() if k not in DROP}, **g, **scorer.score(it, g["answer"])}
            for it, g in zip(items, gens)]
    metrics = scorer.summarize(rows) | {
        "avg_sec": round(sum(r["sec"] for r in rows) / len(rows), 3),
        "avg_prompt_tokens": round(sum(r["prompt_tokens"] for r in rows) / len(rows), 1),
        "avg_gen_tokens": round(sum(r["gen_tokens"] for r in rows) / len(rows), 1),
        "hit_max_tokens": round(sum(r["hit_max_tokens"] for r in rows) / len(rows), 4),
        "wall_sec": round(time.perf_counter() - t0, 1)}
    return {"cell": cell_name, "set": set_name, "model": model_key, "prompt": prompt, "path": es.path,
            "max_tokens": es.max_tokens, "batch": batch, "decoding": "greedy",
            "system_sha256": sorted({sha256_text(s) for s in systems}), "n": len(rows),
            "metrics": metrics, "rows": rows}


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--run")
    ap.add_argument("--cells", nargs="+", default=[])
    ap.add_argument("--all", action="store_true", help="the 21 required cells")
    ap.add_argument("--optional", action="store_true", help="add the optional cells")
    ap.add_argument("--day7-regression", action="store_true")
    ap.add_argument("--batch", type=int, default=8)
    ap.add_argument("--force", action="store_true", help="recompute cells that already have results")
    ap.add_argument("--allow-hash-mismatch", action="store_true")
    ap.add_argument("--hashes", action="store_true", help="print current hashes and exit")
    args = ap.parse_args(argv)

    if args.hashes:
        print(json.dumps(current_hashes(), ensure_ascii=False, indent=1))
        return 0
    if not args.run:
        ap.error("--run is required")
    cells = list(args.cells)
    if args.day7_regression:
        cells = [DAY7_REGRESSION["cell"]] + cells
    if args.all:
        cells += REQUIRED
    if args.optional:
        cells += OPTIONAL
    cells = list(dict.fromkeys(cells))
    if not cells:
        ap.error("no cells given")
    for c in cells:
        parse_cell(c)

    if bad := check_prereg():
        print(f"hash mismatch with results/prereg.json: {bad}", file=sys.stderr)
        if not args.allow_hash_mismatch:
            return 1

    out = ROOT / "results" / args.run
    out.mkdir(parents=True, exist_ok=True)
    summary_path = out / "summary.json"
    todo = [c for c in cells if args.force or not (out / f"eval_{c}.json").exists()]
    by_model: dict[str, list[str]] = defaultdict(list)
    for c in todo:
        by_model[parse_cell(c)[1]].append(c)

    from eval.generate import load_model, unload

    for model_key, model_cells in sorted(by_model.items(), key=lambda kv: kv[0] != "base"):
        model, tok = load_model(model_key, args.run)
        for c in model_cells:
            res = run_cell(model, tok, c, args.batch)
            write_json(out / f"eval_{c}.json", res)
            summary = read_json(summary_path) if summary_path.exists() else {}
            summary[c] = res["metrics"]
            write_json(summary_path, summary)
            print(c, json.dumps(res["metrics"], ensure_ascii=False), flush=True)
        unload(model)

    if args.day7_regression:
        reg = DAY7_REGRESSION
        got = read_json(out / f"eval_{reg['cell']}.json")["metrics"][reg["metric"]]
        ok = abs(got - reg["expected"]) <= reg["tol"]
        print(f"Day7 regression {reg['metric']}={got} expected {reg['expected']}±{reg['tol']}: "
              f"{'OK' if ok else 'FAIL — switch to mlx_lm.generate'}")
        if not ok:
            return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
