"""Pre-registration of Day9 -> results/prereg.json (plan §4.2, §5.9).

usage (from Day9/):
  python prereg.py write --stage v1     before the first teacher call: hashes, teacher, rules, gates, budget
  python prereg.py write --stage v2     after the data gates: hashes of train/valid, filter report, spend
  python prereg.py freeze teacher_reasoning off --reason "G1 rule: ..."   dated amendment + frozen value
  python prereg.py freeze KEY VALUE --supersede --reason "..."           replace a frozen value (old kept)
  python prereg.py amend "text"         dated free-text amendment
  python prereg.py rehash --reason "..."   dated amendment that re-pins the hashes of the changed files
                                        (must name why; old and new hashes are both kept)
  python prereg.py check                hash mismatches (exit 1 if any)
A stage is written once; changes after that are amendments only.
"""

from __future__ import annotations

import argparse
import datetime as dt
import sys
from pathlib import Path
from typing import Any

from core.hashes import check_prereg, current_hashes
from core.io import ROOT, git_hash, read_json, write_json

PATH = ROOT / "results/prereg.json"
FREEZABLE = ("teacher_reasoning", "teacher_provider", "student_iters", "smoke_lr")


def _now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds")


def load(path: Path = PATH) -> dict[str, Any]:
    return read_json(path) if path.exists() else {"stages": {}, "frozen": {}, "amendments": []}


def stage_v1(root: Path = ROOT) -> dict[str, Any]:
    import distill
    import gates
    from core.config import load_teacher_config, load_train_config
    from eval import cascade
    from eval.registry import BASELINE, OPTIONAL, REASONING_ON, REQUIRED, TEACHER_G1
    from gen import gen_situations, make_cells
    from select_ckpt import RULE as SELECT_RULE
    from teacher import budget

    tok_path = root / "results/prompt_tokens.json"
    return {
        "created": _now(), "git_head": git_hash(root), "hashes": current_hashes(root),
        "prompt_tokens": read_json(tok_path) if tok_path.exists() else None,
        "teacher": load_teacher_config(root / "configs/teacher.yaml"),
        "teacher_decoding": {"eval": "T=0, seed 42", "reasoning_on_means": f"effort={REASONING_ON}"},
        "reasoning_rule": {"min_delta_both": gates.REASONING_MIN_DELTA, "format_ok_on_min": gates.FORMAT_OK_MIN,
                           "forecast_max_usd": gates.REASONING_FORECAST_MAX,
                           "text": "on only if Δboth(on−off) ≥ +0.07 on eval 57, format_ok(on) ≥ 0.95 and the "
                                   "full-budget forecast ≤ $1.6; otherwise off; frozen by a dated amendment"},
        "gates": {"G0": {"sha256": gates.G0_SHA256, "both": gates.G0_EXPECTED, "tol": gates.G0_TOL},
                  "G1": {"ci_lb": "> 0", "mcnemar_p": f"< {gates.G1_P}", "format_ok": f">= {gates.FORMAT_OK_MIN}",
                         "teacher_gold_valid_ref": gates.G1_TEACHER_GOLD_REF},
                  "G2": gates.G2, "G3": {"jaccard_eval": 0.3, "jaccard_valid": 0.3, "jaccard_intra": 0.5,
                                         "semantic_audit": "eval × top-10 candidates, LEAK rows removed from train"},
                  "G4": {"label_agreement_min": gates.G4_MIN_AGREE, "n_valid": 20, "decoding": "greedy, no cache"},
                  "G5": "student+list > base+list (LB > 0) and >= base+good",
                  "G6": {"margin": gates.G6_MARGIN, "format_ok": gates.FORMAT_OK_MIN},
                  "G7": {"margin": gates.G7_MARGIN, "set": "ood 100 strict"},
                  "G8": "prereg hashes, select and τ without eval, budget <= $2"},
        "replaces_definition": "student+list replaces teacher+good if G6 PASS: LB of the paired bootstrap CI "
                               "(10k, seed 42) of both_acc(student+list − teacher+good) on eval 57 ≥ −10 p.p. and "
                               "student format_ok ≥ 0.95",
        "labelling": {"gold_temps": distill.GOLD_TEMPS, "new_extra_temps": distill.NEW_EXTRA_TEMPS,
                      "seed": f"{distill.SEED} + sample_idx", "max_tokens": distill.MAX_TOKENS,
                      "rule": distill.__doc__.split("Labelling rules (prereg v1):", 1)[1].strip()},
        "filter": {"rules": distill.FILTER_RULES, "chars": [distill.MIN_CHARS, distill.MAX_CHARS],
                   "latin_max": distill.MAX_LATIN, "sentences": distill.SENTENCES, "dedup": "normalized Обоснование",
                   "reference_only": "train_formatonly.jsonl (format rules only) is not trained on"},
        "data_plan": {"cells": make_cells.summary(make_cells.make_cells()), "verdict_plan": make_cells.VERDICT_PLAN,
                      "valid": f"30 orig + {distill.N_VALID_NEW} new (first accepted in a seed-{distill.SEED} order "
                               f"of {distill.VALID_CANDIDATES} candidates drawn before labelling)",
                      "train": "249 orig + ~400 new, multiple of 4", "generation": {
                          "temperature": gen_situations.TEMPERATURE, "max_tokens": gen_situations.MAX_TOKENS,
                          "excerpt_chars": gen_situations.EXCERPT_CHARS}},
        "recipe": {name: load_train_config(root / f"configs/{name}.yaml")
                   for name in ("smoke", "student", "fallback_F1", "fallback_F2")},
        "smoke_rule": "loss falls, no NaN, no jump ×2, format_ok ≥ 0.8 on 20 valid; else one restart with lr 2.0e-5",
        "checkpoint_selection": SELECT_RULE,
        "fallback": {"trigger": gates.FALLBACK, "F1": "configs/fallback_F1.yaml", "F2": "configs/fallback_F2.yaml",
                     "max": 2, "decision_on": "valid only"},
        "cascade": {"tau_rule": f"minimal defer rate on valid 60 with accuracy ≥ teacher − {cascade.TOL}",
                    "eval": "report only"},
        "cells": {"baseline": BASELINE, "teacher_g1": TEACHER_G1, "required": REQUIRED, "optional": OPTIONAL},
        "stats": {"bootstrap_B": 10_000, "seed": 42, "alpha": 0.05, "mcnemar": "exact two-sided"},
        "budget": {"limit_usd": budget.LIMIT_USD, "soft_limit_usd": budget.SOFT_LIMIT_USD,
                   "privileged_steps": sorted(budget.PRIVILEGED_STEPS), "forecast_max_usd": budget.FORECAST_MAX_USD},
    }


