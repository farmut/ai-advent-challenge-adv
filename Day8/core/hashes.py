"""Hashes of everything an eval result depends on (for results/prereg.json and its check)."""

from __future__ import annotations

from pathlib import Path

from core.io import ROOT, read_json, sha256_file, sha256_text

HASHED_FILES = (
    "data/format/eval.jsonl", "data/facts/eval.jsonl", "data/style/eval.jsonl", "data/ood/eval.jsonl",
    "data/dissimilar/eval.jsonl", "data/mix/train.jsonl", "data/mix/valid_format.jsonl",
    "data/mix/valid_facts.jsonl", "data/mix/valid_style.jsonl", "facts/regulation.json", "facts/regulation.md",
    "facts/company.json", "common.py", "core/prompts.py", "core/text.py", "gen/style_proxies.py",
    "gen/ood_checkers.py", "eval/generate.py", "eval/registry.py", "eval/scorers/format.py",
    "eval/scorers/facts.py", "eval/scorers/style.py", "eval/scorers/ood.py", "eval/scorers/dissimilar.py",
    "eval/scorers/contamination.py", "eval/scorers/chrf.py", "data/excluded.json", "data/audit.json",
)


def current_hashes(root: Path = ROOT) -> dict[str, str]:
    """sha256 of the hashed files that exist, plus 'prompt:<NAME>' for every system prompt."""
    from core.prompts import all_prompts

    out = {rel: sha256_file(root / rel) for rel in HASHED_FILES if (root / rel).exists()}
    out |= {f"prompt:{k}": sha256_text(v) for k, v in all_prompts(root).items()}
    return out


def check_prereg(root: Path = ROOT, keys: set[str] | None = None) -> list[str]:
    """Mismatches between results/prereg.json["hashes"] and the current files; [] if no prereg yet."""
    path = root / "results/prereg.json"
    if not path.exists():
        return []
    want: dict[str, str] = read_json(path).get("hashes", {})
    have = current_hashes(root)
    return [k for k, v in want.items() if (keys is None or k in keys) and have.get(k) != v]
