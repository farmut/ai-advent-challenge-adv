"""New situations by the teacher (plan §5.2) -> data/distill/situations_raw.jsonl.

usage: python -m gen.gen_situations [--cells PATH] [--append] [--limit-calls N] [--dry-run]
Inputs are only principles.json, book/pages.jsonl and the selected cells file (tests/test_isolation.py):
the generator never sees eval or valid. Prompt templates live in gen/GEN_SITUATIONS.md.
One call = one principle, up to 5 cell specs; step C1 of the budget. --append preserves prior output rows.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import re
from collections import defaultdict
from functools import cache
from pathlib import Path
from typing import Any

from common import PRINCIPLES
from core.io import ROOT, read_jsonl, write_jsonl

SPEC_MD = ROOT / "gen/GEN_SITUATIONS.md"
BOOK = ROOT / "book/pages.jsonl"
CELLS = ROOT / "data/distill/cells.jsonl"
OUT = ROOT / "data/distill/situations_raw.jsonl"
REJECTED = ROOT / "data/distill/situations_rejected.jsonl"

EXCERPT_CHARS = 9000
WINDOWS = 9                      # calls per principle: ceil(42 / 5)
TEMPERATURE = 0.9
MAX_TOKENS = 1600
SEED = 42
MIN_CHARS, MAX_CHARS = 40, 900
_VERDICT_WORD = re.compile(r"(?<![\w-])(?:нарушени\w*|наруша\w*|риск\w{0,2}|соответствует|соответствуют)(?![\w-])",
                           re.I)


def _section(text: str, name: str) -> str:
    m = re.search(rf"<!-- {name} -->\n(.*?)\n<!-- /{name} -->", text, re.S)
    if not m:
        raise ValueError(f"section {name} not found in {SPEC_MD.name}")
    return m.group(1).strip()


@cache
def templates(path: Path = SPEC_MD) -> tuple[str, str]:
    text = path.read_text(encoding="utf-8")
    return _section(text, "SYSTEM"), _section(text, "USER")


@cache
def book_pages(path: Path = BOOK) -> dict[int, str]:
    return {r["pdf_page"]: r["text"] for r in read_jsonl(path)}


def excerpt(principle: dict[str, Any], window: int, max_chars: int = EXCERPT_CHARS,
            pages: dict[int, str] | None = None) -> str:
    """Pages of the principle starting at a rotating offset; whole pages until max_chars."""
    pages = pages if pages is not None else book_pages()
    lo, hi = principle["pages"]
    nums = [n for n in range(lo, hi + 1) if pages.get(n, "").strip()]
    if not nums:
        return ""
    start = (window * max(1, len(nums) // WINDOWS)) % len(nums)   # spread the calls of one principle
    out: list[str] = []
    size = 0
    for n in nums[start:] + nums[:start]:
        t = " ".join(pages[n].split())
        if size + len(t) > max_chars and out:
            break
        out.append(f"[стр. {n}] {t[:max_chars]}")
        size += len(t)
    return "\n".join(out)


def spec_line(cell: dict[str, Any]) -> str:
    return (f'- spec_id {cell["cell_id"]}: вердикт «{cell["verdict"]}»; автор — {cell["role"]}; '
            f'форма — {cell["form"]}; область — {cell["domain"]}')


def build_messages(cells: list[dict[str, Any]], window: int,
                   pages: dict[int, str] | None = None) -> list[dict[str, str]]:
    """Messages of one generation call; all cells must share the principle."""
    pids = {c["principle"] for c in cells}
    if len(pids) != 1:
        raise ValueError("one call = one principle")
    pid = pids.pop()
    p = next(x for x in PRINCIPLES if x["id"] == pid)
    system, user = templates()
    user = user.format(principle_name=p["name"], gist=p["gist"], excerpt=excerpt(p, window, pages=pages),
                       specs="\n".join(spec_line(c) for c in cells), n=len(cells))
    return [{"role": "system", "content": system}, {"role": "user", "content": user}]


def label_leak(situation: str, principle_name: str) -> tuple[bool, bool]:
    """(names a verdict word, names the principle)."""
    low = situation.lower().replace("ё", "е")
    return bool(_VERDICT_WORD.search(low)), principle_name.lower().replace("ё", "е") in low


def parse_situations(text: str, cells: list[dict[str, Any]]) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    """(accepted rows, rejected rows with reason) for one call's answer."""
    by_id = {c["cell_id"]: c for c in cells}
    name = {p["id"]: p["name"] for p in PRINCIPLES}
    got: dict[str, str] = {}
    for line in text.splitlines():
        line = line.strip().strip(",")
        if not line.startswith("{"):
            continue
        try:
            obj = json.loads(line)
        except ValueError:
            continue
        sid, sit = obj.get("spec_id"), obj.get("situation")
        if sid in by_id and isinstance(sit, str) and sid not in got:
            got[sid] = " ".join(sit.split())
    ok: list[dict[str, Any]] = []
    bad: list[dict[str, Any]] = []
    named = 0
    for sid, cell in by_id.items():
        sit = got.get(sid)
        reason = None
        if sit is None:
            reason = "missing"
        elif not MIN_CHARS <= len(sit) <= MAX_CHARS:
            reason = "length"
        else:
            verdict_word, principle_named = label_leak(sit, name[cell["principle"]])
            if verdict_word:
                reason = "verdict_word"
            elif principle_named:
                named += 1
                if named > max(1, len(cells) // 5):
                    reason = "principle_named"
        row = {**cell, "situation": sit}
        (bad if reason else ok).append({**row, "reason": reason} if reason else row)
    return ok, bad


def calls(cells: list[dict[str, Any]]) -> list[list[dict[str, Any]]]:
    by_call: dict[str, list[dict[str, Any]]] = defaultdict(list)
    for c in cells:
        by_call[c["call_id"]].append(c)
    return [by_call[k] for k in sorted(by_call)]


def merge_rows(existing: list[dict[str, Any]], new: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Append a wave without replacing prior rows or silently merging duplicate IDs."""
    out = existing + new
    ids = [r["cell_id"] for r in out]
    if len(ids) != len(set(ids)):
        raise ValueError("duplicate cell_id while appending generated situations")
    return out


async def generate_all(client: Any, groups: list[list[dict[str, Any]]]) -> tuple[list[dict], list[dict]]:
    from teacher.client import TeacherRequest

    reqs = []
    for i, g in enumerate(groups):
        window = int(g[0]["call_id"].rsplit("-", 1)[1])
        reqs.append(TeacherRequest(messages=tuple(build_messages(g, window)), step="C1", temperature=TEMPERATURE,
                                   max_tokens=MAX_TOKENS, reasoning="off", seed=SEED + i, tag=g[0]["call_id"]))
    async with client:
        results = await client.complete_many(reqs)
    ok_all: list[dict] = []
    bad_all: list[dict] = []
    for g, r in zip(groups, results):
        if isinstance(r, BaseException):
            bad_all += [{**c, "situation": None, "reason": f"api: {type(r).__name__}"} for c in g]
            continue
        ok, bad = parse_situations(r.text, g)
        ok_all += ok
        bad_all += bad
    return ok_all, bad_all


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--cells", type=Path, default=CELLS)
    ap.add_argument("--append", action="store_true", help="append to raw/rejected outputs, preserving prior rows")
    ap.add_argument("--limit-calls", type=int, default=None)
    ap.add_argument("--dry-run", action="store_true", help="print the first call's messages, no network")
    args = ap.parse_args(argv)
    if args.cells.exists():
        cells = read_jsonl(args.cells)
    elif args.dry_run and args.cells == CELLS:
        from gen.make_cells import make_cells

        cells = make_cells()
    else:
        raise SystemExit(f"{args.cells} not found: run python -m gen.make_cells first")
    groups = calls(cells)[:args.limit_calls]
    if args.dry_run:
        msgs = build_messages(groups[0], 0)
        print(json.dumps({"calls": len(groups), "first_call_chars": sum(len(m["content"]) for m in msgs)},
                         ensure_ascii=False))
        return 0
    from teacher.factory import make_client

    ok, bad = asyncio.run(generate_all(make_client(), groups))
    if args.append:
        ok = merge_rows(read_jsonl(OUT) if OUT.exists() else [], ok)
        bad = merge_rows(read_jsonl(REJECTED) if REJECTED.exists() else [], bad)
    write_jsonl(OUT, ok)
    write_jsonl(REJECTED, bad)
    print({"accepted": len(ok), "rejected": len(bad)})
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