def stage_v2(root: Path = ROOT, path: Path = PATH) -> dict[str, Any]:
    from teacher.budget import LIMIT_USD, PRIVILEGED_STEPS, Budget, derived_limits

    stats = read_json(root / "data/distill/build_stats.json")
    audit = read_json(root / "data/distill/audit.json")
    g3a = read_json(root / "results/g3a.json")
    gates = read_json(root / "results/gates.json")
    data = load(path)
    active_limit = next((a["budget_override"]["limit_usd"] for a in reversed(data["amendments"])
                         if "budget_override" in a), LIMIT_USD)
    spent = Budget.spent_in_log(root / "results/teacher_usage.jsonl")
    budget = derived_limits(active_limit) | {"privileged_steps": sorted(PRIVILEGED_STEPS), "spent_usd": spent}
    return {"created": _now(), "git_head": git_hash(root), "hashes": current_hashes(root),
            "data_sizes": {"train": stats["train"], "valid": stats["valid"]},
            "filter_stats": {"rejected": stats["rejected"],
                             "normalized_principle_line": stats["normalized_principle_line"], "cells": stats["cells"]},
            "build_stats": stats,
            "audit_stats": {"g3a": {"n_new": g3a["n_new"],
                                    "violations": {k: len(g3a[k]) for k in ("eval", "eval_answers", "valid", "intra")}},
                            "g3b_summary": audit["summary"], "g3b_coverage": audit["coverage"],
                            "leaks_drop": read_json(root / "data/distill/leaks_drop.json")},
            "gates": {k: gates[k] for k in ("G2", "G3", "G4")},
            "budget": budget, "spent_usd": spent}


def write_stage(stage: str, root: Path = ROOT, path: Path = PATH, allow_unpinned: bool = False) -> dict[str, Any]:
    data = load(path)
    if stage in data["stages"]:
        raise SystemExit(f"stage {stage} already written; use amend")
    if stage == "v1":
        content = stage_v1(root)
        teacher = content["teacher"]
        if (teacher.get("backend", "openrouter") == "openrouter" and not teacher["provider_order"]
                and not allow_unpinned):
            raise SystemExit("configs/teacher.yaml: provider_order is empty; pin the provider before prereg v1")
    elif stage == "v2":
        if "v1" not in data["stages"]:
            raise SystemExit("v2 needs v1")
        content = stage_v2(root, path)
    else:
        raise SystemExit(f"unknown stage {stage}")
    data["stages"][stage] = content
    write_json(path, data)
    return content


