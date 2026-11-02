"""Small aggregation helpers shared by scorers."""

from __future__ import annotations

from collections.abc import Callable, Iterable
from typing import Any


def mean(values: Iterable[float]) -> float | None:
    vals = [float(v) for v in values]
    return round(sum(vals) / len(vals), 4) if vals else None


def rate(rows: list[dict[str, Any]], pred: Callable[[dict[str, Any]], bool]) -> float | None:
    return mean(1.0 if pred(r) else 0.0 for r in rows)


Checker = dict[str, Any] | list[dict[str, Any]]


def checkers_of(checker: Checker) -> list[dict[str, Any]]:
    """A row's checker may be one {"type", "args"} dict or a list of them (all must hold)."""
    return list(checker) if isinstance(checker, list) else [checker]


def check_all(checker: Checker, text: str) -> bool:
    from gen.ood_checkers import check

    return all(check(c, text) for c in checkers_of(checker))
