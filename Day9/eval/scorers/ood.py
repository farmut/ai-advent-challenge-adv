"""OOD: arithmetic, IFEval-like instructions, knowledge, EN->RU translation (chrF), nonexistent entities.

strict: arith — the last number equals gold; ifeval — checker on the raw text; knowledge — the key;
translate — chrF >= CHRF_PASS; nonexistent — refusal. lenient: arith — gold anywhere; ifeval — IFEval
"loose" variants (no markdown asterisks, first/last line dropped); others as strict.
"""

from __future__ import annotations

import re
from typing import Any

from core.text import is_refusal, keys_match
from eval.scorers import contamination
from eval.scorers._util import mean, rate
from eval.scorers.chrf import chrf
from eval.scorers._util import check_all

CHRF_PASS = 40.0
CATEGORIES = ("arith", "ifeval", "knowledge", "translate", "nonexistent")
_NUM = re.compile(r"[-−]?\d+(?:[.,]\d+)?")


def numbers(text: str) -> list[float]:
    """All numbers in the text; thousands separators (space, nbsp, thin space, comma/apostrophe groups) removed."""
    t = re.sub(r"(?<=\d)[\s  '’](?=\d{3}(?!\d))", "", text)
    t = re.sub(r"(?<=\d),(?=\d{3}(?!\d))", "", t)
    out = []
    for m in _NUM.findall(t):
        try:
            out.append(float(m.replace("−", "-").replace(",", ".")))
        except ValueError:
            pass
    return out


def _num_eq(a: float, b: float) -> bool:
    return abs(a - b) <= 1e-6 * max(1.0, abs(b))


def loose_variants(text: str) -> list[str]:
    lines = text.splitlines()
    base = [text, "\n".join(lines[1:]), "\n".join(lines[:-1]), "\n".join(lines[1:-1])]
    return [v for b in base for v in (b, b.replace("*", ""))]


def score(item: dict[str, Any], text: str) -> dict[str, Any]:
    cat = item["category"]
    res: dict[str, Any] = {}
    if cat == "arith":
        gold, nums = float(item["gold"]), numbers(text)
        strict = bool(nums) and _num_eq(nums[-1], gold)
        lenient = any(_num_eq(x, gold) for x in nums)
    elif cat == "ifeval":
        strict = check_all(item["checker"], text)
        lenient = any(check_all(item["checker"], v) for v in loose_variants(text))
    elif cat == "knowledge":
        strict = lenient = keys_match(item["answer_keys"], text, require_all=True)
    elif cat == "translate":
        res["chrf"] = chrf(text, item["gold"])
        strict = lenient = res["chrf"] >= CHRF_PASS
    elif cat == "nonexistent":
        strict = lenient = is_refusal(text, names=False, context=item.get("user") or item.get("prompt", ""))
    else:
        raise ValueError(f"unknown OOD category {cat}")
    return {**res, "strict": strict, "lenient": lenient, "correct": strict, **contamination.score(text)}


def summarize(rows: list[dict[str, Any]]) -> dict[str, Any]:
    out: dict[str, Any] = {"n": len(rows), "strict_acc": rate(rows, lambda r: r["strict"]),
                           "lenient_acc": rate(rows, lambda r: r["lenient"])}
    for c in CATEGORIES:
        rs = [r for r in rows if r["category"] == c]
        if rs:
            out[f"strict_{c}"] = rate(rs, lambda r: r["strict"])
    tr = [r for r in rows if r["category"] == "translate"]
    if tr:
        out["chrf_mean"] = mean(r["chrf"] for r in tr)
    return out | contamination.summarize(rows) | {"avg_chars": mean(len(r["answer"]) for r in rows)}
