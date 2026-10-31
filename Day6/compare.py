"""Run the same prompts on the base model and on base+adapter, save answers and metrics.

usage: python compare.py [--extra N]
  20 fixed queries (16 held-out SberQuAD + 4 general without context) -> results/compare_20.json
  --extra N: N more held-out SberQuAD items for aggregate metrics -> results/compare_extra.json
Metrics (SQuAD-style normalization): EM, token F1, contains (gold span is inside the answer).
"""

import argparse
import json
import random
import string
import time
from collections import Counter
from pathlib import Path

from mlx_lm import generate, load
from mlx_lm.sample_utils import make_sampler

from prepare_data import SYSTEM, user_prompt

ROOT = Path(__file__).parent
MODEL = str(ROOT / "models/qwen2.5-7b-instruct-4bit-bf16")  # same base the adapter was trained on
ADAPTER = ROOT / "adapters"
OUT = ROOT / "results"

SYSTEM_GENERAL = "Ты — эрудит."
GENERAL = [
    "Сколько будет 17 умножить на 23? Ответь числом.",
    "Кратко расскажи, кто такой Юрий Гагарин.",
    "Напиши функцию на Python, которая проверяет, является ли строка палиндромом.",
    "Почему небо голубое? Объясни в двух предложениях.",
]


def normalize(s: str) -> str:
    s = s.lower().replace("ё", "е")
    s = "".join(ch for ch in s if ch not in string.punctuation + "«»—–…")
    return " ".join(s.split())


def f1(gold: str, ans: str) -> float:
    g, a = normalize(gold).split(), normalize(ans).split()
    common = sum((Counter(g) & Counter(a)).values())
    if not common:
        return 0.0
    p, r = common / len(a), common / len(g)
    return 2 * p * r / (p + r)


def metrics(gold: str, ans: str) -> dict:
    return {"em": float(normalize(gold) == normalize(ans)),
            "f1": round(f1(gold, ans), 3),
            "contains": float(f" {normalize(gold)} " in f" {normalize(ans)} ")}


def run(model, tok, items):
    sampler = make_sampler(temp=0.0)
    res = []
    for it in items:
        if it["kind"] == "general":
            msgs = [{"role": "system", "content": SYSTEM_GENERAL}, {"role": "user", "content": it["question"]}]
        else:
            msgs = [{"role": "system", "content": SYSTEM},
                    {"role": "user", "content": user_prompt(it["context"], it["question"])}]
        prompt = tok.apply_chat_template(msgs, add_generation_prompt=True, tokenize=False)
        t0 = time.time()
        ans = generate(model, tok, prompt, max_tokens=300, sampler=sampler)
        res.append({"answer": ans.strip(), "sec": round(time.time() - t0, 2)})
    return res


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--extra", type=int, default=0)
    args = ap.parse_args()

    pool = [json.loads(l) for l in (ROOT / "data/test_pool.jsonl").open(encoding="utf-8")]
    random.Random(7).shuffle(pool)
    fixed = [{"kind": "sberquad", **p} for p in pool[:16]]
    fixed += [{"kind": "general", "question": q, "context": "", "gold": ""} for q in GENERAL]
    extra = [{"kind": "sberquad", **p} for p in pool[16:16 + args.extra]]
    items = fixed + extra

    answers = {}
    for name, adapter in (("base", None), ("tuned", str(ADAPTER))):
        model, tok = load(MODEL, adapter_path=adapter)
        answers[name] = run(model, tok, items)
        del model

    rows = []
    for i, it in enumerate(items):
        b, t = answers["base"][i], answers["tuned"][i]
        row = {**it, "base": b["answer"], "tuned": t["answer"], "base_sec": b["sec"], "tuned_sec": t["sec"]}
        if it["kind"] == "sberquad":
            row["base_m"], row["tuned_m"] = metrics(it["gold"], b["answer"]), metrics(it["gold"], t["answer"])
        rows.append(row)

    OUT.mkdir(exist_ok=True)
    (OUT / "compare_20.json").write_text(json.dumps(rows[:len(fixed)], ensure_ascii=False, indent=1), encoding="utf-8")
    if extra:
        (OUT / "compare_extra.json").write_text(json.dumps(rows[len(fixed):], ensure_ascii=False, indent=1),
                                                encoding="utf-8")

    summary = {}
    for label, part in (("fixed16", rows[:16]), ("extra", rows[len(fixed):]), ("all_sberquad", rows[:16] + rows[len(fixed):])):
        if not part:
            continue
        summary[label] = {"n": len(part)}
        for who in ("base", "tuned"):
            summary[label][who] = {k: round(sum(r[f"{who}_m"][k] for r in part) / len(part), 3)
                                   for k in ("em", "f1", "contains")}
            summary[label][who]["avg_chars"] = round(sum(len(r[who]) for r in part) / len(part))
            summary[label][who]["avg_sec"] = round(sum(r[f"{who}_sec"] for r in part) / len(part), 2)
    (OUT / "summary.json").write_text(json.dumps(summary, ensure_ascii=False, indent=1), encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False, indent=1))


if __name__ == "__main__":
    main()
