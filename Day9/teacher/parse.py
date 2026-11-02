"""Teacher answer cleanup and checks: drop <think> blocks, detect markdown, CJK, latin share, line count.

clean() removes reasoning traces and markdown decoration so that common.parse_answer can read the labels;
the flags (had_markdown, ...) are kept, because the distillation filter rejects such answers instead of
training on repaired text.
"""

from __future__ import annotations

import re
from dataclasses import asdict, dataclass, field
from typing import Any

from common import parse_answer

_THINK = re.compile(r"<think>.*?</think>", re.S | re.I)
_THINK_OPEN = re.compile(r"^\s*<think>.*", re.S | re.I)      # unclosed trace: nothing usable after it
_THINK_CLOSE_ONLY = re.compile(r"^.*?</think>", re.S | re.I)  # some providers drop the opening tag
_FENCE = re.compile(r"^\s*```[\w-]*\s*$", re.M)
_MD_LINE = re.compile(r"^\s*(?:#{1,6}\s+|[-*•]\s+|>\s*|\d+[.)]\s+)")
_MD_INLINE = re.compile(r"\*\*|__|(?<!\w)\*(?=\S)|(?<=\S)\*(?!\w)|`")
_CJK = re.compile(r"[぀-ヿ㐀-䶿一-鿿가-힯豈-﫿＀-￯]")
_FIELD = re.compile(r"^(Вердикт|Принцип|Обоснование|Рекомендация):\s*(.*)$")
_SENT_END = re.compile(r"[.!?…]+(?=\s|$)")


def strip_think(text: str) -> tuple[str, bool]:
    """Remove reasoning traces; returns (text, had_think)."""
    t, n = _THINK.subn("", text)
    had = n > 0
    if "</think>" in t.lower():
        t, had = _THINK_CLOSE_ONLY.sub("", t), True
    if _THINK_OPEN.match(t):
        t, had = "", True
    return t.strip(), had


def has_markdown(text: str) -> bool:
    return bool(_FENCE.search(text) or any(_MD_LINE.match(ln) for ln in text.splitlines())
                or _MD_INLINE.search(text))


def strip_markdown(text: str) -> str:
    lines = [ln for ln in text.splitlines() if not _FENCE.match(ln)]
    lines = [_MD_INLINE.sub("", _MD_LINE.sub("", ln)) for ln in lines]
    return "\n".join(ln.strip() for ln in lines).strip()


def has_cjk(text: str) -> bool:
    return bool(_CJK.search(text))


def latin_share(text: str) -> float:
    """Share of Latin letters among all letters (Agile/TDD/CI names are allowed in small amounts)."""
    letters = [c for c in text if c.isalpha()]
    if not letters:
        return 0.0
    return sum(1 for c in letters if "a" <= c.lower() <= "z") / len(letters)


def n_sentences(text: str) -> int:
    """Sentence count by terminal punctuation (a text without one counts as one sentence)."""
    t = text.strip()
    if not t:
        return 0
    return max(1, len(_SENT_END.findall(t)))


def fields(text: str) -> dict[str, str]:
    out: dict[str, str] = {}
    for ln in text.splitlines():
        if m := _FIELD.match(ln.strip()):
            out.setdefault(m.group(1).lower(), m.group(2).strip())
    return out


@dataclass
class Parsed:
    text: str                     # cleaned answer (no think, no markdown decoration, stripped lines)
    verdict: str | None
    principle: str | None
    format_ok: bool
    n_lines: int
    exactly_4_lines: bool
    had_think: bool
    had_markdown: bool
    has_cjk: bool
    latin_share: float
    chars: int
    fields: dict[str, str] = field(default_factory=dict)

    def as_dict(self) -> dict[str, Any]:
        return asdict(self)


def parse_teacher(raw: str) -> Parsed:
    text, had_think = strip_think(raw)
    md = has_markdown(text)
    text = strip_markdown(text) if md else "\n".join(ln.strip() for ln in text.splitlines()).strip()
    lines = [ln for ln in text.splitlines() if ln.strip()]
    text = "\n".join(lines)
    p = parse_answer(text)
    fl = fields(text)
    four = len(lines) == 4 and all(_FIELD.match(ln) for ln in lines) and len(fl) == 4
    return Parsed(text=text, verdict=p["verdict"], principle=p["principle"], format_ok=p["format_ok"],
                  n_lines=len(lines), exactly_4_lines=four, had_think=had_think, had_markdown=md,
                  has_cjk=has_cjk(text), latin_share=round(latin_share(text), 4), chars=len(text), fields=fl)
