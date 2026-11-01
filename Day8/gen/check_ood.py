"""Self-check for data/ood/eval.jsonl and data/dissimilar/eval.jsonl. Exit 1 on any failure."""
import collections
import json
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
from make_arith import generate, safe_eval  # noqa: E402
from ood_checkers import CHECKERS, KNOWN_TYPES, SELF_TESTS, check, words  # noqa: E402

ROOT = os.path.dirname(HERE)
CYR = re.compile(r"[а-яё]", re.I)
DOMAIN = re.compile(r"agile|scrum|kanban|спринт|ретро|регламент|компани|стендап|story point", re.I)
errors = []


def err(msg):
    errors.append(msg)


def load(rel):
    with open(os.path.join(ROOT, rel), encoding="utf-8") as f:
        return [json.loads(line) for line in f if line.strip()]


ood, ds = load("data/ood/eval.jsonl"), load("data/dissimilar/eval.jsonl")

# ids
ids = [r["id"] for r in ood + ds]
dup = [i for i, n in collections.Counter(ids).items() if n > 1]
if dup:
    err(f"duplicate ids: {dup}")

# --- OOD ---
OOD_FIELDS = {"id", "task", "category", "prompt", "gold", "answer_keys", "checker"}
want = {"arith": 30, "ifeval": 25, "knowledge": 25, "translate": 10, "nonexistent": 10}
cat = collections.Counter(r["category"] for r in ood)
if len(ood) != 100 or dict(cat) != want:
    err(f"ood counts {len(ood)} {dict(cat)}")
for r in ood:
    if set(r) != OOD_FIELDS or r["task"] != "ood":
        err(f"{r['id']}: fields {sorted(r)}")
    if r["checker"]["type"] not in KNOWN_TYPES:
        err(f"{r['id']}: unknown checker {r['checker']['type']}")
    if r["category"] != "ifeval" and r["checker"]["type"] != "none":
        err(f"{r['id']}: non-ifeval with checker")
    if r["category"] != "translate" and DOMAIN.search(r["prompt"]):
        err(f"{r['id']}: domain word in OOD prompt")

ar = [r for r in ood if r["category"] == "arith"]
regen = {a["id"]: a for a in generate(42)}
ar_ok = 0
for r in ar:
    a = regen.get(r["id"])
    if not a or a["prompt"] != r["prompt"] or str(safe_eval(a["expr"])) != r["gold"] or r["answer_keys"] != [r["gold"]]:
        err(f"{r['id']}: arith mismatch")
    elif not (r["prompt"].endswith("В конце ответа напишите итоговое число.") or r["prompt"].endswith("End with the final number.")):
        err(f"{r['id']}: missing final-number instruction")
    else:
        ar_ok += 1
ar_lang = collections.Counter("ru" if CYR.search(r["prompt"]) else "en" for r in ar)

ife = [r for r in ood if r["category"] == "ifeval"]
ife_types = collections.Counter(r["checker"]["type"] for r in ife)
for t in CHECKERS:
    if ife_types[t] < 2:
        err(f"ifeval type {t} used {ife_types[t]} < 2")
for r in ife:
    if CYR.search(r["prompt"]):
        err(f"{r['id']}: ifeval must be English")
    if not r["checker"]["args"] and r["checker"]["type"] not in ("no_commas", "lowercase", "uppercase"):
        err(f"{r['id']}: empty args")

kn = [r for r in ood if r["category"] == "knowledge"]
kn_lang = collections.Counter("ru" if CYR.search(r["prompt"]) else "en" for r in kn)
if dict(kn_lang) != {"ru": 13, "en": 12}:
    err(f"knowledge langs {dict(kn_lang)}")
for r in kn:
    if not r["answer_keys"] or any(k != k.lower().replace("ё", "е") for k in r["answer_keys"]):
        err(f"{r['id']}: answer_keys must be non-empty normalized")
    if any(k in r["prompt"].lower() for k in r["answer_keys"]):
        err(f"{r['id']}: answer key leaks into prompt")

for r in ood:
    if r["category"] == "translate":
        src = r["prompt"].split("\n\n", 1)[1]
        n = len(words(src))
        if not (10 <= n <= 20) or not CYR.search(r["gold"]):
            err(f"{r['id']}: translate src words={n}")
    if r["category"] == "nonexistent" and r["gold"] != "refusal":
        err(f"{r['id']}: nonexistent gold")
ne_lang = collections.Counter("ru" if CYR.search(r["prompt"]) else "en" for r in ood if r["category"] == "nonexistent")

# --- dissimilar ---
DS_FIELDS = {"id", "task", "group", "lang", "prompt", "checker", "note"}
grp = collections.Counter(r["group"] for r in ds)
if len(ds) != 32 or dict(grp) != {1: 8, 2: 8, 3: 8, 4: 8}:
    err(f"dissimilar counts {len(ds)} {dict(grp)}")
for r in ds:
    if set(r) - {"answer_keys"} != DS_FIELDS or r["task"] != "dissimilar" or not r["note"]:
        err(f"{r['id']}: fields")
    if r["checker"]["type"] not in KNOWN_TYPES:
        err(f"{r['id']}: unknown checker {r['checker']['type']}")
    if r["lang"] != ("ru" if CYR.search(r["prompt"]) else "en"):
        err(f"{r['id']}: lang mismatch")
    if not r["id"].startswith(f"DS-{r['group']}-"):
        err(f"{r['id']}: id/group mismatch")
ds_auto = sum(r["checker"]["type"] != "none" for r in ds)

# checker self-test
st = sum(check(c, g) and not check(c, b) for c, g, b in SELF_TESTS)
if st != len(SELF_TESTS):
    err(f"checker self-test {st}/{len(SELF_TESTS)}")

print("ood:", dict(cat), "| arith langs", dict(ar_lang), f"| arith recomputed {ar_ok}/{len(ar)}")
print("ifeval types:", dict(ife_types))
print("knowledge langs:", dict(kn_lang), "| nonexistent langs:", dict(ne_lang))
print("dissimilar groups:", dict(grp), f"| auto checkers {ds_auto}/{len(ds)}",
      dict(collections.Counter(r["checker"]["type"] for r in ds)))
print(f"unique ids: {len(set(ids))}/{len(ids)} | checker self-test {st}/{len(SELF_TESTS)}")
if errors:
    print("FAIL:\n  " + "\n  ".join(errors))
    sys.exit(1)
print("OK")
