"""Gates G0–G8, the reasoning rule and the fallback trigger of Day9 (plan §4–§6). Thresholds are frozen in prereg v1.

Pure functions take per-item booleans / rates, so the boundaries are unit-tested (tests/test_gates.py).
CLI (from Day9/), results merged into results/gates.json:
  python gates.py g0                   base SHA-256 + prompt/eval hashes (+ regression from results/main if present)
  python gates.py g1   [--run main]    teacher+good vs base+good on eval 57 (reasoning frozen by the rule)
  python gates.py g2                   data gate from data/distill (labeled, train) and the teacher usage log
  python gates.py g3                   complete 570-decision G3b audit and lexical/semantic leak removal
  python gates.py post [--run main]    G5–G8 from the block D cells
Boundaries: strict where the plan says ">" / "<", inclusive where it says "≥" / "≤".
"""

from __future__ import annotations

import argparse
from collections import Counter
from collections.abc import Sequence
from pathlib import Path
from typing import Any

from core.io import ROOT, read_json, read_jsonl, sha256_file, write_json
from eval.stats import mcnemar, non_inferior, paired_bootstrap

EPS = 1e-9

G0_SHA256 = "281d68db1c0d537bf1e19b0744ff2c3dc26e7c86ca5b7deae51935266c97ac58"
G0_EXPECTED, G0_TOL = 0.439, 0.035
G1_P = 0.05
FORMAT_OK_MIN = 0.95
G1_TEACHER_GOLD_REF = 0.60                  # reference only (30 valid)
REASONING_MIN_DELTA = 0.07                  # 4/57
REASONING_FORECAST_MAX = 1.60
G2 = {"api_error_max": 0.02, "reject_max": 0.50, "train_min": 400, "verdict_share_min": 0.20,
      "principle_min_rows": 20}
G4_MIN_AGREE = 0.90
G6_MARGIN = -0.10
G7_MARGIN = -0.05
BUDGET_LIMIT = 2.00
FALLBACK = {"principle_abs_min": 0.60, "principle_over_base_min": 0.10}


def _rate(xs: Sequence[bool]) -> float:
    return sum(map(bool, xs)) / len(xs) if xs else 0.0


def gate_g0(sha256: str, both_acc: float | None, hash_mismatch: Sequence[str] = ()) -> dict[str, Any]:
    checks = {"sha256": sha256 == G0_SHA256,
              "regression": None if both_acc is None else abs(both_acc - G0_EXPECTED) <= G0_TOL + EPS,
              "hashes": not hash_mismatch}
    return {"pass": all(v is True for v in checks.values()), "checks": checks, "both_acc": both_acc,
            "expected": f"{G0_EXPECTED}±{G0_TOL}", "hash_mismatch": list(hash_mismatch)}


def gate_g1(teacher_both: Sequence[bool], base_both: Sequence[bool], teacher_format_ok: float,
            clusters: Sequence[str] | None = None) -> dict[str, Any]:
    """teacher+good significantly better than base+good: LB of the bootstrap CI > 0, McNemar p < 0.05,
    teacher format_ok ≥ 0.95."""
    ci = paired_bootstrap([float(x) for x in base_both], [float(x) for x in teacher_both], clusters)
    mn = mcnemar(teacher_both, base_both)
    checks = {"ci_lb_gt_0": ci["lo"] > 0, "mcnemar_p_lt_0.05": mn["p"] < G1_P,
              "format_ok_ge_0.95": teacher_format_ok >= FORMAT_OK_MIN - EPS}
    return {"pass": all(checks.values()), "checks": checks, "ci": ci, "mcnemar": mn,
            "teacher_both": _rate(teacher_both), "base_both": _rate(base_both), "teacher_format_ok": teacher_format_ok}


def reasoning_decision(both_off: Sequence[bool], both_on: Sequence[bool], format_ok_on: float,
                       forecast_usd: float) -> dict[str, Any]:
    """Reasoning on only if Δboth(on − off) ≥ +0.07, format_ok(on) ≥ 0.95 and the full-budget forecast ≤ $1.6."""
    if len(both_off) != len(both_on) or not both_off:
        raise ValueError("paired non-empty sequences expected")
    delta = _rate(both_on) - _rate(both_off)
    checks = {"delta_ge_0.07": delta >= REASONING_MIN_DELTA - EPS, "format_ok_on_ge_0.95": format_ok_on >= FORMAT_OK_MIN - EPS,
              "forecast_le_1.6": forecast_usd <= REASONING_FORECAST_MAX + EPS}
    return {"reasoning": "on" if all(checks.values()) else "off", "delta": round(delta, 4), "checks": checks,
            "forecast_usd": forecast_usd}


