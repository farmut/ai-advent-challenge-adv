"""Leak checks between the distillation data and eval / valid (plan §5.3, §5.7).

usage (from Day9/):
  python leaks.py g3a          lexical: char-3-gram Jaccard of new situations ≤ 0.3 to eval and valid,
                               ≤ 0.5 inside train (later duplicates dropped) -> results/g3a.json, data/distill/leaks_drop.json
  python leaks.py candidates   top-10 nearest train rows for every eval row -> data/distill/audit_candidates.json
                               (input of the semantic audit G3b by a Claude subagent, format of Day7 eval_audit.json)
  python leaks.py aggregate-g3b  validate and merge four independent shards -> data/distill/audit.json
  python leaks.py apply-audit  LEAK verdicts of data/distill/audit.json -> appended to leaks_drop.json
This is the only data-building module that reads the eval set, and only to remove train rows: eval never changes.
"""

from __future__ import annotations

import argparse
from collections.abc import Mapping
from pathlib import Path
from typing import Any

from core.io import ROOT, read_json, read_jsonl, write_json
from core.text import char_ngrams, jaccard

EVAL_MAX = 0.3
VALID_MAX = 0.3
INTRA_MAX = 0.5
TOP_K = 10
DATA = ROOT / "data/distill"
GENERATED_PREFIXES = ("NS-", "NS2-", "NS3-")


def is_generated(row_id: str) -> bool:
    return row_id.startswith(GENERATED_PREFIXES)


def grams(texts: Mapping[str, str]) -> dict[str, set[str]]:
    return {k: char_ngrams(v) for k, v in texts.items()}


def max_sim(g: set[str], refs: Mapping[str, set[str]]) -> tuple[str | None, float]:
    best_id, best = None, 0.0
    for rid, rg in refs.items():
        j = jaccard(g, rg)
        if j > best:
            best_id, best = rid, j
    return best_id, best


def violations(texts: Mapping[str, str], refs: Mapping[str, str], threshold: float) -> list[dict[str, Any]]:
    """Rows whose max Jaccard to any reference is strictly above the threshold."""
    rg = grams(refs)
    out = []
    for tid, g in grams(texts).items():
        ref_id, j = max_sim(g, rg)
        if j > threshold:
            out.append({"id": tid, "ref": ref_id, "jaccard": round(j, 4)})
    return out


def dedup_within(texts: Mapping[str, str], threshold: float = INTRA_MAX,
                 keep_first: tuple[str, ...] = ("FT-", "FV-")) -> list[dict[str, Any]]:
    """Greedy dedup in id order (orig rows first): a row too close to an already kept row is dropped."""
    order = sorted(texts, key=lambda i: (not i.startswith(keep_first), i))
    kept: dict[str, set[str]] = {}
    dropped = []
    for tid in order:
        g = char_ngrams(texts[tid])
        ref_id, j = max_sim(g, kept)
        if j > threshold and not tid.startswith(keep_first):
            dropped.append({"id": tid, "ref": ref_id, "jaccard": round(j, 4)})
        else:
            kept[tid] = g
    return dropped


def top_k(queries: Mapping[str, str], pool: Mapping[str, str], k: int = TOP_K) -> dict[str, list[dict[str, Any]]]:
    """For every query the k most similar pool rows by char-3-gram Jaccard (ties by id)."""
    pg = grams(pool)
    out = {}
    for qid, q in grams(queries).items():
        scored = sorted(((round(jaccard(q, g), 4), pid) for pid, g in pg.items()), key=lambda x: (-x[0], x[1]))
        out[qid] = [{"id": pid, "jaccard": j} for j, pid in scored[:k]]
    return out


def audit_leaks(audit: Mapping[str, Any]) -> set[str]:
    """Unique train IDs marked LEAK, in either the Day7 list format or the per-eval mapping format."""
    if "leaks" in audit:
        return {tid for entry in audit["leaks"] for tid in entry["train"]}
    out = set()
    for entry in audit.values():
        for tid, verdict in (entry.get("candidates") or {}).items():
            v = verdict.get("status") if isinstance(verdict, dict) else verdict
            if str(v).strip().upper() == "LEAK":
                out.add(tid)
    return out


