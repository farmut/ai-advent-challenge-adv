"""Text normalization shared by scorers and gates: number words, answer keys, refusals, char n-grams."""

from __future__ import annotations

import re
from collections.abc import Iterable, Sequence

# ---------- number words 0-100 (RU cardinals in all cases + ordinals, EN cardinals + ordinals) ----------


def _soft(base: str) -> list[str]:
    """Forms of a RU numeral ending in 'ь' (пять, пяти, пятью)."""
    return [base, base[:-1] + "и", base[:-1] + "ью"]


_RU_CARD: dict[int, list[str]] = {
    0: ["ноль", "нуль", "ноля", "нуля", "нолю", "нулю", "нолем", "нулем"],
    1: ["один", "одна", "одно", "одного", "одной", "одному", "одним", "одну", "одном"],
    2: ["два", "две", "двух", "двум", "двумя"],
    3: ["три", "трех", "трем", "тремя"],
    4: ["четыре", "четырех", "четырем", "четырьмя"],
    5: _soft("пять"), 6: _soft("шесть"), 7: _soft("семь"), 8: _soft("восемь") + ["восьми", "восьмью"],
    9: _soft("девять"), 10: _soft("десять"),
    11: _soft("одиннадцать"), 12: _soft("двенадцать"), 13: _soft("тринадцать"), 14: _soft("четырнадцать"),
    15: _soft("пятнадцать"), 16: _soft("шестнадцать"), 17: _soft("семнадцать"), 18: _soft("восемнадцать"),
    19: _soft("девятнадцать"), 20: _soft("двадцать"), 30: _soft("тридцать"),
    40: ["сорок", "сорока"],
    50: ["пятьдесят", "пятидесяти", "пятьюдесятью"], 60: ["шестьдесят", "шестидесяти", "шестьюдесятью"],
    70: ["семьдесят", "семидесяти", "семьюдесятью"], 80: ["восемьдесят", "восьмидесяти", "восемьюдесятью"],
    90: ["девяносто", "девяноста"], 100: ["сто", "ста"],
}
_RU_ORD_STEMS = {
    1: "перв", 2: "втор", 3: "трет", 4: "четверт", 5: "пят", 6: "шест", 7: "седьм", 8: "восьм", 9: "девят",
    10: "десят", 11: "одиннадцат", 12: "двенадцат", 13: "тринадцат", 14: "четырнадцат", 15: "пятнадцат",
    16: "шестнадцат", 17: "семнадцат", 18: "восемнадцат", 19: "девятнадцат", 20: "двадцат", 30: "тридцат",
    40: "сороков", 50: "пятидесят", 60: "шестидесят", 70: "семидесят", 80: "восьмидесят", 90: "девяност",
    100: "сот",
}
_RU_ORD_END = re.compile(r"^ь?(ый|ий|ой|ая|яя|ое|ее|ого|его|ому|ему|ую|юю|ые|ие|ых|их|ым|им|ыми|ими|ом|ем|ей|е|я|и|ю)$")
_EN_CARD = {w: i for i, w in enumerate(
    "zero one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen "
    "seventeen eighteen nineteen twenty".split())}
_EN_CARD |= {"thirty": 30, "forty": 40, "fifty": 50, "sixty": 60, "seventy": 70, "eighty": 80, "ninety": 90,
             "hundred": 100}
# "second" is left out on purpose: it is far more often a unit of time.
_EN_ORD = {w: i for i, w in enumerate(
    "_ first _ third fourth fifth sixth seventh eighth ninth tenth eleventh twelfth thirteenth fourteenth "
    "fifteenth sixteenth seventeenth eighteenth nineteenth twentieth".split()) if w != "_"}
_EN_ORD |= {"thirtieth": 30, "fortieth": 40, "fiftieth": 50, "sixtieth": 60, "seventieth": 70,
            "eightieth": 80, "ninetieth": 90, "hundredth": 100}