def gate_g2(n_calls: int, n_api_errors: int, n_candidates: int, n_rejected: int,
            train_labels: Sequence[tuple[str, str]], verdicts: Sequence[str], principles: Sequence[str]
            ) -> dict[str, Any]:
    """train_labels: (verdict, principle) of every train row."""
    n = len(train_labels)
    v = Counter(x for x, _ in train_labels)
    p = Counter(y for _, y in train_labels)
    err = n_api_errors / n_calls if n_calls else 0.0
    rej = n_rejected / n_candidates if n_candidates else 0.0
    checks = {"api_errors": err <= G2["api_error_max"] + EPS, "reject": rej <= G2["reject_max"] + EPS,
              "train_size": n >= G2["train_min"],
              "verdict_share": all(v.get(x, 0) / n >= G2["verdict_share_min"] - EPS for x in verdicts) if n else False,
              "principle_rows": all(p.get(x, 0) >= G2["principle_min_rows"] for x in principles)}
    return {"pass": all(checks.values()), "checks": checks, "api_error_rate": round(err, 4),
            "reject_rate": round(rej, 4), "train": n, "by_verdict": dict(v), "by_principle": dict(p)}


def gate_g3(g3a_left: int, audit_leaks_left: int, audit_done: bool) -> dict[str, Any]:
    checks = {"lexical": g3a_left == 0, "semantic_audit_done": audit_done, "semantic": audit_leaks_left == 0}
    return {"pass": all(checks.values()), "checks": checks}


def gate_g4(run_a: Sequence[tuple[str | None, str | None]], run_b: Sequence[tuple[str | None, str | None]],
            texts_a: Sequence[str] = (), texts_b: Sequence[str] = ()) -> dict[str, Any]:
    """Determinism of the teacher: share of identical (verdict, principle) in two cold greedy runs ≥ 0.9."""
    if len(run_a) != len(run_b) or not run_a:
        raise ValueError("paired non-empty runs expected")
    agree = sum(a == b and a[0] is not None for a, b in zip(run_a, run_b)) / len(run_a)
    same_text = (sum(x == y for x, y in zip(texts_a, texts_b)) / len(texts_a)) if texts_a else None
    return {"pass": agree >= G4_MIN_AGREE - EPS, "label_agreement": round(agree, 4), "text_identical": same_text}


def gate_g5(student_list: Sequence[bool], base_list: Sequence[bool], base_good: Sequence[bool]) -> dict[str, Any]:
    """student+list > base+list (LB > 0) and student+list ≥ base+good (point estimate)."""
    ci = paired_bootstrap([float(x) for x in base_list], [float(x) for x in student_list])
    checks = {"vs_base_list_lb_gt_0": ci["lo"] > 0, "ge_base_good": _rate(student_list) >= _rate(base_good) - EPS}
    return {"pass": all(checks.values()), "checks": checks, "ci_vs_base_list": ci,
            "ci_vs_base_good": paired_bootstrap([float(x) for x in base_good], [float(x) for x in student_list]),
            "mcnemar_vs_base_list": mcnemar(student_list, base_list), "mcnemar_vs_base_good": mcnemar(student_list, base_good)}


def gate_g6(student_list: Sequence[bool], teacher_good: Sequence[bool], student_format_ok: float) -> dict[str, Any]:
    """"Replaces the teacher": LB of CI(student+list − teacher+good) ≥ −10 p.p. and student format_ok ≥ 0.95."""
    ci = paired_bootstrap([float(x) for x in teacher_good], [float(x) for x in student_list])
    checks = {"non_inferior_10pp": non_inferior(ci, G6_MARGIN - EPS), "format_ok_ge_0.95": student_format_ok >= FORMAT_OK_MIN - EPS}
    return {"pass": all(checks.values()), "checks": checks, "ci": ci, "mcnemar": mcnemar(student_list, teacher_good)}


def gate_g7(student_ood: Sequence[bool], base_ood: Sequence[bool]) -> dict[str, Any]:
    ci = paired_bootstrap([float(x) for x in base_ood], [float(x) for x in student_ood])
    return {"pass": non_inferior(ci, G7_MARGIN - EPS), "ci": ci}