def aggregate_audit(candidates: Mapping[str, list[dict[str, Any]]], shards: list[dict[str, Any]]) -> dict[str, Any]:
    """Validate exact candidate coverage and merge independent shard decisions; LEAK wins a conflicting duplicate."""
    expected = {(ev, c["id"]) for ev, cs in candidates.items() for c in cs}
    decisions: dict[tuple[str, str], dict[str, Any]] = {}
    duplicates: list[dict[str, str]] = []
    conflicts: list[dict[str, str]] = []
    received = 0
    for shard in shards:
        for d in shard["decisions"]:
            received += 1
            pair = (d["eval"], d["train"])
            if pair not in expected:
                raise ValueError(f"unexpected audit decision: {pair}")
            if d.get("status") not in {"OK", "LEAK", "NOT_IN_DATA"}:
                raise ValueError(f"invalid audit status for {pair}: {d.get('status')}")
            if not d.get("why", "").strip():
                raise ValueError(f"empty audit rationale: {pair}")
            if pair in decisions:
                duplicates.append({"eval": pair[0], "train": pair[1]})
                if decisions[pair]["status"] != d["status"]:
                    conflicts.append({"eval": pair[0], "train": pair[1]})
                if d["status"] != "LEAK":
                    continue
            decisions[pair] = {"status": d["status"], "why": d["why"].strip()}
    missing = sorted(expected - decisions.keys())
    if missing:
        raise ValueError(f"missing audit decisions: {missing[:10]} ({len(missing)} total)")
    counts = {s: sum(d["status"] == s for d in decisions.values()) for s in ("OK", "LEAK", "NOT_IN_DATA")}
    leaks = [{"eval": ev, "train": [tid], "why": d["why"]}
             for (ev, tid), d in sorted(decisions.items()) if d["status"] == "LEAK"]
    leak_ids = sorted(audit_leaks({"leaks": leaks}))
    return {"leaks": leaks,
            "summary": {"eval_rows": len(candidates), "decisions": len(decisions), **counts,
                        "leak_eval_rows": len({x["eval"] for x in leaks}), "unique_leak_train_ids": len(leak_ids)},
            "coverage": {"expected": len(expected), "received": received, "unique": len(decisions),
                         "missing": [], "unexpected": [], "duplicates": duplicates, "conflicts": conflicts},
            "methodology": {
                "candidate_source": "data/distill/audit_candidates.json: top-10 char-3-gram candidates per eval row",
                "decision_rule": "Independent semantic comparison of the decisive situation and teacher answer; LEAK wins conflicts",
                "removal_rule": "Every unique LEAK train/valid ID is dropped; OK and NOT_IN_DATA are retained",
                "required_coverage": "57 eval rows × 10 candidates = 570 decisions"}}


def _labeled(root: Path) -> list[dict[str, Any]]:
    path = root / "data/distill/labeled.jsonl"
    if path.exists():
        return read_jsonl(path)
    return [{"id": r["cell_id"], "user": r["situation"]} for r in read_jsonl(root / "data/distill/situations_raw.jsonl")]


def _train_situations(root: Path) -> dict[str, str]:
    return {r["id"]: r["user"] for r in _labeled(root)}


def _train_answers(root: Path) -> dict[str, str]:
    """Обоснование + Рекомендация of the teacher (a leak can also come through the answer)."""
    return {r["id"]: " ".join(r["answer"].splitlines()[2:]) for r in _labeled(root) if r.get("answer")}


def _eval_rows(root: Path) -> list[dict[str, Any]]:
    return read_jsonl(root / "data/format/eval.jsonl")


def _valid_texts(root: Path) -> dict[str, str]:
    return {f"FV-{i:03d}": r["messages"][1]["content"]
            for i, r in enumerate(read_jsonl(root / "data/format/valid.jsonl"), 1)}


def main(argv: list[str] | None = None, root: Path = ROOT) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("cmd", choices=("g3a", "candidates", "aggregate-g3b", "apply-audit"))
    args = ap.parse_args(argv)
    drop_path = root / "data/distill/leaks_drop.json"
    drop = set(read_json(drop_path)) if drop_path.exists() else set()
    if args.cmd == "aggregate-g3b":
        candidates = read_json(root / "data/distill/audit_candidates.json")
        shards = [read_json(root / f"data/distill/audit_shard_{i}.json") for i in range(1, 5)]
        audit = aggregate_audit(candidates, shards)
        if audit["coverage"]["duplicates"]:
            raise SystemExit(f"duplicate shard decisions: {audit['coverage']['duplicates'][:10]}")
        audit["shards"] = [{"path": f"data/distill/audit_shard_{i}.json",
                            "input": f"data/distill/audit_input_shard_{i}.json",
                            "range": shard["range"], "decisions": len(shard["decisions"])}
                           for i, shard in enumerate(shards, 1)]
        write_json(root / "data/distill/audit.json", audit)
        print(audit["summary"])
        return 0
    if args.cmd == "g3a":
        sit = {k: v for k, v in _train_situations(root).items() if not k.startswith("FV-")}
        new = {k: v for k, v in sit.items() if is_generated(k)}
        ans = {k: v for k, v in _train_answers(root).items() if not k.startswith("FV-")}
        ev_rows = _eval_rows(root)
        ev = violations(new, {r["id"]: r["situation"] for r in ev_rows}, EVAL_MAX)
        ev_ans = violations(ans, {r["id"]: r["justification"] + " " + r["recommendation"] for r in ev_rows}, EVAL_MAX)
        va = violations(new, _valid_texts(root), VALID_MAX)
        intra = dedup_within(sit)
        drop |= {v["id"] for v in ev + ev_ans + va + intra}
        write_json(root / "results/g3a.json", {"eval": ev, "eval_answers": ev_ans, "valid": va, "intra": intra,
                                               "n_new": len(new), "thresholds": {"eval": EVAL_MAX, "valid": VALID_MAX,
                                                                                 "intra": INTRA_MAX}})
        print({"eval": len(ev), "eval_answers": len(ev_ans), "valid": len(va), "intra": len(intra)})
    elif args.cmd == "candidates":
        ans = _train_answers(root)
        train = {k: v + " " + ans.get(k, "") for k, v in _train_situations(root).items()
                 if k not in drop and not k.startswith("FV-")}
        queries = {r["id"]: f"{r['situation']} {r['justification']} {r['recommendation']}" for r in _eval_rows(root)}
        write_json(root / "data/distill/audit_candidates.json", top_k(queries, train))
        print(f"candidates for {len(queries)} eval rows")
        return 0
    else:
        leaks = audit_leaks(read_json(root / "data/distill/audit.json"))
        drop |= leaks
        print({"audit_leaks": len(leaks)})
    write_json(drop_path, sorted(drop))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
