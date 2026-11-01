"""Validate all Day8 data against gen/SCHEMA.md and build the derived files.

usage: python build_data.py [--check-only]
Builds:
  data/facts/eval.jsonl        eval_gen.jsonl + strata a (seed 42: one of the 5 train questions per train fact)
  data/mix/train.jsonl         format 249 (as is) + facts 150 (system TAG_FACTS) + style 101 (TAG_STYLE), shuffled
  data/mix/valid_{format,facts,style}.jsonl and valid.jsonl (their union)
Fails (exit 1) on any schema or size mismatch.
"""

from __future__ import annotations

import argparse
import random
import re
import sys
from collections import Counter
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from common import SYSTEM_PROMPTED
from core.io import ROOT, read_json, read_jsonl, write_jsonl
from core.prompts import TAG_STYLE, tag_facts_for

SEED = 42
Row = dict[str, Any]


@dataclass(frozen=True)
class Sizes:
    facts_train: int = 30
    facts_control: int = 10
    templates: tuple[str, ...] = ("T1", "T2", "T3", "T4", "T5")
    facts_eval: dict[str, int] = field(default_factory=lambda: {"b": 60, "c": 30, "d": 10, "control": 10})
    facts_eval_b_lang: dict[str, int] = field(default_factory=lambda: {"ru": 40, "en": 20})
    min_related_pairs: int = 15
    style: tuple[int, int, int] = (101, 20, 40)
    ood: dict[str, int] = field(default_factory=lambda: {"arith": 30, "ifeval": 25, "knowledge": 25,
                                                           "translate": 10, "nonexistent": 10})
    dissimilar_groups: int = 4
    dissimilar_per_group: int = 8
    format: tuple[int, int, int] = (249, 30, 57)
    batch: int = 4


class DataError(Exception):
    """Raised with the list of all schema problems found."""


def _need(rows: list[Row], fields: set[str], name: str, errs: list[str]) -> None:
    for r in rows:
        if miss := fields - r.keys():
            errs.append(f"{name}: {r.get('id', '?')} lacks {sorted(miss)}")


def _count(name: str, got: int, want: int, errs: list[str]) -> None:
    if got != want:
        errs.append(f"{name}: {got} rows, expected {want}")


def load_all(root: Path) -> dict[str, Any]:
    paths = {
        "regulation": "facts/regulation.json", "company": "facts/company.json",
        "facts_train": "data/facts/train.jsonl", "facts_valid": "data/facts/valid.jsonl",
        "facts_eval_gen": "data/facts/eval_gen.jsonl", "style_train": "data/style/train.jsonl",
        "style_valid": "data/style/valid.jsonl", "style_eval": "data/style/eval.jsonl",
        "ood": "data/ood/eval.jsonl", "dissimilar": "data/dissimilar/eval.jsonl",
        "format_train": "data/format/train.jsonl", "format_valid": "data/format/valid.jsonl",
        "format_eval": "data/format/eval.jsonl",
    }
    missing = [p for p in [*paths.values(), "facts/regulation.md"] if not (root / p).exists()]
    if missing:
        raise DataError([f"missing file: {p}" for p in missing])
    return {k: read_json(root / p) if p.endswith(".json") else read_jsonl(root / p) for k, p in paths.items()}


def validate_regulation(reg: list[Row], company: Row, s: Sizes, errs: list[str]) -> None:
    _need(reg, {"fact_id", "split", "topic", "kind", "statement", "answer", "answer_keys", "answer_keys_en",
                "related"}, "regulation", errs)
    _count("regulation", len(reg), s.facts_train + s.facts_control, errs)
    splits = Counter(f.get("split") for f in reg)
    if splits != Counter({"train": s.facts_train, "control": s.facts_control}):
        errs.append(f"regulation: splits {dict(splits)}")
    ids = [f.get("fact_id") for f in reg]
    if len(set(ids)) != len(ids) or not all(re.fullmatch(r"F\d\d", str(i)) for i in ids):
        errs.append("regulation: fact_id must be unique Fxx")
    for f in reg:
        if f.get("kind") not in ("number", "name", "time", "term", "rule"):
            errs.append(f"regulation: {f.get('fact_id')} bad kind {f.get('kind')}")
        if not f.get("answer_keys") or not f.get("answer_keys_en"):
            errs.append(f"regulation: {f.get('fact_id')} empty answer keys")
        if bad := set(f.get("related", [])) - set(ids):
            errs.append(f"regulation: {f.get('fact_id')} related to unknown {sorted(bad)}")
    pairs = {frozenset((f["fact_id"], r)) for f in reg for r in f.get("related", []) if r != f.get("fact_id")}
    if len(pairs) < s.min_related_pairs:
        errs.append(f"regulation: {len(pairs)} related pairs < {s.min_related_pairs}")
    if not company.get("name"):
        errs.append("company.json: no name")


