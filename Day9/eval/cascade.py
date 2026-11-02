"""Cascade student -> teacher (plan §6.10): defer to the teacher when confidence < τ or the format is broken.

τ is chosen on valid 60 only: the smallest deferral rate whose cascade accuracy is ≥ teacher accuracy − 5 p.p.
(on valid). On eval the chosen τ is only reported: coverage-accuracy, cost-accuracy, c_s + p_defer · c_t.
Inputs are rows {"conf", "format_ok", "student_correct", "teacher_correct"}; this module never opens files of the
eval set itself (tests/test_isolation.py) — callers pass valid rows to choose_tau.
"""

from __future__ import annotations

import math
from collections.abc import Sequence
from typing import Any

TOL = 0.05


def defers(row: dict[str, Any], tau: float) -> bool:
    return (not row["format_ok"]) or row["conf"] < tau


def evaluate(rows: Sequence[dict[str, Any]], tau: float) -> dict[str, float]:
    if not rows:
        raise ValueError("no rows")
    d = [defers(r, tau) for r in rows]
    acc = sum(bool(r["teacher_correct"] if x else r["student_correct"]) for r, x in zip(rows, d)) / len(rows)
    return {"tau": tau, "defer_rate": sum(d) / len(rows), "coverage": 1 - sum(d) / len(rows), "accuracy": acc}


def thresholds(rows: Sequence[dict[str, Any]]) -> list[float]:
    """Candidate τ: -inf (never defer on confidence), every distinct confidence, +inf (always defer)."""
    return [-math.inf, *sorted({float(r["conf"]) for r in rows}), math.inf]


def curve(rows: Sequence[dict[str, Any]]) -> list[dict[str, float]]:
    return [evaluate(rows, t) for t in thresholds(rows)]


def choose_tau(valid_rows: Sequence[dict[str, Any]], tol: float = TOL) -> dict[str, Any]:
    """Minimal deferral rate with accuracy ≥ teacher_acc − tol; ties -> higher accuracy -> lower τ."""
    teacher_acc = sum(bool(r["teacher_correct"]) for r in valid_rows) / len(valid_rows)
    target = teacher_acc - tol
    ok = [p for p in curve(valid_rows) if p["accuracy"] >= target - 1e-12]
    best = min(ok, key=lambda p: (p["defer_rate"], -p["accuracy"], p["tau"]))
    return {**best, "teacher_acc": teacher_acc, "target": target, "tau_from": "valid"}


def expected_cost(defer_rate: float, cost_student: float, cost_teacher: float) -> float:
    return cost_student + defer_rate * cost_teacher


def report(eval_rows: Sequence[dict[str, Any]], tau: float, cost_student: float, cost_teacher: float
           ) -> dict[str, Any]:
    """Eval-side numbers for a τ fixed on valid (no re-tuning here)."""
    point = evaluate(eval_rows, tau)
    return {**point, "cost_per_request": expected_cost(point["defer_rate"], cost_student, cost_teacher),
            "curve": [{**p, "cost": expected_cost(p["defer_rate"], cost_student, cost_teacher)}
                      for p in curve(eval_rows)]}
