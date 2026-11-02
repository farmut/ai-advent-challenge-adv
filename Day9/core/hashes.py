"""Hashes of everything a Day9 result depends on (results/prereg.json and its check). New file (not Day8's)."""

from __future__ import annotations

from pathlib import Path

from core.io import ROOT, read_json, sha256_file, sha256_text

HASHED_FILES = (
    "data/format/eval.jsonl", "data/format/valid.jsonl", "data/format/train.jsonl", "data/ood/eval.jsonl",
    "data/dissimilar/eval.jsonl", "data/distill/train.jsonl", "data/distill/valid.jsonl",
    "data/distill/audit.json", "common.py", "principles.json", "prompts.py", "core/text.py",
    "gen/ood_checkers.py", "eval/generate.py", "eval/registry.py", "eval/scorers/format.py",
    "eval/scorers/ood.py", "eval/scorers/dissimilar.py", "eval/scorers/contamination.py", "eval/scorers/chrf.py",
    "teacher/parse.py", "distill.py", "gen/GEN_SITUATIONS.md", "gen/gen_situations.py", "gen/make_cells.py",
    "data/distill/cells.jsonl", "configs/teacher.yaml", "configs/smoke.yaml", "configs/student.yaml",
    "configs/fallback_F1.yaml", "configs/fallback_F2.yaml",
)


def current_hashes(root: Path = ROOT) -> dict[str, str]:
    """sha256 of the hashed files that exist, plus 'prompt:<NAME>' for every system prompt."""
    from prompts import NAMES

    out = {rel: sha256_file(root / rel) for rel in HASHED_FILES if (root / rel).exists()}
    out |= {f"prompt:{k}": sha256_text(v) for k, v in NAMES.items()}
    return out


def expected_hashes(prereg: dict) -> dict[str, str]:
    want: dict[str, str] = {}
    for stage in prereg.get("stages", {}).values():
        want |= stage.get("hashes", {})
    for am in prereg.get("amendments", []):
        want |= am.get("rehash", {})
    return want


def check_prereg(root: Path = ROOT, keys: set[str] | None = None) -> list[str]:
    """Mismatches between the hashes of every prereg stage and the current files; [] if no prereg yet.

    A later stage (v2) overrides the hashes of an earlier one (v1) for the same key: v2 freezes the train data
    that did not exist at v1. A dated "rehash" amendment (prereg.py rehash --reason ...) overrides both, in order;
    stages are applied first, then amendments, so the most recent explicit decision wins.
    """
    path = root / "results/prereg.json"
    if not path.exists():
        return []
    want = expected_hashes(read_json(path))
    have = current_hashes(root)
    return sorted(k for k, v in want.items() if (keys is None or k in keys) and have.get(k) != v)
