"""summarize.py <run-out-dir>: one-screen summary of a harness run (meta, tools, checks)."""
import json
import os
import re
import sys

out = sys.argv[1]


def load(name, default=None):
    try:
        with open(os.path.join(out, name), encoding="utf-8") as f:
            return json.load(f)
    except (OSError, json.JSONDecodeError):
        return default


def text(name):
    try:
        with open(os.path.join(out, name), encoding="utf-8", errors="replace") as f:
            return f.read()
    except OSError:
        return ""


meta = load("meta.json", {}) or {}
print(f"run: {out}")
print(f"status={meta.get('status')} wall_s={round((meta.get('wall_ms') or 0) / 1000)} requests={meta.get('requests')} "
      f"tokens={meta.get('tokens')} model={meta.get('model')} ctx={meta.get('context_window')}")
print(f"params={json.dumps(meta.get('parameters'), ensure_ascii=False)}")
print(f"tools={json.dumps(meta.get('tool_calls'), ensure_ascii=False)}")
if meta.get("error"):
    print(f"error={meta['error']}")

changed = [l for l in text("changed.txt").splitlines() if l.strip()]
print(f"changed_files={len(changed)}")

checks = text("checks.log")
for line in checks.splitlines():
    m = re.match(r"^CRIT (\S+) (\S+) ?(.*)$", line)
    if m:
        print(f"  {m.group(1):<10} {m.group(2):<6} {m.group(3)[:150]}")

metrics = text("metrics.txt")
for pat in [r"^build: .*", r"^vet: .*", r"^test: .*", r"^total: .*", r".*stdlib-only.*", r"^go \d.*"]:
    for m in re.finditer(pat, metrics, re.M):
        print(f"  metrics {m.group(0)[:150]}")

bb = load("blackbox.json")
if bb:
    exp = [s.get("flags", {}).get("expect") for s in bb.get("scenarios", [])]
    ok = sum(1 for e in exp if e)
    total = sum(1 for e in exp if e is not None)
    print(f"  blackbox expected-behaviour {ok}/{total} mode={bb.get('mode')} leaks={sum((bb.get('leaks') or {}).values())} {bb.get('leaks')}")
elif os.path.exists(os.path.join(out, "blackbox.log")):
    print("  blackbox: no transcript, tail: " + " | ".join(text("blackbox.log").strip().splitlines()[-3:])[:300])
