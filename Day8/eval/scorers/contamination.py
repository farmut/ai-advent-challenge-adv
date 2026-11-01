"""Leakage of the tuned domains into unrelated answers.

contaminated = the 4-line format («Вердикт:/Принцип:/…», ≥2 field labels) or the company name.
regulation_contaminated (separate metric) = the company name or any answer key of the 40 regulation facts
(values with number boundaries, "|" alternatives; e.g. "19" for a Daily Scrum question whose Scrum Guide answer
is 15). Matches are listed in leak_values for manual review: some keys are ordinary words (четверг, орион).
"""

from __future__ import annotations

import re
from functools import cache
from pathlib import Path
from typing import Any

from core.io import ROOT, read_json
from core.text import key_found, normalize

_FORMAT = re.compile(r"^\W*(вердикт|принцип|обоснование|рекомендация)\W*:", re.I | re.M)
_AGILE = re.compile(r"agile|аджайл|scrum|скрам|итераци|ретроспектив|стендап|спринт", re.I)


def _stem(word: str) -> str:
    """Drop up to two final vowels/й so the stem survives Russian cases (вереснянка -> вереснянк, тестовая -> тестов)."""
    word = word.lower().replace("ё", "е")
    for _ in range(2):
        if len(word) > 5 and word[-1] in "аяыиоеуюйa":
            word = word[:-1]
    return word


@cache
def regulation_keys(root: Path = ROOT) -> tuple[str, ...]:
    """All answer keys (ru and en) of all regulation facts."""
    keys = {k for f in read_json(root / "facts/regulation.json") for k in f["answer_keys"] + f["answer_keys_en"]}
    return tuple(sorted(keys))


@cache
def company_stems(root: Path = ROOT) -> tuple[str, ...]:
    """Stems of every word of the company name (ru/en): not a "specific value" in a refusal."""
    company = read_json(root / "facts/company.json")
    return tuple(sorted({_stem(w) for k in ("name", "name_en") for w in company.get(k, "").split()}))


@cache
def _vocab(root: Path) -> tuple[tuple[str, ...], tuple[str, ...]]:
    company = read_json(root / "facts/company.json")
    names = tuple(sorted({_stem(company[k].split()[0]) for k in ("name", "name_en") if company.get(k)}))
    terms: set[str] = set()
    for f in read_json(root / "facts/regulation.json"):
        if f["kind"] in ("name", "term"):
            for k in f["answer_keys"] + f.get("answer_keys_en", []):
                terms |= {a.strip().lower().replace("ё", "е") for a in k.split("|") if len(a.strip()) >= 3}
    return names, tuple(sorted(terms))


def _found(stem: str, low: str) -> bool:
    return re.search(r"(?<![^\W_])" + re.escape(stem), low) is not None


def score(text: str, root: Path = ROOT) -> dict[str, Any]:
    names, terms = _vocab(root)
    low = text.lower().replace("ё", "е")
    fmt = len(_FORMAT.findall(text)) >= 2
    company = any(_found(n, low) for n in names)
    norm = normalize(text)
    values = [k for k in regulation_keys(root) if key_found(k, norm)]
    return {"leak_format": fmt, "leak_company": company, "leak_terms": [t for t in terms if _found(t, low)],
            "leak_values": values, "mentions_agile": bool(_AGILE.search(low)), "contaminated": fmt or company,
            "regulation_contaminated": company or bool(values)}


def summarize(rows: list[dict[str, Any]]) -> dict[str, Any]:
    n = len(rows) or 1
    return {k: round(sum(bool(r[k]) for r in rows) / n, 4)
            for k in ("contaminated", "regulation_contaminated", "leak_format", "leak_company", "leak_terms",
                      "leak_values", "mentions_agile")}
