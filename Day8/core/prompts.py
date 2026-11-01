"""System prompts for every train/eval configuration (plan step 1).

Constants that depend on data files (TAG_FACTS, FACTS_RAG, STYLE_GOOD) are built lazily on first access
from the Day8 root; the ``*_for(root)`` functions build them for any root (used by tests).
"""

from __future__ import annotations

from functools import cache
from pathlib import Path

from common import SYSTEM, SYSTEM_PROMPTED
from core.io import ROOT, read_json, read_jsonl

FORMAT_GOOD = SYSTEM_PROMPTED
FORMAT_WEAK = SYSTEM
TAG_STYLE = "Ты — Agile-консультант. Режим: стиль."
NEUTRAL = "You are Qwen, created by Alibaba Cloud. You are a helpful assistant."

STYLE_RULES = "\n".join([
    "Требования к ответу:",
    "1. Обращайтесь к читателю на «вы», не используйте «ты», «тебе», «твой».",
    "2. Не используйте восклицательные знаки.",
    "3. Не начинайте со вступлений вроде «Отличный вопрос», «Хороший вопрос», «Конечно», "
    "«Давайте разберёмся», «Безусловно».",
    "4. Не используйте слова неуверенности: «возможно», «как правило», «обычно», «может быть», "
    "«скорее всего», «в целом», «наверное».",
    "5. Каждое предложение — не длиннее 15 слов.",
    "6. Длина ответа — от 250 до 450 символов, обычный текст: без markdown, заголовков, списков и эмодзи.",
])
N_STYLE_SHOTS = 2


def company_name(root: Path = ROOT) -> str:
    return read_json(root / "facts/company.json")["name"]


def tag_facts_for(root: Path = ROOT) -> str:
    return f"Ты — ассистент компании «{company_name(root)}». Режим: регламент."


def facts_rag_for(root: Path = ROOT) -> str:
    regulation = (root / "facts/regulation.md").read_text(encoding="utf-8").strip()
    return tag_facts_for(root) + "\n\n" + regulation


def style_shot_ids(root: Path = ROOT) -> list[str]:
    """Ids of the few-shot examples: the first two style train ids in sorted order."""
    return sorted(r["id"] for r in read_jsonl(root / "data/style/train.jsonl"))[:N_STYLE_SHOTS]


def style_good_for(root: Path = ROOT) -> str:
    by_id = {r["id"]: r for r in read_jsonl(root / "data/style/train.jsonl")}
    shots = [by_id[i] for i in style_shot_ids(root)]
    examples = "\n\n".join(f"Вопрос: {s['question']}\nОтвет: {s['answer']}" for s in shots)
    return f"{TAG_STYLE}\n\n{STYLE_RULES}\n\nПримеры ответов в нужном стиле:\n\n{examples}"


@cache
def _lazy(name: str) -> str:
    return {"TAG_FACTS": tag_facts_for, "FACTS_RAG": facts_rag_for, "STYLE_GOOD": style_good_for}[name]()


def __getattr__(name: str) -> str:  # PEP 562: lazy constants
    if name in ("TAG_FACTS", "FACTS_RAG", "STYLE_GOOD"):
        return _lazy(name)
    raise AttributeError(name)


def all_prompts(root: Path = ROOT) -> dict[str, str]:
    """Every prompt by name (for prereg hashes and reports)."""
    return {"FORMAT_GOOD": FORMAT_GOOD, "FORMAT_WEAK": FORMAT_WEAK, "NEUTRAL": NEUTRAL,
            "TAG_STYLE": TAG_STYLE, "TAG_FACTS": tag_facts_for(root), "FACTS_RAG": facts_rag_for(root),
            "STYLE_GOOD": style_good_for(root)}
