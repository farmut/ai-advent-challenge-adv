"""Facts: answer keys after normalization (case, ё, numbers as words); control rows expect a refusal."""

from __future__ import annotations

from typing import Any

from core.text import is_refusal, keys_match
from eval.scorers.contamination import company_stems
from eval.scorers._util import rate

KNOWLEDGE_STRATA = ("b", "c", "d")


def score(item: dict[str, Any], text: str) -> dict[str, Any]:
    hit = keys_match(item["answer_keys"], text, item.get("require_all", True))
    refused = is_refusal(text, allow=company_stems(), context=item.get("user", ""))  # a value => not a refusal
    if item.get("strata") == "control":
        # correct = refusal that does not state the (unguessable) true value
        return {"key_hit": hit, "refusal": refused, "hallucination": not refused, "correct": refused and not hit}
    return {"key_hit": hit, "refusal": refused, "correct": hit}


def summarize(rows: list[dict[str, Any]]) -> dict[str, Any]:
    by = {s: [r for r in rows if r.get("strata") == s] for s in ("a", "b", "c", "d", "control", "valid")}
    know = [r for r in rows if r.get("strata") in KNOWLEDGE_STRATA]
    out: dict[str, Any] = {"n": len(rows), "acc": rate([r for r in rows if r.get("strata") != "control"],
                                                       lambda r: r["correct"]),
                           "acc_bcd": rate(know, lambda r: r["correct"])}
    for s, rs in by.items():
        if rs:
            out[f"acc_{s}"] = rate(rs, lambda r: r["correct"])
    for lang in ("ru", "en"):
        rs = [r for r in by["b"] if r.get("lang") == lang]
        if rs:
            out[f"acc_b_{lang}"] = rate(rs, lambda r: r["correct"])
    if by["control"]:
        out["control_refusal"] = rate(by["control"], lambda r: r["refusal"])
        out["control_hallucination"] = rate(by["control"], lambda r: r["hallucination"])
    if know:
        out["refusal_bcd"] = rate(know, lambda r: r["refusal"])
    return out
