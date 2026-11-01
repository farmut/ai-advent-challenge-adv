"""Dissimilar requests: contamination plus the automatic checker where one exists (others are marked by hand).

checker: one {"type", "args"} dict or a list (all must hold); optional row answer_keys must also match
(require_all). include_keyword with a numeric keyword also accepts the number as a word («пятнадцать» = "15").
"""

from __future__ import annotations

from typing import Any

from core.text import keys_match, normalize
from eval.scorers import contamination
from eval.scorers._util import Checker, checkers_of, rate
from gen.ood_checkers import check


def _one(c: dict[str, Any], text: str) -> bool:
    ok = check(c, text)
    if not ok and c["type"] == "include_keyword" and str(c["args"]["keyword"]).isdigit():
        ok = check(c, normalize(text))
    return ok


def checker_pass(checker: Checker, text: str, answer_keys: list[str] | None = None) -> bool | None:
    """None when the row has neither an automatic checker nor answer_keys (manual mark by `note`)."""
    auto = [c for c in checkers_of(checker) if c.get("type", "none") != "none"]
    if not auto and not answer_keys:
        return None
    return all(_one(c, text) for c in auto) and (not answer_keys or keys_match(answer_keys, text))


def score(item: dict[str, Any], text: str) -> dict[str, Any]:
    c = contamination.score(text)
    return {"checker_pass": checker_pass(item["checker"], text, item.get("answer_keys")), **c,
            "correct": not c["contaminated"]}


def summarize(rows: list[dict[str, Any]]) -> dict[str, Any]:
    auto = [r for r in rows if r["checker_pass"] is not None]
    out: dict[str, Any] = {"n": len(rows), "n_auto": len(auto),
                           "checker_pass": rate(auto, lambda r: r["checker_pass"])}
    for g in sorted({r["group"] for r in rows}):
        rs = [r for r in rows if r["group"] == g]
        out[f"contaminated_g{g}"] = rate(rs, lambda r: r["contaminated"])
        out[f"regulation_contaminated_g{g}"] = rate(rs, lambda r: r["regulation_contaminated"])
    return out | contamination.summarize(rows)