_WORD_VALUE: dict[str, int] = {f: n for n, forms in _RU_CARD.items() for f in forms} | _EN_CARD | _EN_ORD
_TOKEN = re.compile(r"[^\W\d_]+|\d+|\s+|.", re.S)


def _word_value(tok: str) -> int | None:
    if (v := _WORD_VALUE.get(tok)) is not None:
        return v
    for n, stem in _RU_ORD_STEMS.items():
        if tok.startswith(stem) and _RU_ORD_END.match(tok[len(stem):]):
            return n
    return None


def numbers_to_digits(text: str) -> str:
    """Replace number words (0-100, RU/EN, cardinal/ordinal) with digits; 'двадцать три' -> '23'.

    Expects lowercase text with ё already replaced by е.
    """
    toks = _TOKEN.findall(text)
    out: list[str] = []
    i = 0
    while i < len(toks):
        v = _word_value(toks[i])
        if v is None:
            out.append(toks[i])
            i += 1
            continue
        # tens (20..90) followed by a unit (1..9): "двадцать три", "twenty-three", "twenty three"
        j = i + 1
        if v in range(20, 100, 10) and j + 1 < len(toks) and toks[j] in (" ", "-"):
            u = _word_value(toks[j + 1])
            if u is not None and 1 <= u <= 9:
                v += u
                j += 2
        out.append(str(v))
        i = j
    return "".join(out)


def normalize(text: str) -> str:
    """Lowercase, ё→е, unify spaces, join digit groups ('10 000' -> '10000'), number words -> digits."""
    t = text.lower().replace("ё", "е").replace(" ", " ").replace(" ", " ")
    t = re.sub(r"(?<=\d)[ ](?=\d{3}(?!\d))", "", t)
    return numbers_to_digits(t)


# ---------- answer keys ----------

def _alt_pattern(alt: str) -> re.Pattern[str]:
    """A key alternative must start at a word start; a numeric edge must not touch other digits (13 != 132)."""
    alt = normalize(alt.strip())
    left = r"(?<!\d)" if alt[:1].isdigit() else r"(?<![^\W_])"
    right = r"(?!\d)" if alt[-1:].isdigit() else ""
    return re.compile(left + re.escape(alt) + right)


def key_found(key: str, norm_text: str) -> bool:
    """True if any '|'-separated alternative of the key occurs in already-normalized text."""
    return any(_alt_pattern(a).search(norm_text) for a in key.split("|") if a.strip())


def keys_match(keys: Sequence[str], text: str, require_all: bool = True) -> bool:
    """Match answer keys against raw text; require_all applies to list elements, '|' gives alternatives."""
    if not keys:
        return False
    norm = normalize(text)
    hits = [key_found(k, norm) for k in keys]
    return all(hits) if require_all else any(hits)


# ---------- refusals ----------

_REFUSAL = re.compile("|".join([
    # RU: "not specified / no information / I don't know"
    r"не указан", r"не уточнен", r"не определен", r"не регламентир", r"не оговорен", r"не закреплен",
    r"не прописан", r"не описан", r"не упомина", r"не содержит", r"не установлен", r"не предусмотрен",
    r"нет (?:информации|данных|сведений|упоминани)", r"не (?:располага|наш(?:ел|ла|лось))",
    r"не знаю", r"не могу (?:сказать|ответить|найти|подтвердить)", r"неизвест", r"не (?:слышал|встречал)",
    r"информаци\w* (?:об? \w+ )?(?:нет|отсутству)", r"отсутству\w* (?:информаци|данн|сведени)",
    # RU: negation of existence ("нет такого романа", "такого закона нет", "X у Толстого нет.")
    r"нет так\w+", r"(?<![\w-])так\w+ \w+(?: \w+)? (?:нет|не существует)", r"не существу",
    r"(?<=\w)[»\")]?\s+нет\s*(?:[.;!?…]|$)",
    # EN
    r"not (?:specified|mentioned|covered|defined|stated|documented|aware|familiar)",
    r"no (?:information|data|record|mention|details|such)", r"there(?: is|'s| are| was| were) no\b",
    r"(?:do not|don't|doesn't|does not|did not|didn't) (?:know|specify|have|contain|mention|exist)",
    r"(?:unable|not able) to (?:find|answer|provide|confirm)", r"(?:cannot|can't) (?:find|answer|provide|confirm)",
    r"i'?m not sure", r"never (?:existed|wrote|written|published)", r"fictional", r"not a real", r"unknown",
]))
_SENT = re.compile(r"(?<=[.!?…])\s+")
_WEEKDAY = re.compile(r"понедельник|вторник|сред[аеуы]\b|четверг|пятниц|суббот|воскресен|"
                      r"monday|tuesday|wednesday|thursday|friday|saturday|sunday")
