"""Teacher labelling, quality filter, split and the mlx_lm dataset (plan §5.4–§5.6).

usage (from Day9/):
  python distill.py label  [--only gold|new] [--limit N]    teacher answers with SYSTEM_PROMPTED (steps C2, C3)
  python distill.py build  [--prompt list|gist]             filter + split -> data/distill[_gist]/{train,valid}.jsonl
Inputs: data/format/{train,valid}.jsonl (the 249 + 30 rows with gold), data/distill/situations_raw.jsonl
(new situations with the intended label), data/distill/leaks_drop.json (train ids removed by G3).
This module never reads the eval set (tests/test_isolation.py); eval ids (EV-*) are rejected outright.

Labelling rules (prereg v1):
  gold rows:  sample 0 greedy (T=0), samples 1..3 at T=0.7 (seed 42+i); take the first sample that agrees with
              gold on (verdict, principle) and passes the filter; otherwise the row goes to rejected.jsonl.
  new rows:   greedy (T=0) must agree with the intended label and pass the filter, else reject (no more calls);
              then 2 samples at T=0.7 must both agree (3/3); the greedy answer goes to train.
Filter: format_ok; exactly 4 lines; no markdown; no CJK; latin share < 5%; 150–700 chars; Обоснование and
Рекомендация 1–2 sentences each; duplicate justifications dropped; fields normalised with common.clean_text.
Principle-line normalisation (amendment 2026-11-01, block C; train/valid data only, never in eval scoring):
a line "Принцип: <exact name from principles.json>" followed by ":" or " —" / " -" and an explanation becomes
"Принцип: <name>" before the labels are compared and the filter runs; the count of such answers is reported.
"""

from __future__ import annotations

import argparse
import asyncio
import random
import re
from collections import Counter, defaultdict
from collections.abc import Iterable, Sequence
from dataclasses import dataclass, replace
from pathlib import Path
from typing import Any

import prompts
from common import NAME_BY_ID, clean_text, parse_answer
from core.io import ROOT, read_json, read_jsonl, write_json, write_jsonl
from teacher.parse import Parsed, n_sentences, parse_teacher

DATA = ROOT / "data/distill"
GOLD_SOURCES = {"FT": "data/format/train.jsonl", "FV": "data/format/valid.jsonl"}
FORBIDDEN_ID_PREFIXES = ("EV-",)

MIN_CHARS, MAX_CHARS = 150, 700
MAX_LATIN = 0.05
SENTENCES = (1, 2)
GOLD_TEMPS = (0.0, 0.7, 0.7, 0.7)   # k = 4
NEW_EXTRA_TEMPS = (0.7, 0.7)        # after a matching greedy answer: 3/3
SEED = 42
N_VALID_NEW = 30
VALID_CANDIDATES = 45               # fixed random order, drawn before labelling
MAX_TOKENS = 400
FILTER_RULES = ("format", "lines", "markdown", "cjk", "latin", "length", "justification_sentences",
                "recommendation_sentences")


# ---------- source rows ----------

def gold_rows(root: Path = ROOT) -> list[dict[str, Any]]:
    """The 249 + 30 Day8 rows with gold labels; ids FT-### / FV-### by file order."""
    out = []
    for prefix, rel in GOLD_SOURCES.items():
        for i, r in enumerate(read_jsonl(root / rel), 1):
            p = parse_answer(r["messages"][2]["content"])
            out.append({"id": f"{prefix}-{i:03d}", "source": "orig", "split": "valid" if prefix == "FV" else "train",
                        "user": r["messages"][1]["content"], "gold_answer": r["messages"][2]["content"],
                        "gold_verdict": p["verdict"], "gold_principle": p["principle"]})
    return out


def new_rows(path: Path = DATA / "situations_raw.jsonl") -> list[dict[str, Any]]:
    return [{"id": r["cell_id"], "source": "new", "user": r["situation"], "gold_verdict": r["verdict"],
             "gold_principle": r["principle"], "cell": f"{r['principle']}|{r['verdict']}"}
            for r in read_jsonl(path)]


