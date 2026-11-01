"""Deterministic proxies for the 6 target-style rules (gen/SCHEMA.md, section data/style).

check(text) -> {"vy", "no_exclaim", "no_intro", "no_hedge", "short_sentences", "length_plain", "all"}
"""

import re
import sys

RULES_ORDER = ["vy", "no_exclaim", "no_intro", "no_hedge", "short_sentences", "length_plain", "all"]
MAX_WORDS = 15
MIN_CHARS, MAX_CHARS = 250, 450

# Rule 1: informal «ты» forms as separate words.
TY_RE = re.compile(
    r"(?<![\wё])(ты|тебе|тебя|тобой|тобою|твой|твоя|твоё|твое|твои|твоего|твоему|твоим|"
    r"твоей|твою|твоих|твоими)(?![\wё])",
    re.IGNORECASE,
)

# Rule 3: intro filler (stop-list from SCHEMA.md), anywhere in the text.
INTRO_PHRASES = ["отличный вопрос", "хороший вопрос", "конечно", "давайте разберемся", "безусловно"]

# Rule 4: hedging words (stop-list from SCHEMA.md).
HEDGE_PHRASES = ["возможно", "как правило", "обычно", "может быть", "скорее всего", "в целом", "наверное"]

ABBREVIATIONS = ["т.е.", "т.д.", "т.п.", "т.к.", "т.н.", "и др.", "и пр.", "напр.", "см.", "др.", "г.", "гг.",
                 "e.g.", "i.e.", "etc."]
_DOT = "․"  # placeholder for a protected dot

WORD_RE = re.compile(r"[0-9A-Za-zА-Яа-яЁё]+(?:[-'’][0-9A-Za-zА-Яа-яЁё]+)*")
EMOJI_RE = re.compile(
    "[\U0001F000-\U0001FAFF\U00002600-\U000027BF\U0001F900-\U0001F9FF⬀-⯿️‍]"
)
MD_LINE_RE = re.compile(r"^\s*(#{1,6}\s|[-*+•]\s|\d+[.)]\s|>)")
MD_INLINE_RE = re.compile(r"(\*\*|__|`|\*[^*\s]|\[[^\]]+\]\([^)]+\)|^#|\n#)")


def _norm(text: str) -> str:
    return text.lower().replace("ё", "е")


def _phrase_re(phrase: str) -> re.Pattern:
    words = [re.escape(w) for w in _norm(phrase).split()]
    return re.compile(r"(?<![\w])" + r"\s+".join(words) + r"(?![\w])")


# G3 round 2 (blind labels disagreed on greetings, "давайте разберём" in any form, evaluative openers):
# generic intro-filler families, matched as word stems anywhere in the text.
INTRO_STEMS = {  # label -> regex on normalized text (ё->е, lowercase)
    "привет": r"привет(?:ствую|ствуем)?(?![\w])",
    "здравствуйте": r"здравствуй(?:те)?(?![\w])",
    "добрый день": r"добр(?:ый|ого) (?:день|дня|утро|вечер)(?![\w])",
    "давайте разберем": r"давайте (?:разбер|посмотр|рассмотр)\w*",
    "оценка вопроса": r"(?:отличн|хорош|интересн|важн|правильн)\w* вопрос(?:ы|а)?(?![\w])",
    "заслуживает внимания": r"заслужива\w* внимани\w*",
    "рад помочь": r"рады? помочь(?![\w])",
}
INTRO_RES = [_phrase_re(p) for p in INTRO_PHRASES] + [re.compile(r"(?<![\w])" + v) for v in INTRO_STEMS.values()]
INTRO_PHRASES = INTRO_PHRASES + list(INTRO_STEMS)
HEDGE_RES = [_phrase_re(p) for p in HEDGE_PHRASES]


def split_sentences(text: str) -> list[str]:
    """Split on . ? ! … keeping abbreviations (т.е., т.д.) and decimals (3.5) intact."""
    s = text.strip()
    for abbr in sorted(ABBREVIATIONS, key=len, reverse=True):
        pat = re.compile(r"(?<![\w])" + re.escape(abbr), re.IGNORECASE)
        s = pat.sub(lambda m: m.group(0).replace(".", _DOT), s)
    s = re.sub(r"(?<=\d)\.(?=\d)", _DOT, s)  # decimals / versions
    parts = re.split(r"(?<=[.?!…])[»\")]*\s+", s)
    return [p.replace(_DOT, ".").strip() for p in parts if WORD_RE.search(p)]


def count_words(sentence: str) -> int:
    return len(WORD_RE.findall(sentence))


def find_hits(text: str, regexes: list[re.Pattern], phrases: list[str]) -> list[str]:
    n = _norm(text)
    return [p for p, r in zip(phrases, regexes) if r.search(n)]


def check(text: str) -> dict:
    sentences = split_sentences(text)
    stripped = text.strip()
    plain = not any(MD_LINE_RE.match(line) for line in stripped.splitlines()) \
        and not MD_INLINE_RE.search(stripped) and not EMOJI_RE.search(stripped)
    res = {
        "vy": TY_RE.search(text) is None,
        "no_exclaim": "!" not in text,
        "no_intro": not find_hits(text, INTRO_RES, INTRO_PHRASES),
        "no_hedge": not find_hits(text, HEDGE_RES, HEDGE_PHRASES),
        "short_sentences": bool(sentences) and max(count_words(x) for x in sentences) <= MAX_WORDS,
        "length_plain": MIN_CHARS <= len(stripped) <= MAX_CHARS and plain,
    }
    res["all"] = all(res.values())
    return res


def explain(text: str) -> str:
    """Human-readable reason for failures (for regeneration)."""
    out = []
    r = check(text)
    if not r["vy"]:
        out.append(f"ty: {TY_RE.findall(text)}")
    if not r["no_exclaim"]:
        out.append("exclaim")
    if not r["no_intro"]:
        out.append(f"intro: {find_hits(text, INTRO_RES, INTRO_PHRASES)}")
    if not r["no_hedge"]:
        out.append(f"hedge: {find_hits(text, HEDGE_RES, HEDGE_PHRASES)}")
    if not r["short_sentences"]:
        out += [f"long({count_words(s)}): {s}" for s in split_sentences(text) if count_words(s) > MAX_WORDS]
    if not r["length_plain"]:
        out.append(f"len={len(text.strip())}")
    return "; ".join(out)


def _selftest() -> None:
    assert split_sentences("Это т.е. пример, и т.д. и т.п. подобное. Версия 3.5 вышла. Да? Нет… Ок.") == [
        "Это т.е. пример, и т.д. и т.п. подобное.", "Версия 3.5 вышла.", "Да?", "Нет…", "Ок."]
    assert not check("Ты прав.")["vy"] and check("Тысяча вариантов.")["vy"]
    assert not check("Это, наверное, так.")["no_hedge"] and check("Это невозможно.")["no_hedge"]
    assert check("Обычными средствами.")["no_hedge"] and not check("Обычно так.")["no_hedge"]
    assert not check("Конечно, да.")["no_intro"] and check("Конечный результат.")["no_intro"]
    assert not check("- пункт")["length_plain"]


if __name__ == "__main__":
    _selftest()
    import json
    paths = sys.argv[1:]
    bad = total = 0
    for path in paths:
        for line in open(path, encoding="utf-8"):
            row = json.loads(line)
            total += 1
            if not check(row["answer"])["all"]:
                bad += 1
                print(row["id"], explain(row["answer"]))
    print(f"selftest ok; checked {total}, failed {bad}")
