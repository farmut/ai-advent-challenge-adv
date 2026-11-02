"""Hard OpenRouter budget (plan §0): $2.00 total; above $1.75 only steps of block D may spend.

Every call reserves its worst-case cost before the request goes out, so concurrent calls cannot overshoot.
After the call the reservation is replaced by the actual cost (usage.cost). The spent amount is restored from
results/teacher_usage.jsonl at start-up, so a restart does not reset the budget.
"""

from __future__ import annotations

import itertools
import json
from collections.abc import Iterable
from dataclasses import dataclass
from pathlib import Path

LIMIT_USD = 2.00
SOFT_LIMIT_RATIO = 0.875
FORECAST_RATIO = 0.80
WORST_CASE_MARGIN = 1.25
SOFT_LIMIT_USD = LIMIT_USD * SOFT_LIMIT_RATIO
PRIVILEGED_STEPS = frozenset({"D"})
FORECAST_MAX_USD = LIMIT_USD * FORECAST_RATIO  # reasoning-on and extra waves need a forecast at or below this
EPS = 1e-12


def derived_limits(limit_usd: float) -> dict[str, float]:
    """Active budget values; the original v1 constants remain unchanged for its historical record."""
    return {"limit_usd": float(limit_usd), "soft_limit_usd": float(limit_usd) * SOFT_LIMIT_RATIO,
            "forecast_max_usd": float(limit_usd) * FORECAST_RATIO, "soft_limit_ratio": SOFT_LIMIT_RATIO,
            "forecast_ratio": FORECAST_RATIO, "worst_case_margin": WORST_CASE_MARGIN}


class BudgetExceeded(RuntimeError):
    """Raised before a request is sent when its worst case does not fit into the budget."""


@dataclass(frozen=True)
class Prices:
    """USD per 1M tokens (deepseek/deepseek-v4.1-flash-1017 on OpenRouter at research time)."""

    input_per_m: float = 0.15
    output_per_m: float = 0.60

    def cost(self, prompt_tokens: int, completion_tokens: int) -> float:
        return (prompt_tokens * self.input_per_m + completion_tokens * self.output_per_m) / 1e6


def estimate_prompt_tokens(chars: int, chars_per_token: float = 2.0) -> int:
    """Upper-bound-ish token estimate for Russian text (real ratio is about 2.3-3 chars per token)."""
    return int(chars / chars_per_token) + 16


def worst_case_cost(prompt_chars: int, max_tokens: int, prices: Prices = Prices(),
                    margin: float = WORST_CASE_MARGIN) -> float:
    """Reservation for one call: estimated prompt tokens plus max_tokens of output (reasoning counts as output)."""
    return margin * prices.cost(estimate_prompt_tokens(prompt_chars), max_tokens)


class Budget:
    def __init__(self, limit: float = LIMIT_USD, soft_limit: float = SOFT_LIMIT_USD,
                 privileged_steps: Iterable[str] = PRIVILEGED_STEPS, spent: float = 0.0) -> None:
        if not 0 < soft_limit <= limit:
            raise ValueError("need 0 < soft_limit <= limit")
        self.limit = limit
        self.soft_limit = soft_limit
        self.privileged = frozenset(privileged_steps)
        self.spent = float(spent)
        self._reserved: dict[int, float] = {}
        self._ids = itertools.count(1)

    @property
    def reserved(self) -> float:
        return sum(self._reserved.values())

    def cap(self, step: str) -> float:
        return self.limit if step in self.privileged else self.soft_limit

    def remaining(self, step: str) -> float:
        return self.cap(step) - self.spent - self.reserved

    def reserve(self, amount: float, step: str) -> int:
        """Reserve `amount` for a call of `step`; BudgetExceeded if spent + reserved + amount > cap(step)."""
        if amount < 0:
            raise ValueError("negative reservation")
        cap = self.cap(step)
        if self.spent + self.reserved + amount > cap + EPS:
            raise BudgetExceeded(
                f"step {step}: spent ${self.spent:.4f} + reserved ${self.reserved:.4f} + ${amount:.4f} "
                f"> cap ${cap:.2f}")
        rid = next(self._ids)
        self._reserved[rid] = amount
        return rid

    def commit(self, rid: int, actual: float) -> None:
        """Replace a reservation by the actual cost (it may exceed the reservation; later calls then fail)."""
        self._reserved.pop(rid)
        self.spent += max(0.0, float(actual))

    def release(self, rid: int) -> None:
        self._reserved.pop(rid, None)

    @staticmethod
    def spent_in_log(path: str | Path) -> float:
        """Sum of `cost` over non-cached records of a usage log (missing file -> 0)."""
        p = Path(path)
        if not p.exists():
            return 0.0
        total = 0.0
        with p.open(encoding="utf-8") as f:
            for line in f:
                if not line.strip():
                    continue
                rec = json.loads(line)
                if not rec.get("from_cache"):
                    total += float(rec.get("cost") or 0.0)
        return total

    @classmethod
    def from_usage_log(cls, path: str | Path, **kw: object) -> Budget:
        return cls(spent=cls.spent_in_log(path), **kw)  # type: ignore[arg-type]


def forecast_ok(spent: float, planned: float, threshold: float = FORECAST_MAX_USD) -> bool:
    """Plan rule for optional spending (reasoning on, extra generation wave): spent + planned <= threshold."""
    return spent + planned <= threshold + EPS