_CAP = re.compile(r"[A-ZА-ЯЁ][a-zа-яё]{2,}")
# Capitalized words that are not a "specific value" (domain words, polite «Вы»).
GENERIC_CAPS = ("agile", "scrum", "kanban", "product", "owner", "guide", "daily", "sprint", "review",
                "retrospective", "planning", "backlog", "вы", "вам", "вас", "ваш", "регламент", "компани")


def _norm_low(text: str) -> str:
    return text.lower().replace("ё", "е").replace("’", "'")


def refusal_pattern(text: str) -> bool:
    """Any refusal / non-existence formula anywhere in the text."""
    return bool(_REFUSAL.search(_norm_low(text)))


def has_specific_value(text: str, allow: Iterable[str] = (), names: bool = True, context: str = "") -> bool:
    """A concrete value: a number (digits or words ≥2), a time, a weekday, or (names=True) a capitalized word
    in mid-sentence that does not start with an allowed stem (company name, GENERIC_CAPS).
    Numbers that occur in `context` (the question) are not values."""
    known = set(re.findall(r"\d+", normalize(context)))
    raw_digits = set(re.findall(r"\d+", text)) - known
    if raw_digits or set(re.findall(r"\d+", normalize(text))) - {"0", "1"} - known:
        return True
    if _WEEKDAY.search(_norm_low(text)):
        return True
    if names:
        stems = tuple(a.lower() for a in (*GENERIC_CAPS, *allow))
        for sent in _SENT.split(text.strip()):
            words = re.findall(r"[\w-]+", sent)
            for w in words[1:]:
                if _CAP.fullmatch(w) and not w.lower().replace("ё", "е").startswith(stems):
                    return True
    return False


def is_refusal(text: str, allow: Iterable[str] = (), names: bool = True, context: str = "") -> bool:
    """Refusal = the first sentence is a refusal formula without a value, or a refusal formula anywhere and no
    value in the whole answer. names=False: proper names do not count as values (OOD questions about invented
    entities, where the refusal repeats the invented name). «Нет, это неверно, итерация 13 дней» is not a refusal.
    """
    allow = tuple(allow)
    first = _SENT.split(text.strip(), maxsplit=1)[0] if text.strip() else ""
    if refusal_pattern(first) and not has_specific_value(first, allow, names, context):
        return True
    return refusal_pattern(text) and not has_specific_value(text, allow, names, context)


# ---------- lexical overlap ----------

def char_ngrams(text: str, n: int = 3) -> set[str]:
    t = " ".join(re.sub(r"[^\w\s]", " ", text.lower().replace("ё", "е")).split())
    return {t[i:i + n] for i in range(max(len(t) - n + 1, 0))}


def jaccard(a: set[str], b: set[str]) -> float:
    return len(a & b) / len(a | b) if a or b else 0.0


def max_jaccard(text: str, others: Iterable[str], n: int = 3) -> float:
    g = char_ngrams(text, n)
    return max((jaccard(g, char_ngrams(o, n)) for o in others), default=0.0)
