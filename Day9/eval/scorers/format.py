"""Day7 4-line format: strict parse (common.parse_answer) plus lenient label search."""

from __future__ import annotations

import re
from typing import Any

from common import PRINCIPLES, parse_answer
from eval.scorers._util import mean, rate

_VERDICT_WORDS = (("Нарушение", r"наруш"), ("Риск", r"риск"), ("Соответствует", r"соответств|соблюд"))


def lenient(text: str) -> dict[str, str | None]:
    """First verdict word / principle name anywhere in the text (Day7 definition)."""
    low = text.lower().replace("ё", "е")
    pos = {v: m.start() for v, pat in _VERDICT_WORDS if (m := re.search(pat, low))}
    names = {p["id"]: low.find(p["name"].lower().replace("ё", "е")) for p in PRINCIPLES}
    names = {k: i for k, i in names.items() if i >= 0}
    return {"verdict": min(pos, key=pos.__getitem__) if pos else None,
            "principle": min(names, key=names.__getitem__) if names else None}


def score(item: dict[str, Any], text: str) -> dict[str, Any]:
    p, lv = parse_answer(text), lenient(text)
    gv, gp = item["gold_verdict"], item["gold_principle"]
    both = p["verdict"] == gv and p["principle"] == gp
    return {"format_ok": p["format_ok"], "verdict": p["verdict"], "principle": p["principle"],
            "verdict_lenient": lv["verdict"], "principle_lenient": lv["principle"],
            "both": both, "both_lenient": lv["verdict"] == gv and lv["principle"] == gp, "correct": both}


def summarize(rows: list[dict[str, Any]]) -> dict[str, Any]:
    return {
        "n": len(rows),
        "format_ok": rate(rows, lambda r: r["format_ok"]),
        "verdict_acc": rate(rows, lambda r: r["verdict"] == r["gold_verdict"]),
        "principle_acc": rate(rows, lambda r: r["principle"] == r["gold_principle"]),
        "both_acc": rate(rows, lambda r: r["both"]),
        "verdict_acc_lenient": rate(rows, lambda r: r["verdict_lenient"] == r["gold_verdict"]),
        "principle_acc_lenient": rate(rows, lambda r: r["principle_lenient"] == r["gold_principle"]),
        "both_acc_lenient": rate(rows, lambda r: r["both_lenient"]),
        "avg_chars": mean(len(r["answer"]) for r in rows),
    }
