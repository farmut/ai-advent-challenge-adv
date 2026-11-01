"""Self-check for data/facts/eval_gen.jsonl (GEN_FACTS_EVAL). Exit 1 on any failure."""
import datetime as dt
import json
import re
import sys
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
FACTS = {f["fact_id"]: f for f in json.loads((ROOT / "facts" / "regulation.json").read_text(encoding="utf-8"))}
ROWS = [json.loads(l) for l in (ROOT / "data" / "facts" / "eval_gen.jsonl").read_text(encoding="utf-8").splitlines() if l.strip()]
FORBIDDEN = [r"первую неделю", r"дополните", r"правда ли", r"планируем релиз", r"я тут", r"нович", r"письм", r"руководител",
             r"fill in", r"is it true", r"составляю", r"готовлю", r"чек-лист", r"в других компаниях"]
POLAR = {"да", "нет", "no", "yes", "нельзя", "не мож", "not blocked", "успе", "провод", "превыс", "ниже"}
errors = []


def norm(s):
    return s.lower().replace("ё", "е")


def num(fid, i=0):
    return int(next(k for k in FACTS[fid]["answer_keys"] if k.isdigit())) if i == 0 else None


def hm(fid):
    k = next(k for k in FACTS[fid]["answer_keys"] if ":" in k)
    h, m = map(int, k.split(":"))
    return dt.datetime(2026, 3, 2, h, m)


def t(d):
    return d.strftime("%H:%M")


def err(rid, msg):
    errors.append(f"{rid}: {msg}")


# --- structure / counts
ids = [r["id"] for r in ROWS]
if len(ids) != len(set(ids)):
    err("ALL", f"duplicate ids {[i for i, c in Counter(ids).items() if c > 1]}")
by = Counter(r["strata"] for r in ROWS)
exp = {"b": 60, "c": 30, "d": 10, "control": 10}
if dict(by) != exp:
    err("ALL", f"strata counts {dict(by)} != {exp}")
b = [r for r in ROWS if r["strata"] == "b"]
bl = Counter(r["lang"] for r in b)
if bl != Counter(ru=40, en=20):
    err("b", f"lang counts {dict(bl)}")
bf = Counter(r["fact_ids"][0] for r in b)
train = sorted(f for f, x in FACTS.items() if x["split"] == "train")
control = sorted(f for f, x in FACTS.items() if x["split"] == "control")
if any(bf[f] != 2 for f in train) or set(bf) != set(train):
    err("b", f"per-fact counts wrong: {dict(bf)}")
for f in train:
    langs = sorted(r["lang"] for r in b if r["fact_ids"][0] == f)
    if "ru" not in langs:
        err("b", f"{f} has no RU row")
cf = Counter(r["fact_ids"][0] for r in ROWS if r["strata"] == "control")
if sorted(cf) != control or any(v != 1 for v in cf.values()):
    err("control", f"per-fact counts wrong: {dict(cf)}")
c = [r for r in ROWS if r["strata"] == "c"]
if Counter(len(r["fact_ids"]) for r in c) != Counter({2: 15, 1: 15}):
    err("c", "need 15 composition (2 ids) + 15 application (1 id)")
for r in ROWS:
    rid = r["id"]
    for k in ("id", "task", "fact_ids", "strata", "template_id", "lang", "question", "answer_keys", "require_all"):
        if k not in r:
            err(rid, f"missing {k}")
    if r["task"] != "facts" or r["lang"] not in ("ru", "en") or not r["answer_keys"]:
        err(rid, "bad task/lang/keys")
    for f in r["fact_ids"]:
        if f not in FACTS:
            err(rid, f"unknown fact {f}")
    if r["strata"] in ("b", "c", "d") and any(FACTS[f]["split"] != "train" for f in r["fact_ids"]):
        err(rid, "non-train fact in b/c/d")
    if r["strata"] == "control" and FACTS[r["fact_ids"][0]]["split"] != "control":
        err(rid, "control row on train fact")
    if r["strata"] == "d" and FACTS[r["fact_ids"][0]]["kind"] not in ("name", "term", "time"):
        err(rid, "d must use name/term/time fact")
    if len(r["fact_ids"]) == 2:
        a, bb = r["fact_ids"]
        if bb not in FACTS[a]["related"] and a not in FACTS[bb]["related"]:
            err(rid, f"{a},{bb} not related")
    for k in r["answer_keys"]:
        if k != norm(k):
            err(rid, f"key not normalized: {k}")
    q = norm(r["question"])
    if r["strata"] != "d":
        for k in r["answer_keys"]:
            for alt in k.split("|"):
                if alt in q:
                    err(rid, f"key alt '{alt}' occurs in question")
    for pat in FORBIDDEN:
        if re.search(pat, q):
            err(rid, f"forbidden frame /{pat}/")
    # EN: every non-numeric key also accepts a Cyrillic alternative
    if r["lang"] == "en":
        for k in r["answer_keys"]:
            if not all(re.search(r"[0-9]", x) for x in k.split("|")[:1]) and not re.search(r"[а-я]", k):
                err(rid, f"EN key without Cyrillic alternative: {k}")
    # c application: no polar (yes/no) keys
    if r["strata"] == "c":
        for k in r["answer_keys"]:
            if set(k.split("|")) & POLAR:
                err(rid, f"polar key {k}")
    # b/control: every true fact key must be covered by one of the row's keys
    if r["strata"] in ("b", "control"):
        f = FACTS[r["fact_ids"][0]]
        true = f["answer_keys"] if r["lang"] == "ru" else f["answer_keys_en"]
        alts = [set(k.split("|")) for k in r["answer_keys"]]
        for tk in true:
            if not any(tk in a for a in alts):
                err(rid, f"fact key '{tk}' not in row keys {r['answer_keys']}")

