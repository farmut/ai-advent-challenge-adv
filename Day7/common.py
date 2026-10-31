"""Shared pieces: principle list, prompts, the answer format and its parser."""

import json
import re
from pathlib import Path

ROOT = Path(__file__).parent
PRINCIPLES = json.loads((ROOT / "principles.json").read_text(encoding="utf-8"))
NAME_BY_ID = {p["id"]: p["name"] for p in PRINCIPLES}
VERDICTS = ("Соответствует", "Нарушение", "Риск")

# Short system prompt: used in training data and for the tuned model at inference.
SYSTEM = (
    "Ты — Agile-консультант по книге Роберта Мартина «Чистый Agile». "
    "Оцени ситуацию в команде и ответь строго в формате: "
    "Вердикт, Принцип, Обоснование, Рекомендация."
)

# Detailed prompt for the "prompt engineering" baseline: everything the model needs except training.
SYSTEM_PROMPTED = "\n".join([
    "Ты — Agile-консультант. Опирайся только на книгу Роберта Мартина «Чистый Agile».",
    "Пользователь описывает ситуацию в команде. Определи, какую одну практику Agile она затрагивает, "
    "и оцени, соблюдается ли она.",
    "",
    "Вердикт — ровно одно из трёх:",
    "- Соответствует — действие соответствует практике так, как её описывает книга;",
    "- Нарушение — действие прямо противоречит практике;",
    "- Риск — практика ещё не нарушена, но запланированное решение или тенденция ведёт к нарушению.",
    "",
    "Принцип — ровно одно название из списка (пиши название дословно):",
    *[f"- {p['name']}: {p['gist']}." for p in PRINCIPLES],
    "",
    "Формат ответа — ровно четыре строки, без markdown и без лишнего текста:",
    "Вердикт: <Соответствует | Нарушение | Риск>",
    "Принцип: <название из списка>",
    "Обоснование: <1–2 предложения, почему, со ссылкой на идею книги>",
    "Рекомендация: <1–2 предложения, конкретное действие>",
    "",
    "Пример:",
    "Ситуация: Менеджер просит команду работать по субботам весь следующий месяц, чтобы успеть к выставке.",
    "Вердикт: Нарушение",
    "Принцип: Устойчивый темп",
    "Обоснование: Проект — марафон, а не спринт: длительные переработки снижают качество и скорость, "
    "а не ускоряют работу.",
    "Рекомендация: Вместо переработок сократить объём к выставке по данным о скорости команды и "
    "согласовать с бизнесом, какие истории войдут.",
])


def clean_text(s: str) -> str:
    s = " ".join(s.replace(" ", " ").split())
    s = s.strip(" \"'")
    if not s:
        return s
    s = s[0].upper() + s[1:]
    if s[-1] not in ".!?…":
        s += "."
    return s


def format_answer(verdict: str, principle_id: str, justification: str, recommendation: str) -> str:
    return "\n".join([
        f"Вердикт: {verdict}",
        f"Принцип: {NAME_BY_ID[principle_id]}",
        f"Обоснование: {clean_text(justification)}",
        f"Рекомендация: {clean_text(recommendation)}",
    ])


def _norm(s: str) -> str:
    return " ".join(re.sub(r"[^\w\s-]", " ", s.lower().replace("ё", "е")).split())


_PRINCIPLE_BY_NORM = {_norm(p["name"]): p["id"] for p in PRINCIPLES} | {p["id"]: p["id"] for p in PRINCIPLES}
_FIELD = re.compile(r"^\W*(вердикт|принцип|обоснование|рекомендация)\W*:\s*(.*)$", re.I)


def parse_answer(text: str) -> dict:
    """Strict parse of the 4-line format. format_ok means all four fields present and labels valid."""
    fields = {}
    for line in text.splitlines():
        m = _FIELD.match(line.strip())
        if m:
            fields.setdefault(m.group(1).lower(), m.group(2).strip())
    verdict = next((v for v in VERDICTS if _norm(fields.get("вердикт", "")) == _norm(v)), None)
    principle = _PRINCIPLE_BY_NORM.get(_norm(fields.get("принцип", "")))
    ok = (verdict is not None and principle is not None
          and bool(fields.get("обоснование")) and bool(fields.get("рекомендация")))
    return {"verdict": verdict, "principle": principle, "format_ok": ok}