def validate_facts(d: dict[str, Any], s: Sizes, errs: list[str]) -> None:
    split = {f["fact_id"]: f["split"] for f in d["regulation"]}
    train_facts = sorted(k for k, v in split.items() if v == "train")
    fields = {"id", "task", "fact_id", "strata", "template_id", "lang", "question", "answer", "answer_keys"}
    for name, strata, templates in (("facts_train", "train", s.templates), ("facts_valid", "valid", ("V1",))):
        rows = d[name]
        _need(rows, fields, name, errs)
        _count(name, len(rows), len(train_facts) * len(templates), errs)
        got = Counter((r.get("fact_id"), r.get("template_id")) for r in rows)
        want = Counter((f, t) for f in train_facts for t in templates)
        if got != want:
            errs.append(f"{name}: (fact, template) grid differs from {len(train_facts)} train facts x {templates}")
        for r in rows:
            if r.get("strata") != strata or not r.get("answer_keys"):
                errs.append(f"{name}: {r.get('id')} bad strata or empty keys")
    ev = d["facts_eval_gen"]
    _need(ev, {"id", "task", "fact_ids", "strata", "template_id", "lang", "question", "answer_keys",
               "require_all"}, "facts_eval_gen", errs)
    if (c := dict(Counter(r.get("strata") for r in ev))) != s.facts_eval:
        errs.append(f"facts_eval_gen: strata {c}, expected {s.facts_eval}")
    if (c := dict(Counter(r.get("lang") for r in ev if r.get("strata") == "b"))) != s.facts_eval_b_lang:
        errs.append(f"facts_eval_gen: strata b langs {c}, expected {s.facts_eval_b_lang}")
    for r in ev:
        want = "control" if r.get("strata") == "control" else "train"
        fids = r.get("fact_ids") or []
        if not fids or any(split.get(f) != want for f in fids):
            errs.append(f"facts_eval_gen: {r.get('id')} fact_ids {fids} must be {want} facts")
        if not r.get("answer_keys"):
            errs.append(f"facts_eval_gen: {r.get('id')} empty answer_keys")
    if used := {r.get("template_id") for r in ev} & {*s.templates, "V1"}:
        errs.append(f"facts_eval_gen: template ids overlap train/valid: {sorted(used)}")


def validate_style(d: dict[str, Any], s: Sizes, errs: list[str]) -> None:
    from gen.style_proxies import check, explain

    for name, n in zip(("style_train", "style_valid", "style_eval"), s.style):
        rows = d[name]
        _count(name, len(rows), n, errs)
        need = {"id", "task", "topic_id", "question"} | ({"answer"} if name != "style_eval" else set())
        _need(rows, need, name, errs)
        if name != "style_eval":
            errs += [f"{name}: {r['id']} fails style proxies: {explain(r['answer'])}"
                     for r in rows if "answer" in r and not check(r["answer"])["all"]]
    seen = {r.get("topic_id") for r in d["style_train"] + d["style_valid"]}
    if overlap := seen & {r.get("topic_id") for r in d["style_eval"]}:
        errs.append(f"style_eval: topic_ids overlap train/valid: {sorted(overlap)}")


def _checker_types(checker: Any) -> list[Any]:
    return [c.get("type") for c in (checker if isinstance(checker, list) else [checker])]


def validate_ood(d: dict[str, Any], s: Sizes, errs: list[str]) -> None:
    from gen.ood_checkers import KNOWN_TYPES

    ood = d["ood"]
    _need(ood, {"id", "task", "category", "prompt", "gold", "answer_keys", "checker"}, "ood", errs)
    if (c := dict(Counter(r.get("category") for r in ood))) != s.ood:
        errs.append(f"ood: categories {c}, expected {s.ood}")
    for r in ood:
        types = _checker_types(r.get("checker", {}))
        auto = [t for t in types if t != "none"]
        if not set(types) <= KNOWN_TYPES or (r.get("category") == "ifeval") != bool(auto):
            errs.append(f"ood: {r.get('id')} checker {types} does not fit category {r.get('category')}")
        if r.get("category") == "arith":
            try:
                float(r["gold"])
            except (KeyError, ValueError):
                errs.append(f"ood: {r.get('id')} arith gold is not a number")
        if r.get("category") == "knowledge" and not r.get("answer_keys"):
            errs.append(f"ood: {r.get('id')} knowledge without answer_keys")
        if r.get("category") == "translate" and not r.get("gold"):
            errs.append(f"ood: {r.get('id')} translate without gold")
    ds = d["dissimilar"]
    _need(ds, {"id", "task", "group", "lang", "prompt", "checker", "note"}, "dissimilar", errs)
    want = {g: s.dissimilar_per_group for g in range(1, s.dissimilar_groups + 1)}
    if (c := dict(Counter(r.get("group") for r in ds))) != want:
        errs.append(f"dissimilar: groups {c}, expected {want}")
    errs += [f"dissimilar: {r.get('id')} unknown checker" for r in ds
             if not set(_checker_types(r.get("checker", {}))) <= KNOWN_TYPES]


