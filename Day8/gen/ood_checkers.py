"""Deterministic checkers for Day8 OOD ifeval / dissimilar rows.

check(checker, text) -> bool, where checker = {"type": ..., "args": {...}}.
"none" always returns True (manual labelling via `note`).
"""
import json
import re

WORD_RE = re.compile(r"[^\W_]+(?:['’\-][^\W_]+)*")
BULLET_RE = re.compile(r"^\s*[-*•]\s+\S")


def words(text):
    return WORD_RE.findall(text)


def _strip_fences(text):
    t = text.strip()
    m = re.match(r"^```[a-zA-Z]*\s*\n(.*?)\n?```\s*$", t, re.S)
    return m.group(1).strip() if m else t


def _word_count_max(text, a):
    return len(words(text)) <= a["max"]


def _word_count_min(text, a):
    return len(words(text)) >= a["min"]


def _no_commas(text, a):
    return "," not in text and "，" not in text


def _lowercase(text, a):
    return any(c.isalpha() for c in text) and text == text.lower()


def _uppercase(text, a):
    return any(c.isalpha() for c in text) and text == text.upper()


def _include_keyword(text, a):
    kw = re.escape(a["keyword"].lower())
    hits = re.findall(r"(?<![^\W_])" + kw + r"(?![^\W_])", text.lower())
    return len(hits) >= a.get("n", 1)


def _n_bullets(text, a):
    return sum(1 for ln in text.splitlines() if BULLET_RE.match(ln)) == a["n"]


def _json_keys(text, a):
    try:
        obj = json.loads(_strip_fences(text))
    except (ValueError, TypeError):
        return False
    return isinstance(obj, dict) and all(k in obj for k in a["keys"])


def _ends_with(text, a):
    t = text.rstrip().rstrip("*\"'`»” ").rstrip()
    return t.lower().endswith(a["phrase"].lower())


def _n_paragraphs(text, a):
    paras = [p for p in re.split(r"\n\s*\n", text.strip()) if p.strip()]
    return len(paras) == a["n"]


CHECKERS = {
    "word_count_max": _word_count_max,
    "word_count_min": _word_count_min,
    "no_commas": _no_commas,
    "lowercase": _lowercase,
    "uppercase": _uppercase,
    "include_keyword": _include_keyword,
    "n_bullets": _n_bullets,
    "json_keys": _json_keys,
    "ends_with": _ends_with,
    "n_paragraphs": _n_paragraphs,
}
KNOWN_TYPES = set(CHECKERS) | {"none"}


def check(checker, text):
    t = checker["type"]
    if t == "none":
        return True
    if t not in CHECKERS:
        raise ValueError(f"unknown checker type: {t}")
    return bool(CHECKERS[t](text, checker.get("args", {})))


SELF_TESTS = [
    # (checker, good_text, bad_text)
    ({"type": "word_count_max", "args": {"max": 5}}, "Lava rises and cools.", "Lava rises from deep below and slowly cools down."),
    ({"type": "word_count_min", "args": {"min": 6}}, "Bees carry pollen between many flowers daily.", "Bees carry pollen."),
    ({"type": "no_commas", "args": {}}, "The sun rose. The sea glowed.", "The sun rose, and the sea glowed."),
    ({"type": "lowercase", "args": {}}, "penguins live in the south.", "Penguins live in the south."),
    ({"type": "uppercase", "args": {}}, "THUNDER IS LOUD.", "THUNDER is loud."),
    ({"type": "include_keyword", "args": {"keyword": "cinnamon", "n": 2}}, "Cinnamon first, then more cinnamon.", "Add cinnamon and sugar. Cinnamonroll."),
    ({"type": "n_bullets", "args": {"n": 3}}, "Benefits:\n- a\n- b\n* c", "- a\n- b"),
    ({"type": "json_keys", "args": {"keys": ["name", "year"]}}, '```json\n{"name": "Neil Armstrong", "year": 1969}\n```', '{"name": "Neil Armstrong"}'),
    ({"type": "ends_with", "args": {"phrase": "The end."}}, "The fox ran away. The end.\n", "The end. The fox ran away."),
    ({"type": "n_paragraphs", "args": {"n": 2}}, "Water evaporates.\n\nThen it rains.", "Water evaporates. Then it rains."),
]

if __name__ == "__main__":
    assert {c["type"] for c, _, _ in SELF_TESTS} == set(CHECKERS), "self-test must cover every type"
    ok = 0
    for c, good, bad in SELF_TESTS:
        g, b = check(c, good), check(c, bad)
        status = "OK" if (g and not b) else "FAIL"
        ok += status == "OK"
        print(f"{status:4} {c['type']:16} good={g} bad={b}")
    assert check({"type": "none", "args": {}}, "anything")
    print(f"self-test: {ok}/{len(SELF_TESTS)} passed")
    raise SystemExit(0 if ok == len(SELF_TESTS) else 1)