def gate_g8(hash_mismatch: Sequence[str], select_read_eval: bool, tau_read_eval: bool, spent_usd: float
            ) -> dict[str, Any]:
    checks = {"hashes": not hash_mismatch, "select_without_eval": not select_read_eval,
              "tau_without_eval": not tau_read_eval, "budget": spent_usd <= BUDGET_LIMIT + EPS}
    return {"pass": all(checks.values()), "checks": checks, "hash_mismatch": list(hash_mismatch),
            "spent_usd": spent_usd}


def fallback_trigger(principle_acc_best: float, principle_acc_base_list: float) -> dict[str, Any]:
    """On valid only: fallback if principle_acc(best) < 0.60 or < principle_acc(base+list) + 0.10."""
    low_abs = principle_acc_best < FALLBACK["principle_abs_min"] - EPS
    low_rel = principle_acc_best < principle_acc_base_list + FALLBACK["principle_over_base_min"] - EPS
    return {"fallback": low_abs or low_rel, "below_abs": low_abs, "below_base_plus": low_rel,
            "best": principle_acc_best, "base_list": principle_acc_base_list}


# ---------- CLI ----------

def _cell(run: str, stem: str, root: Path = ROOT) -> dict[str, Any]:
    return read_json(root / "results" / run / f"{stem}.json")


def _both(cell: dict[str, Any]) -> list[bool]:
    return [bool(r["both"]) for r in cell["rows"]]


def teacher_stem(root: Path = ROOT) -> str:
    """File stem of the teacher+good cell with the reasoning level frozen after G1 (prereg 'frozen')."""
    path = root / "results/prereg.json"
    level = read_json(path).get("frozen", {}).get("teacher_reasoning", "off") if path.exists() else "off"
    return f"eval_format__teacher__good__r-{ {'on': 'low'}.get(level, level) }"


DATA_STEPS = ("C1", "C2", "C3")


def g2_counts(usage: Sequence[dict[str, Any]], labeled: Sequence[dict[str, Any]],
              train: Sequence[dict[str, Any]], valid: Sequence[dict[str, Any]]) -> dict[str, Any]:
    """Inputs of gate_g2. Calls = network calls of the data steps C1–C3 (cache hits excluded), an API error is a call
    that finally failed (status != 200 after retries). Candidates = labelled rows except the 30 orig valid rows
    (they always stay in valid); rejected = candidates that ended neither in train nor in valid (labelling
    disagreement, filter, dedup, leak, balance)."""
    calls = [u for u in usage if u.get("step") in DATA_STEPS and not u.get("from_cache")]
    kept = {r["id"] for r in train} | {r["id"] for r in valid}
    cands = [r for r in labeled if not (r.get("source") == "orig" and r.get("split") == "valid")]
    return {"n_calls": len(calls), "n_api_errors": sum(u.get("status") != 200 for u in calls),
            "n_candidates": len(cands), "n_rejected": sum(r["id"] not in kept for r in cands),
            "train_labels": [(r["meta"]["gold_verdict"], r["meta"]["gold_principle"]) for r in train]}


def merge(name: str, res: dict[str, Any], root: Path = ROOT) -> None:
    path = root / "results/gates.json"
    data = read_json(path) if path.exists() else {}
    data[name] = res
    write_json(path, data)
    print(name, "PASS" if res.get("pass", res.get("reasoning")) else "FAIL", res.get("checks"))