def validate_format(d: dict[str, Any], s: Sizes, errs: list[str]) -> None:
    for name, n in zip(("format_train", "format_valid", "format_eval"), s.format):
        _count(name, len(d[name]), n, errs)
    for name in ("format_train", "format_valid"):
        for i, r in enumerate(d[name]):
            roles = [m.get("role") for m in r.get("messages", [])]
            if roles != ["system", "user", "assistant"] or r["messages"][0]["content"] != SYSTEM_PROMPTED:
                errs.append(f"{name}: row {i} is not [SYSTEM_PROMPTED, user, assistant]")
    _need(d["format_eval"], {"id", "principle", "verdict", "situation"}, "format_eval", errs)


def facts_strata_a(train: list[Row], seed: int = SEED) -> list[Row]:
    """One verbatim train question per train fact, chosen with a fixed seed; ids FE-a-Fxx."""
    rng = random.Random(seed)
    by_fact: dict[str, list[Row]] = {}
    for r in sorted(train, key=lambda r: r["id"]):
        by_fact.setdefault(r["fact_id"], []).append(r)
    out = []
    for fid in sorted(by_fact):
        r = rng.choice(by_fact[fid])
        out.append({"id": f"FE-a-{fid}", "task": "facts", "fact_ids": [fid], "strata": "a",
                    "template_id": r["template_id"], "lang": r["lang"], "question": r["question"],
                    "answer_keys": r["answer_keys"], "require_all": True, "source_id": r["id"]})
    return out


def _msgs(system: str, user: str, answer: str) -> list[dict[str, str]]:
    return [{"role": "system", "content": system}, {"role": "user", "content": user},
            {"role": "assistant", "content": answer}]


def mix_rows(d: dict[str, Any], tag_facts: str) -> dict[str, list[Row]]:
    """Training mix and per-task valid files (messages plus metadata; mlx_lm reads only `messages`)."""
    def fmt(rows: list[Row], prefix: str) -> list[Row]:
        return [{"id": f"{prefix}-{i:03d}", "task": "format", "messages": r["messages"]}
                for i, r in enumerate(rows, 1)]

    def facts(rows: list[Row]) -> list[Row]:
        return [{"id": r["id"], "task": "facts", "fact_id": r["fact_id"], "strata": r["strata"], "lang": r["lang"],
                 "answer_keys": r["answer_keys"], "messages": _msgs(tag_facts, r["question"], r["answer"])}
                for r in rows]

    def style(rows: list[Row]) -> list[Row]:
        return [{"id": r["id"], "task": "style", "topic_id": r["topic_id"],
                 "messages": _msgs(TAG_STYLE, r["question"], r["answer"])} for r in rows]

    train = fmt(d["format_train"], "FMT-T") + facts(d["facts_train"]) + style(d["style_train"])
    random.Random(SEED).shuffle(train)
    vf, vfa, vs = fmt(d["format_valid"], "FMT-V"), facts(d["facts_valid"]), style(d["style_valid"])
    return {"train": train, "valid_format": vf, "valid_facts": vfa, "valid_style": vs, "valid": vf + vfa + vs}


def build(root: Path = ROOT, sizes: Sizes = Sizes(), write: bool = True) -> dict[str, int]:
    """Validate everything, then write derived files. Returns row counts; raises DataError on problems."""
    d = load_all(root)
    errs: list[str] = []
    validate_regulation(d["regulation"], d["company"], sizes, errs)
    validate_facts(d, sizes, errs)
    validate_style(d, sizes, errs)
    validate_ood(d, sizes, errs)
    validate_format(d, sizes, errs)
    if errs:
        raise DataError(errs)
    facts_eval = d["facts_eval_gen"] + facts_strata_a(d["facts_train"])
    mix = mix_rows(d, tag_facts_for(root))
    ids = [r["id"] for k in ("facts_train", "facts_valid", "style_train", "style_valid", "style_eval", "ood",
                             "dissimilar", "format_eval") for r in d[k]]
    ids += [r["id"] for r in facts_eval] + [r["id"] for r in mix["valid"] if r["task"] == "format"]
    ids += [r["id"] for r in mix["train"] if r["task"] == "format"]
    if dup := [i for i, c in Counter(ids).items() if c > 1]:
        errs.append(f"duplicate ids: {dup[:10]}")
    want_train = sizes.format[0] + sizes.facts_train * len(sizes.templates) + sizes.style[0]
    _count("mix/train", len(mix["train"]), want_train, errs)
    if len(mix["train"]) % sizes.batch:
        errs.append(f"mix/train: {len(mix['train'])} rows is not a multiple of batch {sizes.batch}")
    if errs:
        raise DataError(errs)
    if write:
        write_jsonl(root / "data/facts/eval.jsonl", facts_eval)
        for name, rows in mix.items():
            write_jsonl(root / f"data/mix/{name}.jsonl", rows)
    return {"facts_eval": len(facts_eval)} | {f"mix/{k}": len(v) for k, v in mix.items()}


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--check-only", action="store_true")
    args = ap.parse_args()
    try:
        counts = build(write=not args.check_only)
    except DataError as e:
        print("\n".join(e.args[0]), file=sys.stderr)
        return 1
    print(counts)
    return 0


if __name__ == "__main__":
    sys.exit(main())
