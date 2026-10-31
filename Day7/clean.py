"""Raw generated examples -> clean train/valid (chat JSONL for mlx_lm) and eval set.

usage: python clean.py
  data/raw/gen_[A-E].jsonl -> data/train.jsonl, data/valid.jsonl
  data/raw/gen_EV.jsonl    -> data/eval.jsonl
Steps: schema check, text normalization, exact + near-duplicate removal (char 3-gram Jaccard),
removal of train examples similar to any eval item, stratified split, balance report.
Before that, data/style_fixes.json, data/fixes.json (manual review) and data/semantic_leaks.json are
applied to the train pool (see apply_fixes). Eval items marked FAIL in data/eval_audit2.json are dropped.
"""

import json
import random
from collections import Counter
from pathlib import Path

from common import NAME_BY_ID, SYSTEM_PROMPTED, VERDICTS, clean_text, format_answer

ROOT = Path(__file__).parent
RAW = ROOT / "data/raw"
OUT = ROOT / "data"
SEED = 42
VALID_SHARE = 0.1
NEAR_DUP = 0.5    # Jaccard of char 3-grams over the situation text
EVAL_LEAK = 0.4   # stricter: a train example this close to an eval item is dropped
FIELDS = ("id", "principle", "verdict", "situation", "justification", "recommendation")


def load(path: Path) -> list[dict]:
    rows = []
    for n, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip():
            continue
        r = json.loads(line)
        missing = [f for f in FIELDS if not str(r.get(f, "")).strip()]
        if missing or r["principle"] not in NAME_BY_ID or r["verdict"] not in VERDICTS:
            print(f"skip {path.name}:{n} bad schema ({missing or r['principle'] + '/' + r['verdict']})")
            continue
        rows.append({**r, "situation": clean_text(r["situation"])})
    return rows


def shingles(s: str) -> set[str]:
    s = " ".join(s.lower().replace("ё", "е").split())
    return {s[i:i + 3] for i in range(len(s) - 2)}


def jaccard(a: set, b: set) -> float:
    return len(a & b) / len(a | b) if a | b else 0.0


def dedup(rows: list[dict], log: list) -> list[dict]:
    kept, sh = [], []
    for r in rows:
        s = shingles(r["situation"])
        hit = max(((jaccard(s, t), k) for k, t in enumerate(sh)), default=(0.0, -1))
        if hit[0] >= NEAR_DUP:
            log.append({"dropped": r["id"], "dup_of": kept[hit[1]]["id"], "jaccard": round(hit[0], 3)})
            continue
        kept.append(r)
        sh.append(s)
    return kept


def to_chat(r: dict) -> dict:
    return {"messages": [
        # the same detailed prompt as the prompted baseline: the adapter learns to apply the listed
        # principles, not to memorize their names (runs with the short SYSTEM could not, see REPORT.md)
        {"role": "system", "content": SYSTEM_PROMPTED},
        {"role": "user", "content": r["situation"]},
        {"role": "assistant", "content": format_answer(r["verdict"], r["principle"],
                                                       r["justification"], r["recommendation"])},
    ]}


def read_json(name: str) -> dict:
    path = OUT / name
    return json.loads(path.read_text(encoding="utf-8")) if path.exists() else {}


def apply_fixes(rows: list[dict], log: dict) -> list[dict]:
    """style_fixes.json (unified recommendation style), then fixes.json (manual review) on top;
    semantic_leaks.json: train items with the same plot as an eval item or another train item are dropped."""
    style, manual, sem = read_json("style_fixes.json"), read_json("fixes.json"), read_json("semantic_leaks.json")
    drop = {t: f"same plot as {x['eval']}" for x in sem.get("eval_leaks", []) for t in x["train"]}
    drop |= {t: f"same plot as {x['keep']}" for x in sem.get("train_dups", []) for t in x["drop"]}
    out = []
    for r in rows:
        if r["id"] in drop:
            log["semantic_dropped"].append({"dropped": r["id"], "why": drop[r["id"]]})
            continue
        out.append({**r, **style.get(r["id"], {}), **manual.get(r["id"], {})})
    return out


def balance(rows: list[dict]) -> dict:
    return {"by_principle": dict(Counter(r["principle"] for r in rows)),
            "by_verdict": dict(Counter(r["verdict"] for r in rows))}


def main():
    log = {"semantic_dropped": [], "near_dups": [], "eval_leaks": []}
    raw = [r for p in sorted(RAW.glob("gen_[A-E].jsonl")) for r in load(p)]
    n_raw = len(raw)
    pool = apply_fixes(raw, log)
    # eval items that failed the second independent audit are excluded, not rewritten again
    failed = {k for k, v in read_json("eval_audit2.json").items() if v["result"] == "FAIL"}
    ev = [e for e in load(RAW / "gen_EV.jsonl") if e["id"] not in failed]
    seen = set()
    exact = [r for r in pool if not (r["situation"].lower() in seen or seen.add(r["situation"].lower()))]
    pool = dedup(exact, log["near_dups"])
    ev = dedup(ev, log["near_dups"])

    ev_sh = [shingles(e["situation"]) for e in ev]
    clean = []
    for r in pool:
        s = shingles(r["situation"])
        j, k = max((jaccard(s, t), k) for k, t in enumerate(ev_sh))
        if j >= EVAL_LEAK:
            log["eval_leaks"].append({"dropped": r["id"], "eval": ev[k]["id"], "jaccard": round(j, 3)})
        else:
            clean.append(r)

    rng = random.Random(SEED)
    train, valid = [], []
    strata = {}
    for r in clean:
        strata.setdefault((r["principle"], r["verdict"]), []).append(r)
    for key in sorted(strata):
        part = strata[key]
        rng.shuffle(part)
        n_val = round(len(part) * VALID_SHARE)
        valid += part[:n_val]
        train += part[n_val:]
    rng.shuffle(train)
    rng.shuffle(valid)

    for name, part in (("train", train), ("valid", valid)):
        with (OUT / f"{name}.jsonl").open("w", encoding="utf-8") as fh:
            for r in part:
                fh.write(json.dumps(to_chat(r), ensure_ascii=False) + "\n")
    with (OUT / "eval.jsonl").open("w", encoding="utf-8") as fh:
        for e in ev:
            fh.write(json.dumps({k: e[k] for k in (*FIELDS, "pages") if k in e}, ensure_ascii=False) + "\n")
    with (OUT / "train_meta.jsonl").open("w", encoding="utf-8") as fh:
        for r in train + valid:
            fh.write(json.dumps({**r, "split": "train" if r in train else "valid"}, ensure_ascii=False) + "\n")

    report = {"raw": n_raw, "semantic_dropped": len(log["semantic_dropped"]), "after_exact_dedup": len(exact), "after_near_dedup": len(pool),
              "after_eval_leak": len(clean), "train": len(train), "valid": len(valid), "eval": len(ev),
              "near_dups_dropped": len(log["near_dups"]), "eval_leaks_dropped": len(log["eval_leaks"]),
              "train_valid_balance": balance(clean), "eval_balance": balance(ev), "log": log}
    (OUT / "clean_report.json").write_text(json.dumps(report, ensure_ascii=False, indent=1), encoding="utf-8")
    print(json.dumps({k: v for k, v in report.items() if k != "log"}, ensure_ascii=False, indent=1))


if __name__ == "__main__":
    main()
