"""Data gates (plan step 3) -> results/gates.json (merged by gate).

usage:
  python gates.py                       G2 only (lexical, no model)
  python gates.py --with-model          + G1 (base model knows no facts; RAG works) and G3 sample generation
  python gates.py --g3-labels FILE      G3 agreement of style proxies with manual labels
G1: base+TAG_FACTS and base+NEUTRAL answer ≤1/40 fact questions (30 valid V1 + 10 control); base+RAG on
    strata a ≥ 0.90. G2: char-3-gram Jaccard ≤ 0.3, no key in the question, disjoint template ids, style eval
    far from train, audit without LEAK. G3: per proxy rule agreement ≥ 27/30 with manual labels.
Labels file: JSONL rows {"id": <sample id>, "labels": {"vy": true, ...}} for results/g3_samples.jsonl.
"""

from __future__ import annotations

import argparse
import sys
from collections import Counter
from pathlib import Path
from typing import Any

from core.io import ROOT, read_json, read_jsonl, write_json, write_jsonl
from core.text import keys_match, max_jaccard

JACCARD_MAX = 0.3
G1_MAX_CORRECT = 1
G1_RAG_MIN = 0.90
G3_MIN_AGREE = 27
G3_N = 30


def _count_leaks(obj: Any) -> int:
    if isinstance(obj, str):
        return int(obj.strip().upper() == "LEAK")
    if isinstance(obj, dict):
        return sum(_count_leaks(v) for v in obj.values())
    if isinstance(obj, list):
        return sum(_count_leaks(v) for v in obj)
    return 0


def gate_g2(root: Path = ROOT) -> dict[str, Any]:
    """Lexical gates without a model."""
    from core.prompts import style_shot_ids

    train = read_jsonl(root / "data/facts/train.jsonl")
    valid = read_jsonl(root / "data/facts/valid.jsonl")
    ev = read_jsonl(root / "data/facts/eval_gen.jsonl")
    q_by_fact: dict[str, list[str]] = {}
    for r in train:
        q_by_fact.setdefault(r["fact_id"], []).append(r["question"])
    all_train_q = [r["question"] for r in train]

    jac = []
    for r in ev:
        others = all_train_q if r["strata"] == "control" else [q for f in r["fact_ids"] for q in q_by_fact[f]]
        jac.append({"id": r["id"], "strata": r["strata"], "max_jaccard": round(max_jaccard(r["question"], others), 3)})
    from eval.registry import excluded_ids
    drop = excluded_ids(root)
    facts_bad = [j for j in jac if j["strata"] in ("b", "c", "d") and j["max_jaccard"] > JACCARD_MAX
                 and j["id"] not in drop]
    valid_jac = max((max_jaccard(r["question"], q_by_fact[r["fact_id"]]) for r in valid), default=0.0)

    key_in_q = [r["id"] for r in train + valid + ev
                if keys_match(r["answer_keys"], r["question"], require_all=False)]
    tmpl = {r["template_id"] for r in ev} & {r["template_id"] for r in train + valid}

    st_train = read_jsonl(root / "data/style/train.jsonl") + read_jsonl(root / "data/style/valid.jsonl")
    st_eval = read_jsonl(root / "data/style/eval.jsonl")
    st_q = [r["question"] for r in st_train]
    st_jac = {r["id"]: round(max_jaccard(r["question"], st_q), 3) for r in st_eval}
    st_bad = {k: v for k, v in st_jac.items() if v > JACCARD_MAX and k not in drop}
    topics = {r["topic_id"] for r in st_train} & {r["topic_id"] for r in st_eval}
    shots = style_shot_ids(root)

    audit_path = root / "data/audit.json"
    audit = read_json(audit_path) if audit_path.exists() else None
    leaks = _count_leaks(audit) if audit is not None else None
    statuses = (dict(Counter(v["status"] for v in audit.values() if isinstance(v, dict) and "status" in v))
                if isinstance(audit, dict) else None)
    checks = {
        "facts_jaccard": {"pass": not facts_bad, "max_bcd": max((j["max_jaccard"] for j in jac
                                                                 if j["strata"] != "control"), default=0.0),
                          "violations": facts_bad, "max_valid_vs_train": round(valid_jac, 3)},
        "key_not_in_question": {"pass": not key_in_q, "violations": key_in_q},
        "templates_disjoint": {"pass": not tmpl, "overlap": sorted(tmpl)},
        "style_jaccard": {"pass": not st_bad, "max": max(st_jac.values(), default=0.0), "violations": st_bad},
        "style_topics_disjoint": {"pass": not topics, "overlap": sorted(topics)},
        "style_shots_not_in_eval": {"pass": not set(shots) & {r["id"] for r in st_eval}, "shot_ids": shots},
        "audit": {"pass": leaks == 0 if leaks is not None else None, "leaks": leaks, "statuses": statuses,
                  "not_ok": sorted(k for k, v in audit.items() if isinstance(v, dict) and v.get("status") not in
                                   (None, "OK")) if isinstance(audit, dict) else None,
                  "note": None if leaks is not None else "data/audit.json not found yet"},
    }
    return {"pass": all(c["pass"] is not False for c in checks.values())
            and checks["audit"]["pass"] is not None, "checks": checks}