# --- recompute c calculations from facts
D = {r["id"]: r for r in c}


def has(rid, value):
    if not any(value in k.split("|") for k in D[rid]["answer_keys"]):
        err(rid, f"expected key '{value}' not in {D[rid]['answer_keys']}")


def cond(rid, ok, msg):
    if not ok:
        err(rid, f"calc premise false: {msg}")


F = lambda fid: num(fid)
mon = dt.datetime(2026, 3, 2)  # Monday
assert mon.weekday() == 0
has("FE-c-001", str(F("F02") // F("F01"))); cond("FE-c-001", F("F02") % F("F01") == 0, "52/13 integer")
has("FE-c-002", str(5 * F("F01")))  # 5 regular iterations between two Shtil
f26h = int([k for k in FACTS["F26"]["answer_keys"] if k.isdigit()][0])
has("FE-c-003", str(F("F19") - f26h))
plan = dt.datetime(2026, 3, 2, *map(int, FACTS["F17"]["answer_keys"][1].split(":")))
has("FE-c-004", t(plan + dt.timedelta(minutes=F("F04"))))
has("FE-c-005", t(hm("F16") + dt.timedelta(minutes=F("F05"))))
has("FE-c-006", str(5 * F("F05")))
has("FE-c-007", t(hm("F16")))
has("FE-c-008", t(hm("F18"))); has("FE-c-010", t(hm("F18"))); has("FE-c-012", t(hm("F20")))
has("FE-c-013", str(25 - F("F09")))
has("FE-c-014", str(F("F08"))); has("FE-c-015", str(F("F07")))
has("FE-c-016", str((hm("F16") - dt.datetime(2026, 3, 2, 10, 5)).seconds // 60))
parts = -(-40 // F("F03")); cond("FE-c-017", 40 / parts <= F("F03") and 40 / (parts - 1) > F("F03"), "min parts"); has("FE-c-017", str(parts))
has("FE-c-018", str(F("F09") - 19))
has("FE-c-019", str(-(-2000 * F("F08") // 100) - 1500))
has("FE-c-020", str(-(-75 * F("F06") // 100)))
has("FE-c-021", str(F("F07") - 226))
x = mon.replace(hour=10) + dt.timedelta(hours=F("F30"))
cond("FE-c-022", x.weekday() == 2, f"{x} is Wednesday"); has("FE-c-022", t(x).lstrip("0") if t(x)[0] == "0" else t(x))
sat = mon + dt.timedelta(days=5, hours=20)
x = sat - dt.timedelta(hours=F("F19"))
cond("FE-c-023", x.weekday() == 4, f"{x} is Friday"); has("FE-c-023", t(x).lstrip("0"))
x = mon + dt.timedelta(days=1, hours=10 + f26h)
cond("FE-c-024", x.weekday() == 2, f"{x} is Wednesday"); has("FE-c-024", t(x))
d, n = mon, 0
while True:
    if d.weekday() < 5:
        n += 1
        if n == F("F01"):
            break
    d += dt.timedelta(days=1)
has("FE-c-025", str(d.day))
has("FE-c-026", t(mon.replace(hour=10) + dt.timedelta(minutes=F("F04"))))
has("FE-c-027", str(25 - F("F05")))
has("FE-c-028", str((plan - mon.replace(hour=14)).seconds // 60))
has("FE-c-029", str((hm("F20") - mon.replace(hour=11, minute=20)).seconds // 60))
days = ["понедельник", "вторник", "среда", "четверг", "пятница"]; missed = [0, 1, 2]  # mon..wed
first = next(missed[i] for i in range(1, len(missed)) if missed[i] - missed[i - 1] == 1); has("FE-c-030", days[first])

print(f"rows={len(ROWS)} strata={dict(by)} b_lang={dict(bl)} c_comp={sum(len(r['fact_ids'])==2 for r in c)} "
      f"c_lang={dict(Counter(r['lang'] for r in c))}")
if errors:
    print("FAIL"); print("\n".join(errors)); sys.exit(1)
print("OK: all checks passed")
