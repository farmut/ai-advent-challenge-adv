"""Run the eval set on a model configuration, save answers and metrics.

usage: python eval.py [--leaky] CONFIG [CONFIG ...]
  --leaky        run on data/eval_leaky.jsonl (first eval draft whose plots overlap train) instead of
                 data/eval.jsonl; results get the suffix _leaky
  base_raw       base model, no system prompt, the situation only
  base_prompted  base model + detailed prompt (SYSTEM_PROMPTED: verdicts, 15 principles, format, example)
  tuned          base + adapter + detailed prompt (the adapter was trained with SYSTEM_PROMPTED)
  tuned_short    base + adapter, short prompt SYSTEM (runs 1-2 were trained with it)
-> results/eval_<config>.json, results/summary.json (merged across runs)
Metrics: format_ok (strict 4-line format with valid labels), verdict_acc, principle_acc, both_acc.
Lenient variants search the whole text for a verdict word / principle name, so a model that answers
in free form is not scored zero only for the format.
"""

import argparse
import json
import re
import time
from collections import Counter
from pathlib import Path

from mlx_lm import generate, load
from mlx_lm.sample_utils import make_sampler

from common import PRINCIPLES, SYSTEM, SYSTEM_PROMPTED, parse_answer

ROOT = Path(__file__).parent
MODEL = str(ROOT / "models/qwen2.5-7b-instruct-4bit-bf16")
ADAPTER = str(ROOT / "adapters/best")  # step 150 of run 4b, chosen on valid (results/select_ckpt.json)
OUT = ROOT / "results"

CONFIGS = {
    "base_raw": (None, None),
    "base_prompted": (None, SYSTEM_PROMPTED),
    "tuned": (ADAPTER, SYSTEM_PROMPTED),
    "tuned_short": (ADAPTER, SYSTEM),
}

_VERDICT_WORDS = (("Нарушение", r"наруш"), ("Риск", r"риск"), ("Соответствует", r"соответств|соблюд"))


def lenient(text: str) -> dict:
    low = text.lower().replace("ё", "е")
    pos = {v: m.start() for v, pat in _VERDICT_WORDS if (m := re.search(pat, low))}
    names = {p["id"]: low.find(p["name"].lower()) for p in PRINCIPLES}
    names = {k: i for k, i in names.items() if i >= 0}
    return {"verdict": min(pos, key=pos.get) if pos else None,
            "principle": min(names, key=names.get) if names else None}


def run(name: str, items: list[dict]) -> list[dict]:
    adapter, system = CONFIGS[name]
    model, tok = load(MODEL, adapter_path=adapter)
    sampler = make_sampler(temp=0.0)
    rows = []
    for it in items:
        msgs = ([{"role": "system", "content": system}] if system else []) + [
            {"role": "user", "content": it["situation"]}]
        prompt = tok.apply_chat_template(msgs, add_generation_prompt=True, tokenize=False)
        t0 = time.time()
        ans = generate(model, tok, prompt, max_tokens=400, sampler=sampler).strip()
        p, l = parse_answer(ans), lenient(ans)
        rows.append({"id": it["id"], "gold_verdict": it["verdict"], "gold_principle": it["principle"],
                     "answer": ans, "sec": round(time.time() - t0, 2),
                     "format_ok": p["format_ok"], "verdict": p["verdict"], "principle": p["principle"],
                     "verdict_lenient": l["verdict"], "principle_lenient": l["principle"]})
    del model
    return rows


def score(rows: list[dict]) -> dict:
    n = len(rows)

    def acc(f):
        return round(sum(map(f, rows)) / n, 3)
    return {
        "n": n,
        "format_ok": acc(lambda r: r["format_ok"]),
        "verdict_acc": acc(lambda r: r["verdict"] == r["gold_verdict"]),
        "principle_acc": acc(lambda r: r["principle"] == r["gold_principle"]),
        "both_acc": acc(lambda r: r["verdict"] == r["gold_verdict"] and r["principle"] == r["gold_principle"]),
        "verdict_acc_lenient": acc(lambda r: r["verdict_lenient"] == r["gold_verdict"]),
        "principle_acc_lenient": acc(lambda r: r["principle_lenient"] == r["gold_principle"]),
        "verdict_acc_by_gold": {v: round(sum(r["verdict"] == v for r in rows if r["gold_verdict"] == v)
                                         / c, 3) for v, c in Counter(r["gold_verdict"] for r in rows).items()},
        "avg_chars": round(sum(len(r["answer"]) for r in rows) / n),
        "avg_sec": round(sum(r["sec"] for r in rows) / n, 2),
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("configs", nargs="+", choices=list(CONFIGS))
    ap.add_argument("--leaky", action="store_true")
    args = ap.parse_args()
    suffix = "_leaky" if args.leaky else ""

    items = [json.loads(l) for l in (ROOT / f"data/eval{suffix}.jsonl").open(encoding="utf-8")]
    OUT.mkdir(exist_ok=True)
    summary_path = OUT / "summary.json"
    summary = json.loads(summary_path.read_text(encoding="utf-8")) if summary_path.exists() else {}
    for name in args.configs:
        rows = run(name, items)
        key = name + suffix
        (OUT / f"eval_{key}.json").write_text(json.dumps(rows, ensure_ascii=False, indent=1), encoding="utf-8")
        summary[key] = score(rows)
        print(key, json.dumps(summary[key], ensure_ascii=False))
        summary_path.write_text(json.dumps(summary, ensure_ascii=False, indent=1), encoding="utf-8")


if __name__ == "__main__":
    main()