def select_new_cells(rows: list[dict[str, Any]], cells: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Restrict one labelling run to the specified generation cells."""
    ids = {c["cell_id"] for c in cells}
    return [r for r in rows if r["id"] in ids]


# ---------- principle-line normalisation (train/valid data only) ----------

_NAMES_LONGEST_FIRST = tuple(sorted(NAME_BY_ID.values(), key=len, reverse=True))
_PRINCIPLE_TAIL = re.compile(r"^(?::|\s—|\s-)\s*\S")


def normalize_principle_line(text: str) -> tuple[str, bool]:
    """'Принцип: <exact name>: <explanation>' (or ' — ' / ' - ') -> 'Принцип: <exact name>'; (text, changed).

    Only the first 'Принцип:' line is touched, the name must match principles.json exactly (case included),
    and a non-empty explanation must follow the separator. Anything else is returned unchanged.
    """
    lines = text.split("\n")
    for i, line in enumerate(lines):
        if not line.startswith("Принцип:"):
            continue
        rest = line[len("Принцип:"):].lstrip(" ")
        for name in _NAMES_LONGEST_FIRST:
            if rest.startswith(name) and _PRINCIPLE_TAIL.match(rest[len(name):]):
                lines[i] = f"Принцип: {name}"
                return "\n".join(lines), True
        return text, False
    return text, False


def parse_distill(raw: str) -> tuple[Parsed, bool]:
    """parse_teacher + principle-line normalisation; (parsed, normalized). The think/markdown flags of the raw
    answer are kept, so a markdown answer is still rejected by the filter."""
    p = parse_teacher(raw)
    text, changed = normalize_principle_line(p.text)
    if not changed:
        return p, False
    q = parse_teacher(text)
    return replace(q, had_think=p.had_think, had_markdown=p.had_markdown), True


# ---------- filter ----------

def filter_reasons(p: Parsed) -> list[str]:
    """Empty list = the answer may go to train."""
    bad = []
    if not p.format_ok:
        bad.append("format")
    if not p.exactly_4_lines:
        bad.append("lines")
    if p.had_markdown:
        bad.append("markdown")
    if p.has_cjk:
        bad.append("cjk")
    if p.latin_share >= MAX_LATIN:
        bad.append("latin")
    if not MIN_CHARS <= p.chars <= MAX_CHARS:
        bad.append("length")
    lo, hi = SENTENCES
    if not lo <= n_sentences(p.fields.get("обоснование", "")) <= hi:
        bad.append("justification_sentences")
    if not lo <= n_sentences(p.fields.get("рекомендация", "")) <= hi:
        bad.append("recommendation_sentences")
    return bad


def normalized_answer(p: Parsed) -> str:
    """Canonical 4 lines: exact verdict and principle names, cleaned sentences."""
    return "\n".join([f"Вердикт: {p.verdict}", f"Принцип: {NAME_BY_ID[p.principle]}",
                      f"Обоснование: {clean_text(p.fields['обоснование'])}",
                      f"Рекомендация: {clean_text(p.fields['рекомендация'])}"])


def agrees(p: Parsed, row: dict[str, Any]) -> bool:
    return p.verdict == row["gold_verdict"] and p.principle == row["gold_principle"]


# ---------- labelling decisions (pure) ----------

@dataclass
class Decision:
    accepted: bool
    sample: int | None          # index of the answer used for train
    reason: str | None
    calls: int


def decide_gold(samples: Sequence[Parsed], row: dict[str, Any]) -> Decision:
    """First sample that agrees with gold and passes the filter; samples are tried in order (k <= 4)."""
    for i, p in enumerate(samples):
        if agrees(p, row) and not filter_reasons(p):
            return Decision(True, i, None, i + 1)
    reason = "disagree" if not any(agrees(p, row) for p in samples) else "filter"
    return Decision(False, None, reason, len(samples))


def need_extra(greedy: Parsed, row: dict[str, Any]) -> bool:
    """New rows: extra samples are paid only when the greedy answer agrees and is clean."""
    return agrees(greedy, row) and not filter_reasons(greedy)


def decide_new(greedy: Parsed, extra: Sequence[Parsed], row: dict[str, Any]) -> Decision:
    if not agrees(greedy, row):
        return Decision(False, None, "greedy_disagree", 1)
    if reasons := filter_reasons(greedy):
        return Decision(False, None, "filter:" + ",".join(reasons), 1)
    if len(extra) < len(NEW_EXTRA_TEMPS):
        return Decision(False, None, "incomplete", 1 + len(extra))
    if not all(agrees(p, row) for p in extra):
        return Decision(False, None, "self_consistency", 1 + len(extra))
    return Decision(True, 0, None, 1 + len(extra))


# ---------- dedup, split, balance (pure) ----------

def _norm_text(s: str) -> str:
    return " ".join(re.sub(r"[^\w\s]", " ", s.lower().replace("ё", "е")).split())


def dedup_justifications(rows: Iterable[dict[str, Any]]) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    """Keep the first row (in id order) per normalized Обоснование; later duplicates are rejected."""
    seen: set[str] = set()
    kept, dup = [], []
    for r in sorted(rows, key=lambda r: r["id"]):
        key = _norm_text(parse_teacher(r["answer"]).fields.get("обоснование", ""))
        protected = r["source"] == "orig" and r.get("split") == "valid"   # valid keeps all 30 gold rows
        (dup if key in seen and not protected else kept).append(r)
        seen.add(key)
    return kept, dup


def forbidden(row_id: str, extra: set[str] = frozenset()) -> bool:  # type: ignore[assignment]
    return row_id.startswith(FORBIDDEN_ID_PREFIXES) or row_id in extra


def valid_candidates(new_ids: Sequence[str], n: int = VALID_CANDIDATES, seed: int = SEED) -> list[str]:
    """Fixed random order of new-row ids for the 30 new valid rows; drawn before labelling."""
    ids = sorted(new_ids)
    return random.Random(seed).sample(ids, min(n, len(ids)))


def split(accepted: list[dict[str, Any]], candidates: Sequence[str], n_valid_new: int = N_VALID_NEW
          ) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    """train / valid. valid = all orig valid rows + the first n accepted new rows in candidate order."""
    by_id = {r["id"]: r for r in accepted}
    chosen = [i for i in candidates if i in by_id and by_id[i]["source"] == "new"][:n_valid_new]
    valid_ids = {r["id"] for r in accepted if r.get("split") == "valid"} | set(chosen)
    valid = [r for r in accepted if r["id"] in valid_ids]
    train = [r for r in accepted if r["id"] not in valid_ids]
    return train, valid


def balance_to_multiple(train: list[dict[str, Any]], k: int = 4) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    """Drop new rows (never orig) from the largest (principle, verdict) cells until len(train) % k == 0."""
    rows = sorted(train, key=lambda r: r["id"])
    dropped: list[dict[str, Any]] = []
    while len(rows) % k:
        cnt = Counter((r["gold_principle"], r["gold_verdict"]) for r in rows)
        news = [r for r in rows if r["source"] == "new"]
        if not news:
            break
        victim = max(news, key=lambda r: (cnt[(r["gold_principle"], r["gold_verdict"])], r["id"]))
        rows.remove(victim)
        dropped.append(victim)
    return rows, dropped


def to_example(r: dict[str, Any], system: str) -> dict[str, Any]:
    return {"id": r["id"], "task": "format",
            "messages": [{"role": "system", "content": system}, {"role": "user", "content": r["user"]},
                         {"role": "assistant", "content": r["answer"]}],
            "meta": {"source": r["source"], "gold_verdict": r["gold_verdict"],
                     "gold_principle": r["gold_principle"], "teacher": r.get("teacher", True)}}


def cell_stats(rows: Iterable[dict[str, Any]], planned: dict[str, int]) -> dict[str, Any]:
    """Kept vs planned per (principle|verdict); cells below 60% of the plan may get one extra generation wave."""
    got = Counter(f"{r['gold_principle']}|{r['gold_verdict']}" for r in rows if r["source"] == "new")
    out = {c: {"planned": n, "kept": got.get(c, 0), "share": round(got.get(c, 0) / n, 3) if n else None}
           for c, n in sorted(planned.items())}
    return {"cells": out, "below_60": sorted(c for c, v in out.items() if v["share"] is not None and v["share"] < 0.6)}


def formatonly_rows(labeled: Iterable[dict[str, Any]], system: str) -> list[dict[str, Any]]:
    """Reference set (not trained on): the first teacher sample that passes the format filter, labels ignored."""
    out = []
    for r in labeled:
        if forbidden(r["id"]) or r.get("split") == "valid":
            continue
        for text in r.get("samples") or []:
            pt, _ = parse_distill(text)
            if not filter_reasons(pt):
                out.append(to_example({**r, "answer": normalized_answer(pt)}, system))
                break
    return out


# ---------- labelling with the teacher (network; steps C2 / C3) ----------

def label_request(row: dict[str, Any], sample_idx: int, temperature: float, step: str) -> Any:
    from teacher.client import TeacherRequest

    return TeacherRequest.chat(prompts.SYSTEM_PROMPTED, row["user"], step=step, temperature=temperature,
                               max_tokens=MAX_TOKENS, reasoning=reasoning_mode(), seed=SEED + sample_idx,
                               sample_idx=sample_idx, tag=row["id"])


def reasoning_mode() -> str:
    """Frozen after G1 in results/prereg.json (teacher.reasoning); 'off' until then."""
    path = ROOT / "results/prereg.json"
    if path.exists():
        return read_json(path).get("frozen", {}).get("teacher_reasoning", "off")
    return "off"


def _failed(row: dict[str, Any], e: BaseException) -> dict[str, Any]:
    from teacher.budget import BudgetExceeded

    reason = "budget" if isinstance(e, BudgetExceeded) else "api_error"
    return {**row, "decision": Decision(False, None, reason, 0).__dict__, "samples": [], "answer": None,
            "error": f"{type(e).__name__}: {e}"}


async def _guarded(coro: Any, row: dict[str, Any]) -> dict[str, Any]:
    from teacher.budget import BudgetExceeded
    from teacher.client import TeacherError

    try:
        return await coro
    except (TeacherError, BudgetExceeded) as e:
        return _failed(row, e)


async def label_gold_rows(client: Any, rows: list[dict[str, Any]]) -> list[dict[str, Any]]:
    async def one(row: dict[str, Any]) -> dict[str, Any]:
        samples: list[Parsed] = []
        texts: list[str] = []
        norm: list[int] = []
        for i, t in enumerate(GOLD_TEMPS):
            res = await client.complete(label_request(row, i, t, "C2"))
            p, changed = parse_distill(res.text)
            samples.append(p)
            texts.append(res.text)
            norm += [i] if changed else []
            d = decide_gold(samples, row)
            if d.accepted:
                break
        return {**row, "decision": d.__dict__, "samples": texts, "normalized_samples": norm,
                "answer_normalized": d.accepted and d.sample in norm,
                "answer": normalized_answer(samples[d.sample]) if d.accepted else None}

    async with client:
        return list(await asyncio.gather(*(_guarded(one(r), r) for r in rows)))


async def label_new_rows(client: Any, rows: list[dict[str, Any]]) -> list[dict[str, Any]]:
    async def one(row: dict[str, Any]) -> dict[str, Any]:
        g = await client.complete(label_request(row, 0, 0.0, "C3"))
        greedy, changed = parse_distill(g.text)
        texts = [g.text]
        norm = [0] if changed else []
        extra: list[Parsed] = []
        if need_extra(greedy, row):
            for i, t in enumerate(NEW_EXTRA_TEMPS, 1):
                r = await client.complete(label_request(row, i, t, "C3"))
                texts.append(r.text)
                pe, ch = parse_distill(r.text)
                extra.append(pe)
                norm += [i] if ch else []
                if not agrees(extra[-1], row):
                    break
        d = decide_new(greedy, extra, row)
        return {**row, "decision": d.__dict__, "samples": texts, "normalized_samples": norm,
                "answer_normalized": d.accepted and changed,
                "answer": normalized_answer(greedy) if d.accepted else None}

    async with client:
        return list(await asyncio.gather(*(_guarded(one(r), r) for r in rows)))


# ---------- build ----------

def build(labeled: list[dict[str, Any]], candidates: Sequence[str], leak_drop: set[str], prompt_key: str = "list",
          planned: dict[str, int] | None = None) -> dict[str, Any]:
    """Pure assembly: filter decisions already in `labeled`; returns train/valid examples and statistics."""
    system = prompts.BY_KEY[prompt_key]
    rejected: list[dict[str, Any]] = []
    accepted: list[dict[str, Any]] = []
    for r in labeled:
        if forbidden(r["id"], leak_drop):
            rejected.append({**r, "reject": "forbidden_or_leak"})
        elif r.get("answer"):
            accepted.append(r)
        elif r["source"] == "orig" and r.get("split") == "valid":
            # valid needs all 30 gold rows: the reference answer stands in for a missing teacher answer
            accepted.append({**r, "answer": r["gold_answer"], "teacher": False})
        else:
            rejected.append({**r, "reject": (r.get("decision") or {}).get("reason", "unlabeled")})
    accepted, dups = dedup_justifications(accepted)
    rejected += [{**r, "reject": "dup_justification"} for r in dups]
    train, valid = split(accepted, candidates)
    train, extra = balance_to_multiple(train)
    rejected += [{**r, "reject": "balance"} for r in extra]
    valid_ids = {r["id"] for r in valid}
    return {"train": [to_example(r, system) for r in train], "valid": [to_example(r, system) for r in valid],
            "formatonly": [x for x in formatonly_rows(labeled, system) if x["id"] not in valid_ids | leak_drop],
            "rejected": rejected,
            "stats": {"train": len(train), "valid": len(valid), "rejected": dict(Counter(r["reject"] for r in rejected)),
                      "normalized_principle_line": normalization_stats(labeled, train, valid),
                      "train_by_verdict": dict(Counter(r["gold_verdict"] for r in train)),
                      "train_by_principle": dict(Counter(r["gold_principle"] for r in train)),
                      "cells": cell_stats(train + valid, planned or {})}}


def normalization_stats(labeled: Iterable[dict[str, Any]], train: Iterable[dict[str, Any]],
                        valid: Iterable[dict[str, Any]]) -> dict[str, int]:
    """How many teacher answers had their principle line normalised (all samples / used in train / in valid)."""
    labeled = list(labeled)
    return {"samples": sum(len(r.get("normalized_samples") or []) for r in labeled),
            "rows_any_sample": sum(bool(r.get("normalized_samples")) for r in labeled),
            "train": sum(bool(r.get("answer_normalized")) for r in train),
            "valid": sum(bool(r.get("answer_normalized")) for r in valid)}


def to_label(news: list[dict[str, Any]], drop: set[str]) -> list[dict[str, Any]]:
    """New rows that go to the teacher: rows removed by the lexical leak check G3a are never labelled."""
    return [r for r in news if r["id"] not in drop]


# ---------- G4: determinism of the teacher (step C4) ----------

G4_N = 20


def g4_rows(valid_examples: Sequence[dict[str, Any]], n: int = G4_N, seed: int = SEED) -> list[dict[str, Any]]:
    """n rows of the distillation valid set in a fixed seeded order (user = situation)."""
    rows = sorted(valid_examples, key=lambda r: r["id"])
    return random.Random(seed).sample(rows, min(n, len(rows)))


async def g4_runs(make: Any, rows: Sequence[dict[str, Any]]) -> list[list[tuple[str | None, str | None, str]]]:
    """Two cold greedy runs (separate clients, cache off) with the labelling prompt; (verdict, principle, text)."""
    out = []
    for _ in range(2):
        client = make()
        async with client:
            res = await client.complete_many([label_request({"id": r["id"], "user": r["messages"][1]["content"]},
                                                            0, 0.0, "C4") for r in rows])
        run = []
        for x in res:
            if isinstance(x, BaseException):
                run.append((None, None, f"error: {type(x).__name__}"))
            else:
                p, _ = parse_distill(x.text)
                run.append((p.verdict, p.principle, x.text))
        out.append(run)
    return out


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    lab = sub.add_parser("label")
    lab.add_argument("--only", choices=("gold", "new"))
    lab.add_argument("--new-cells", type=Path, help="label only new rows whose IDs occur in this cells JSONL")
    lab.add_argument("--limit", type=int)
    b = sub.add_parser("build")
    b.add_argument("--prompt", choices=("list", "gist"), default="list")
    sub.add_parser("g4", help="teacher determinism on 20 distillation-valid rows, two cold greedy runs")
    args = ap.parse_args(argv)
    drop_path = DATA / "leaks_drop.json"
    leak_drop = set(read_json(drop_path)) if drop_path.exists() else set()

    if args.cmd == "g4":
        import gates
        from teacher.factory import make_client

        rows = g4_rows(read_jsonl(DATA / "valid.jsonl"))
        a, b2 = asyncio.run(g4_runs(lambda: make_client(use_cache=False), rows))
        res = gates.gate_g4([x[:2] for x in a], [x[:2] for x in b2], [x[2] for x in a], [x[2] for x in b2])
        res["ids"] = [r["id"] for r in rows]
        res["disagree"] = [{"id": r["id"], "a": x[:2], "b": y[:2]} for r, x, y in zip(rows, a, b2) if x[:2] != y[:2]]
        gates.merge("G4", res)
        return 0 if res["pass"] else 5

    if args.cmd == "label":
        from teacher.factory import make_client

        news = to_label(new_rows(), leak_drop)
        if args.new_cells:
            news = select_new_cells(news, read_jsonl(args.new_cells))
        cand_path = DATA / "valid_candidates.json"
        if not cand_path.exists():  # drawn once, before any labelling call
            if args.new_cells:
                raise SystemExit("valid_candidates.json must be fixed before a supplemental labelling wave")
            write_json(cand_path, valid_candidates([r["id"] for r in news]))
        out: list[dict[str, Any]] = []
        if args.only in (None, "gold"):
            out += asyncio.run(label_gold_rows(make_client(), gold_rows()[:args.limit]))
        if args.only in (None, "new"):
            out += asyncio.run(label_new_rows(make_client(), news[:args.limit]))
        prev = {r["id"]: r for r in read_jsonl(DATA / "labeled.jsonl")} if (DATA / "labeled.jsonl").exists() else {}
        prev |= {r["id"]: r for r in out}
        write_jsonl(DATA / "labeled.jsonl", sorted(prev.values(), key=lambda r: r["id"]))
        print(Counter((r["source"], bool(r["answer"])) for r in out))
        return 0

    labeled = read_jsonl(DATA / "labeled.jsonl")
    planned: dict[str, int] = defaultdict(int)
    if (DATA / "cells.jsonl").exists():
        for c in read_jsonl(DATA / "cells.jsonl"):
            planned[f"{c['principle']}|{c['verdict']}"] += 1
    res = build(labeled, read_json(DATA / "valid_candidates.json"), leak_drop, args.prompt, planned)
    out_dir = ROOT / ("data/distill" if args.prompt == "list" else "data/distill_gist")
    write_jsonl(out_dir / "train.jsonl", res["train"])
    write_jsonl(out_dir / "valid.jsonl", res["valid"])
    write_jsonl(out_dir / "rejected.jsonl", res["rejected"])
    write_jsonl(out_dir / "train_formatonly.jsonl", res["formatonly"])
    write_json(out_dir / "build_stats.json", res["stats"])
    print(res["stats"])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
