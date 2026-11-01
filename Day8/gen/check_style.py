"""Validate data/style/{train,valid}.jsonl against gen/SCHEMA.md. Exit code 1 on any failure.

Run: ../Day6/.venv/bin/python -I gen/check_style.py   (from Day8/)
"""

import json
import re
import sys
from collections import Counter
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from style_proxies import RULES_ORDER, check, explain  # noqa: E402

ROOT = Path(__file__).resolve().parent.parent
DIR = ROOT / "data" / "style"
SIZES = {"train": 101, "valid": 20}
KEYS = {"id", "task", "topic_id", "question", "answer"}
KEBAB = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
MAX_PER_TOPIC, MIN_TOPICS, MAX_JACCARD = 4, 40, 0.3


def load(name):
    return [json.loads(line) for line in open(DIR / f"{name}.jsonl", encoding="utf-8")]


def grams(text, n=3):
    t = re.sub(r"\s+", " ", text.lower().replace("ё", "е")).strip()
    return {t[i:i + n] for i in range(len(t) - n + 1)}


def jaccard(a, b):
    return len(a & b) / len(a | b) if a | b else 0.0


def main() -> int:
    data = {k: load(k) for k in SIZES}
    errors = []
    for k, n in SIZES.items():
        print(f"{k}: {len(data[k])} rows (expected {n})")
        if len(data[k]) != n:
            errors.append(f"{k} size {len(data[k])} != {n}")
        for r in data[k]:
            if set(r) != KEYS or r["task"] != "style" or not KEBAB.match(r["topic_id"]):
                errors.append(f"{r.get('id')}: bad keys/task/topic_id")
            nq = len([s for s in re.split(r"(?<=[.?!…])\s+", r["question"].strip()) if s])
            if not 1 <= nq <= 3:
                errors.append(f"{r['id']}: question has {nq} sentences")

    rows = data["train"] + data["valid"]
    ids = Counter(r["id"] for r in rows)
    dup = [i for i, c in ids.items() if c > 1]
    print(f"unique ids: {len(ids)}/{len(rows)}")
    if dup:
        errors.append(f"duplicate ids: {dup}")
    dupq = [q for q, c in Counter(r["question"] for r in rows).items() if c > 1]
    if dupq:
        errors.append(f"duplicate questions: {dupq}")

    t_train = Counter(r["topic_id"] for r in data["train"])
    t_valid = Counter(r["topic_id"] for r in data["valid"])
    t_all = t_train + t_valid
    inter = set(t_train) & set(t_valid)
    print(f"topics: train {len(t_train)}, valid {len(t_valid)}, total {len(t_all)}; "
          f"max per topic {max(t_all.values())}; train∩valid = {sorted(inter) or '∅'}")
    if inter:
        errors.append(f"topic overlap {inter}")
    if len(t_all) < MIN_TOPICS:
        errors.append(f"only {len(t_all)} topics")
    over = {t: c for t, c in t_all.items() if c > MAX_PER_TOPIC}
    if over:
        errors.append(f"topics over {MAX_PER_TOPIC}: {over}")

    used = json.loads((DIR / "topics_used.json").read_text(encoding="utf-8"))
    if set(used["all"]) != set(t_all):
        errors.append("topics_used.json out of sync")

    tg = [(r["id"], grams(r["question"])) for r in data["train"]]
    best = max(((jaccard(grams(v["question"]), g), v["id"], tid) for v in data["valid"] for tid, g in tg))
    print(f"max Jaccard(3-gram) valid vs train questions: {best[0]:.3f} ({best[1]} ~ {best[2]})")
    if best[0] > MAX_JACCARD:
        errors.append(f"Jaccard {best[0]:.3f} > {MAX_JACCARD}")

    per_rule = Counter()
    lens = []
    for r in rows:
        res = check(r["answer"])
        lens.append(len(r["answer"]))
        for rule in RULES_ORDER:
            per_rule[rule] += res[rule]
        if not res["all"]:
            errors.append(f"{r['id']} style: {explain(r['answer'])}")
    print("style proxies pass: " + ", ".join(f"{k} {per_rule[k]}/{len(rows)}" for k in RULES_ORDER))
    print(f"answer length: min {min(lens)}, max {max(lens)}, mean {sum(lens) / len(lens):.0f}")

    for e in errors:
        print("FAIL", e)
    print("OK" if not errors else f"{len(errors)} errors")
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
