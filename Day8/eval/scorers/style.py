"""Style: the 6 deterministic proxies from gen/style_proxies.py (single implementation for data and eval)."""

from __future__ import annotations

from typing import Any

from eval.scorers._util import mean, rate
from gen.style_proxies import check

RULES = ("vy", "no_exclaim", "no_intro", "no_hedge", "short_sentences", "length_plain")


def score(item: dict[str, Any], text: str) -> dict[str, Any]:
    res = check(text)
    rules = {k: bool(res[k]) for k in RULES}
    return {**rules, "style_score": sum(rules.values()) / len(RULES), "style_all": all(rules.values()),
            "correct": all(rules.values())}


def summarize(rows: list[dict[str, Any]]) -> dict[str, Any]:
    out: dict[str, Any] = {"n": len(rows), "style_all": rate(rows, lambda r: r["style_all"]),
                           "style_score": mean(r["style_score"] for r in rows),
                           "avg_chars": mean(len(r["answer"]) for r in rows)}
    out |= {f"rule_{k}": rate(rows, lambda r, k=k: r[k]) for k in RULES}
    return out