def main(argv: list[str] | None = None) -> int:
    from core.hashes import check_prereg
    from eval.generate import MODEL

    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("gate", choices=("g0", "g1", "g2", "g3", "post"))
    ap.add_argument("--run", default="main")
    ap.add_argument("--forecast", type=float, default=None, help="g1: full-budget forecast with reasoning on, $")
    args = ap.parse_args(argv)
    if args.gate == "g3":
        from leaks import audit_leaks

        d = ROOT / "data/distill"
        g3a = read_json(ROOT / "results/g3a.json")
        audit = read_json(d / "audit.json")
        drop = set(read_json(d / "leaks_drop.json"))
        final_ids = {r["id"] for name in ("train.jsonl", "valid.jsonl") for r in read_jsonl(d / name)}
        lexical_ids = {v["id"] for key in ("eval", "eval_answers", "valid", "intra") for v in g3a[key]}
        leak_ids = audit_leaks(audit)
        coverage = audit["coverage"]
        summary = audit["summary"]
        audit_done = (summary["eval_rows"] == 57 and summary["decisions"] == 570
                      and coverage["expected"] == coverage["received"] == coverage["unique"] == 570
                      and not coverage["missing"] and not coverage["unexpected"] and not coverage["duplicates"])
        lexical_left = sum(tid in final_ids for tid in lexical_ids)
        audit_left = sum(tid in final_ids or tid not in drop for tid in leak_ids)
        res = gate_g3(lexical_left, audit_left, audit_done)
        res |= {"audit": summary, "coverage": coverage, "lexical_drop": len(lexical_ids),
                "semantic_leak_ids": len(leak_ids), "leaks_drop_total": len(drop),
                "leak_ids_left_in_final": sorted(leak_ids & final_ids)}
        merge("G3", res)
        return 0 if res["pass"] else 6
    if args.gate == "g0":
        sha = sha256_file(Path(MODEL) / "model.safetensors")
        reg = ROOT / "results" / args.run / "eval_format__base__good.json"
        both = read_json(reg)["metrics"]["both_acc"] if reg.exists() else None
        res = gate_g0(sha, both, check_prereg())
        merge("G0", res)
        return 0 if res["pass"] or both is None else 1
    if args.gate == "g2":
        from common import PRINCIPLES, VERDICTS

        d = ROOT / "data/distill"
        c = g2_counts(read_jsonl(ROOT / "results/teacher_usage.jsonl"), read_jsonl(d / "labeled.jsonl"),
                      read_jsonl(d / "train.jsonl"), read_jsonl(d / "valid.jsonl"))
        res = gate_g2(c["n_calls"], c["n_api_errors"], c["n_candidates"], c["n_rejected"], c["train_labels"],
                      VERDICTS, [p["id"] for p in PRINCIPLES])
        res |= {k: v for k, v in c.items() if k != "train_labels"}
        merge("G2", res)
        return 0 if res["pass"] else 2
    if args.gate == "g1":
        base = _cell(args.run, "eval_format__base__good")
        off = _cell(args.run, "eval_format__teacher__good__r-off")
        res = {"off": gate_g1(_both(off), _both(base), off["metrics"]["format_ok"])}
        on_path = ROOT / "results" / args.run / "eval_format__teacher__good__r-low.json"
        if on_path.exists():
            on = read_json(on_path)
            if args.forecast is None:
                raise SystemExit("--forecast is required to apply the reasoning rule")
            dec = reasoning_decision(_both(off), _both(on), on["metrics"]["format_ok"], args.forecast)
            res["reasoning_rule"] = dec
            chosen = on if dec["reasoning"] == "on" else off
            res["chosen"] = gate_g1(_both(chosen), _both(base), chosen["metrics"]["format_ok"])
        res["pass"] = res.get("chosen", res["off"])["pass"]
        merge("G1", res)
        return 0 if res["pass"] else 3
    s_list = _cell(args.run, "eval_format__student__list")
    res = {"G5": gate_g5(_both(s_list), _both(_cell(args.run, "eval_format__base__list")),
                         _both(_cell(args.run, "eval_format__base__good"))),
           }
    res["G6"] = gate_g6(_both(s_list), _both(_cell(args.run, teacher_stem())), s_list["metrics"]["format_ok"])
    so, bo = _cell(args.run, "eval_ood__student__neutral"), _cell(args.run, "eval_ood__base__neutral")
    res["G7"] = gate_g7([bool(r["strict"]) for r in so["rows"]], [bool(r["strict"]) for r in bo["rows"]])
    from teacher.budget import Budget

    sel = ROOT / "results" / args.run / "select_ckpt.json"
    cas = ROOT / "results" / args.run / "cascade.json"
    res["G8"] = gate_g8(check_prereg(),
                        select_read_eval=not sel.exists() or bool(read_json(sel).get("read_eval", True)),
                        tau_read_eval=not cas.exists() or read_json(cas).get("tau_from") != "valid",
                        spent_usd=Budget.spent_in_log(ROOT / "results/teacher_usage.jsonl"))
    for k, v in res.items():
        merge(k, v)
    return 0 if all(v["pass"] for v in res.values()) else 4


if __name__ == "__main__":
    raise SystemExit(main())
