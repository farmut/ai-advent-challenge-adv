"""Merge audit rounds: data/audit1.json overridden by data/audit2.json -> data/audit.json.

Rule fixed before the rewrite (same as Day7): a row is rewritten once; if it fails round 2
(audit status != OK, or lexical G2 Jaccard > 0.3 after the rewrite) it is excluded, not rewritten again.
Excluded rows go to data/excluded.json {id: reason} and are removed from audit.json.
usage: python -I gen/merge_audit.py [--g2-fail ID ...]
"""
import argparse
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

ap = argparse.ArgumentParser()
ap.add_argument("--g2-fail", nargs="*", default=[], help="ids that still fail G2 Jaccard after the rewrite")
ap.add_argument("--manual", nargs="*", default=[],
                help="ids whose gold is right but the automatic scorer is unreliable: kept, scored by blind manual labels")
args = ap.parse_args()

a1 = json.loads((ROOT / "data/audit1.json").read_text(encoding="utf-8"))
a2 = json.loads((ROOT / "data/audit2.json").read_text(encoding="utf-8"))
merged = {k: v for k, v in a1.items() if not k.startswith("_")}
for k, v in a2.items():
    if not k.startswith("_"):
        merged[k] = v | {"round": 2}
for k in args.manual:
    merged[k] = merged[k] | {"status": "OK_MANUAL", "scoring": "manual"}
excluded = {k: f"audit round 2: {v['status']}: {v['why']}" for k, v in merged.items()
            if v["status"] not in ("OK", "OK_MANUAL")}
for k in args.g2_fail:
    excluded.setdefault(k, "G2: char 3-gram Jaccard > 0.3 against train after one rewrite")
final = {k: v for k, v in merged.items() if k not in excluded}
final["_summary"] = {"rows": len(final), "excluded": len(excluded)}
(ROOT / "data/audit.json").write_text(json.dumps(final, ensure_ascii=False, indent=1), encoding="utf-8")
(ROOT / "data/excluded.json").write_text(json.dumps(excluded, ensure_ascii=False, indent=1), encoding="utf-8")
print(f"audit rows {len(final) - 1}, excluded {len(excluded)}: {sorted(excluded)}")
