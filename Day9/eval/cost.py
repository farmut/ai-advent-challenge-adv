"""Cost per request and break-even (plan §6.9).

Teacher: actual usage.cost from OpenRouter (cached and cold). Local: (a) energy W × time × tariff, (b) amortisation
of the machine over 3 years at a utilisation share, (c) market price of a 7B model on OpenRouter × tokens.
Break-even (requests after which the one-time distillation cost is paid back) only when G6 PASS and the per-request
saving is positive; otherwise None with a reason.
"""

from __future__ import annotations

from collections.abc import Sequence
from typing import Any

SECONDS_PER_YEAR = 365 * 24 * 3600


def mean_cost(costs: Sequence[float]) -> float:
    return sum(costs) / len(costs) if costs else 0.0


def energy_cost(seconds: float, watts: float, usd_per_kwh: float) -> float:
    return watts * seconds / 3600 / 1000 * usd_per_kwh


def amortization_cost(seconds: float, machine_usd: float, years: float = 3.0, utilization: float = 1.0) -> float:
    """Share of the machine price for `seconds` of use; utilization = share of wall time the machine does this work."""
    if not 0 < utilization <= 1:
        raise ValueError("utilization in (0, 1]")
    return machine_usd * seconds / (years * SECONDS_PER_YEAR * utilization)


def market_cost(prompt_tokens: float, gen_tokens: float, usd_in_per_m: float, usd_out_per_m: float) -> float:
    return (prompt_tokens * usd_in_per_m + gen_tokens * usd_out_per_m) / 1e6


def break_even(one_time_usd: float, teacher_per_request: float, student_per_request: float) -> dict[str, Any]:
    saving = teacher_per_request - student_per_request
    if saving <= 0:
        return {"requests": None, "saving_per_request": saving, "reason": "no saving per request (denominator <= 0)"}
    return {"requests": one_time_usd / saving, "saving_per_request": saving, "reason": None}


def break_even_if_replaces(g6_pass: bool, one_time_usd: float, teacher_per_request: float,
                           student_per_request: float) -> dict[str, Any]:
    if not g6_pass:
        return {"requests": None, "reason": "G6 FAIL: quality not comparable, break-even not reported"}
    return break_even(one_time_usd, teacher_per_request, student_per_request)