def freeze(key: str, value: str, reason: str, path: Path = PATH, supersede: bool = False) -> None:
    """Freeze a value once. supersede=True replaces an earlier frozen value; the old one stays in the amendment."""
    if key not in FREEZABLE:
        raise SystemExit(f"{key} is not freezable: {FREEZABLE}")
    if not reason.strip():
        raise SystemExit("freeze needs a reason")
    data = load(path)
    old = data["frozen"].get(key)
    if old is not None and not supersede:
        raise SystemExit(f"{key} already frozen as {old!r} (use --supersede with a reason)")
    if old is None and supersede:
        raise SystemExit(f"{key} is not frozen yet: nothing to supersede")
    data["frozen"][key] = value
    entry: dict[str, Any] = {"date": _now(), "freeze": {key: value}, "reason": reason}
    if old is not None:
        entry["superseded"] = {key: old}
    data["amendments"].append(entry)
    write_json(path, data)


def amend(text: str, path: Path = PATH) -> None:
    data = load(path)
    data["amendments"].append({"date": _now(), "text": text})
    write_json(path, data)


def amend_budget(limit_usd: float, reason: str, path: Path = PATH) -> dict[str, Any]:
    """Append an active budget override without rewriting the historical v1 budget."""
    from teacher.budget import LIMIT_USD, derived_limits

    if limit_usd <= 0 or not reason.strip():
        raise SystemExit("budget amendment needs a positive limit and a reason")
    data = load(path)
    previous = next((a["budget_override"]["limit_usd"] for a in reversed(data["amendments"])
                     if "budget_override" in a), LIMIT_USD)
    override = derived_limits(limit_usd) | {"previous_limit_usd": previous, "repeat_g2_correction": True}
    data["amendments"].append({"date": _now(), "reason": reason, "budget_override": override})
    write_json(path, data)
    return override


def rehash(reason: str, root: Path = ROOT, path: Path = PATH) -> dict[str, dict[str, str]]:
    """Re-pin the hashes of changed files by a dated amendment; no-op error if nothing changed."""
    from core.hashes import expected_hashes

    if not reason.strip():
        raise SystemExit("rehash needs a reason")
    data = load(path)
    if not data["stages"]:
        raise SystemExit("rehash needs a written stage")
    want, have = expected_hashes(data), current_hashes(root)
    changed = {k: {"old": v, "new": have.get(k)} for k, v in want.items() if have.get(k) != v}
    if not changed:
        raise SystemExit("nothing to rehash")
    if missing := [k for k, v in changed.items() if v["new"] is None]:
        raise SystemExit(f"files disappeared, cannot rehash: {missing}")
    data["amendments"].append({"date": _now(), "rehash": {k: v["new"] for k, v in changed.items()},
                               "rehash_old": {k: v["old"] for k, v in changed.items()}, "reason": reason})
    write_json(path, data)
    return changed


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    w = sub.add_parser("write")
    w.add_argument("--stage", required=True, choices=("v1", "v2"))
    w.add_argument("--allow-unpinned", action="store_true")
    f = sub.add_parser("freeze")
    f.add_argument("key")
    f.add_argument("value")
    f.add_argument("--reason", required=True)
    f.add_argument("--supersede", action="store_true", help="replace an earlier frozen value (kept in history)")
    a = sub.add_parser("amend")
    a.add_argument("text")
    r = sub.add_parser("rehash")
    r.add_argument("--reason", required=True)
    sub.add_parser("check")
    args = ap.parse_args(argv)
    if args.cmd == "write":
        write_stage(args.stage, allow_unpinned=args.allow_unpinned)
        print(f"prereg {args.stage} written to {PATH}")
    elif args.cmd == "freeze":
        freeze(args.key, args.value, args.reason, supersede=args.supersede)
    elif args.cmd == "amend":
        amend(args.text)
    elif args.cmd == "rehash":
        for k, v in rehash(args.reason).items():
            print(f"rehash {k}: {v['old'][:12]} -> {v['new'][:12]}")
    else:
        bad = check_prereg()
        print(bad or "OK")
        return 1 if bad else 0
    return 0


if __name__ == "__main__":
    sys.exit(main())