def g1_items(root: Path = ROOT) -> list[dict[str, Any]]:
    """One question per fact: valid V1 for the 30 train facts, the control eval question for the 10 control."""
    rows = [{"id": r["id"], "fact_id": r["fact_id"], "user": r["question"], "answer_keys": r["answer_keys"]}
            for r in read_jsonl(root / "data/facts/valid.jsonl")]
    rows += [{"id": r["id"], "fact_id": r["fact_ids"][0], "user": r["question"], "answer_keys": r["answer_keys"]}
             for r in read_jsonl(root / "data/facts/eval_gen.jsonl") if r["strata"] == "control"]
    return rows


def gate_g1_g3_generate(root: Path = ROOT) -> dict[str, Any]:
    """Runs the base model: G1 checks and G3 samples (written to results/g3_samples.jsonl for labelling)."""
    from core import prompts
    from eval.generate import generate, load_model, unload
    from eval.registry import MAX_TOKENS, SETS
    from gen.style_proxies import check

    model, tok = load_model("base")
    items = g1_items(root)
    res: dict[str, Any] = {}
    for name, system in (("tag", prompts.TAG_FACTS), ("neutral", prompts.NEUTRAL)):
        gens = generate(model, tok, [system] * len(items), [i["user"] for i in items], MAX_TOKENS["facts"])
        guessed = [i["fact_id"] for i, g in zip(items, gens) if keys_match(i["answer_keys"], g["answer"])]
        res[f"no_rag_{name}"] = {"pass": len(guessed) <= G1_MAX_CORRECT, "correct": len(guessed),
                                 "n": len(items), "guessed_facts": guessed}
    a = [it for it in SETS["facts"].items(root) if it["strata"] == "a"]
    gens = generate(model, tok, [prompts.FACTS_RAG] * len(a), [i["user"] for i in a], MAX_TOKENS["facts"])
    acc = sum(keys_match(i["answer_keys"], g["answer"], i["require_all"]) for i, g in zip(a, gens)) / len(a)
    res["rag_strata_a"] = {"pass": acc >= G1_RAG_MIN, "acc": round(acc, 4), "n": len(a)}
    g1 = {"pass": all(v["pass"] for v in res.values()), "checks": res}

    # G3 samples: style valid (20) + first train questions that are not few-shot examples, alternating prompts
    shots = set(prompts.style_shot_ids(root))
    qs = read_jsonl(root / "data/style/valid.jsonl")
    qs += [r for r in sorted(read_jsonl(root / "data/style/train.jsonl"), key=lambda r: r["id"])
           if r["id"] not in shots][:G3_N - len(qs)]
    systems = [prompts.TAG_STYLE if i % 2 == 0 else prompts.STYLE_GOOD for i in range(len(qs))]
    gens = generate(model, tok, systems, [q["question"] for q in qs], MAX_TOKENS["style"])
    unload(model)
    samples = [{"id": f"G3-{i + 1:02d}", "source_id": q["id"], "prompt": "tag" if i % 2 == 0 else "good",
                "question": q["question"], "answer": g["answer"], "proxies": check(g["answer"])}
               for i, (q, g) in enumerate(zip(qs, gens))]
    write_jsonl(root / "results/g3_samples.jsonl", samples)
    return {"G1": g1, "G3_samples": {"path": "results/g3_samples.jsonl", "n": len(samples)}}


def gate_g3(labels_path: Path, root: Path = ROOT) -> dict[str, Any]:
    """Agreement of each proxy rule with manual labels."""
    from eval.scorers.style import RULES

    samples = {s["id"]: s for s in read_jsonl(root / "results/g3_samples.jsonl")}
    labels = read_jsonl(labels_path)
    per_rule = {}
    for rule in RULES:
        pairs = [(bool(samples[lab["id"]]["proxies"][rule]), bool(lab["labels"][rule])) for lab in labels]
        agree = sum(p == h for p, h in pairs)
        per_rule[rule] = {"agree": agree, "n": len(pairs), "pass": agree >= G3_MIN_AGREE,
                          "disagree_ids": [lab["id"] for lab, (p, h) in zip(labels, pairs) if p != h]}
    return {"pass": all(v["pass"] for v in per_rule.values()) and len(labels) >= G3_N, "rules": per_rule}


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--with-model", action="store_true")
    ap.add_argument("--g3-labels", type=Path)
    args = ap.parse_args()
    path = ROOT / "results/gates.json"
    out = read_json(path) if path.exists() else {}
    out["G2"] = gate_g2()
    if args.with_model:
        out |= gate_g1_g3_generate()
    if args.g3_labels:
        out["G3"] = gate_g3(args.g3_labels)
    write_json(path, out)
    for k, v in out.items():
        print(k, "PASS" if v.get("pass") else ("—" if "pass" not in v else "FAIL"))
    return 0 if all(v.get("pass", True) for v in out.values()) else 1


if __name__ == "__main__":
    sys.exit(main())
