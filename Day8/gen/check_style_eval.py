"""Self-check for data/style/eval.jsonl (see gen/GEN_STYLE_EVAL.md)."""
import json, re, sys
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
rows = [json.loads(l) for l in (ROOT / "data/style/eval.jsonl").read_text(encoding="utf-8").splitlines() if l.strip()]
used = set(json.loads((ROOT / "data/style/topics_used.json").read_text(encoding="utf-8"))["all"])
err = []
if len(rows) != 40: err.append(f"rows={len(rows)} != 40")
ids = [r.get("id") for r in rows]
if len(set(ids)) != len(ids): err.append("duplicate ids")
if any(not re.fullmatch(r"SE-\d{3}", i or "") for i in ids): err.append("bad id format")
qs = [r.get("question", "").strip() for r in rows]
if len(set(qs)) != len(qs): err.append("duplicate questions")
for r in rows:
    if r.get("task") != "style": err.append(f"{r.get('id')}: task != style")
    t = r.get("topic_id", "")
    if not re.fullmatch(r"[a-z0-9]+(-[a-z0-9]+)*", t): err.append(f"{r['id']}: topic_id not kebab-case")
    if t in used: err.append(f"{r['id']}: topic_id {t} in topics_used.all")
    if not r.get("question"): err.append(f"{r['id']}: empty question")
c = Counter(r.get("topic_id") for r in rows)
if len(c) < 20: err.append(f"topics={len(c)} < 20")
for t, n in c.items():
    if n > 3: err.append(f"topic {t}: {n} > 3")
print(f"rows={len(rows)} topics={len(c)} max_per_topic={max(c.values())}")
print("\n".join(err) if err else "OK")
sys.exit(1 if err else 0)
