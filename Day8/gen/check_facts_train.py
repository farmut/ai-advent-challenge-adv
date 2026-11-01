"""Self-check for facts/regulation.json and data/facts/{train,valid}.jsonl. Exit code 1 on any error."""

import json
import re
import sys
from collections import Counter
from pathlib import Path

DAY8 = Path(__file__).resolve().parent.parent
errors: list[str] = []


def norm(s: str) -> str:
    return s.lower().replace("ё", "е")


def load_jsonl(p: Path) -> list:
    rows = []
    for n, line in enumerate(p.read_text(encoding="utf-8").splitlines(), 1):
        try:
            rows.append(json.loads(line))
        except json.JSONDecodeError as e:
            errors.append(f"{p.name}:{n} invalid JSON: {e}")
    return rows


def has_key(text: str, key: str) -> bool:
    t, k = norm(text), norm(key)
    if k.replace(":", "").isdigit():  # numbers/times: not part of a longer number
        return re.search(rf"(?<![\d:]){re.escape(k)}(?![\d])", t) is not None
    return k in t


reg = json.loads((DAY8 / "facts/regulation.json").read_text(encoding="utf-8"))
company = json.loads((DAY8 / "facts/company.json").read_text(encoding="utf-8"))
byid = {f["fact_id"]: f for f in reg}
split = Counter(f["split"] for f in reg)
if len(reg) != 40 or len(byid) != 40 or split != Counter(train=30, control=10):
    errors.append(f"regulation: {len(reg)} facts, splits {dict(split)}")
for f in reg:
    if not f["answer_keys"] or not f["answer_keys_en"]:
        errors.append(f"{f['fact_id']}: empty keys")
    for k in f["answer_keys"]:
        if k != norm(k) or not has_key(f["statement"], k):
            errors.append(f"{f['fact_id']}: key {k!r} not normalized or absent in statement")
    for r in f["related"]:
        if r not in byid or f["fact_id"] not in byid[r]["related"]:
            errors.append(f"{f['fact_id']}: bad/asymmetric related {r}")
pairs = {tuple(sorted((f["fact_id"], r))) for f in reg for r in f["related"]}
if len(pairs) < 15:
    errors.append(f"related pairs {len(pairs)} < 15")
kinds = {s: Counter(f["kind"] for f in reg if f["split"] == s) for s in ("train", "control")}

train = load_jsonl(DAY8 / "data/facts/train.jsonl")
valid = load_jsonl(DAY8 / "data/facts/valid.jsonl")
train_ids = sorted(f["fact_id"] for f in reg if f["split"] == "train")
if len(train) != 150 or len(valid) != 30:
    errors.append(f"sizes train={len(train)} valid={len(valid)}")
ids = [r["id"] for r in train + valid]
if len(ids) != len(set(ids)):
    errors.append("duplicate ids")
for rows, strata, tmpls in ((train, "train", {"T1", "T2", "T3", "T4", "T5"}), (valid, "valid", {"V1"})):
    per = Counter(r["fact_id"] for r in rows)
    if sorted(per) != train_ids or set(per.values()) != {len(tmpls)}:
        errors.append(f"{strata}: per-fact coverage wrong {dict(per)}")
    for r in rows:
        fid = r["fact_id"]
        if r["strata"] != strata or r["template_id"] not in tmpls or r["task"] != "facts" or r["lang"] != "ru":
            errors.append(f"{r['id']}: bad meta")
        if byid.get(fid, {}).get("split") != "train":
            errors.append(f"{r['id']}: not a train fact")
            continue
        if r["answer_keys"] != byid[fid]["answer_keys"]:
            errors.append(f"{r['id']}: answer_keys differ from regulation")
        for k in r["answer_keys"]:
            if not has_key(r["answer"], k):
                errors.append(f"{r['id']}: key {k!r} missing in answer")
            if has_key(r["question"], k):
                errors.append(f"{r['id']}: key {k!r} leaked into question")
        a = r["answer"]
        if "!" in a or re.search(r"\b(ты|тебе|тебя|твой|твоя|твои)\b", norm(a)):
            errors.append(f"{r['id']}: style (! or ты)")
        if len(re.findall(r"[.?]\s", a + " ")) > 2:
            errors.append(f"{r['id']}: answer longer than 2 sentences")

# Control facts must not leak anywhere in train/valid (keys, ru/en, and the answer value).
blob = "\n".join(r["question"] + "\n" + r["answer"] for r in train + valid)
for f in reg:
    if f["split"] != "control":
        continue
    for k in f["answer_keys"] + f["answer_keys_en"]:
        if has_key(blob, k):
            errors.append(f"control {f['fact_id']}: key {k!r} found in train/valid")
    if norm(f["answer"]) in norm(blob):
        errors.append(f"control {f['fact_id']}: value found in train/valid")

print(f"company: {company['name']} / {company['name_en']}")
print(f"facts: {dict(split)}; kinds train={dict(kinds['train'])} control={dict(kinds['control'])}; related pairs={len(pairs)}")
print(f"train={len(train)} valid={len(valid)} unique_ids={len(set(ids))}")
if errors:
    print(f"FAIL: {len(errors)} errors")
    for e in errors:
        print(" -", e)
    sys.exit(1)
print("OK")
