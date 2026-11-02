"""PROVENANCE.json: every file copied from Day8 at commit 5db71bd (copied, never imported).

usage (from Day9/): python provenance.py          rewrite PROVENANCE.json from MANIFEST and the current files
Entry: {src, dst, commit, sha256_src (blob in the commit), sha256_dst (current file), modified, note}.
tests/test_provenance.py checks that unmodified copies are byte-identical to the commit and that sha256_dst matches.
"""

from __future__ import annotations

import hashlib
import subprocess
from pathlib import Path
from typing import Any

from core.io import ROOT, sha256_file, write_json

COMMIT = "5db71bd162489dd3e3b6af4294939cee19cac557"
SRC_DIR = "Day8"

_UNMODIFIED = (
    "common.py", "principles.json", "core/__init__.py", "core/io.py", "core/text.py", "eval/__init__.py",
    "eval/stats.py", "eval/scorers/_util.py", "eval/scorers/format.py", "eval/scorers/ood.py",
    "eval/scorers/dissimilar.py", "eval/scorers/contamination.py", "eval/scorers/chrf.py", "gen/ood_checkers.py",
    "facts/company.json", "facts/regulation.json", "book/pages.jsonl", "data/format/train.jsonl",
    "data/format/valid.jsonl", "data/format/eval.jsonl", "data/ood/eval.jsonl", "data/dissimilar/eval.jsonl",
    "to_bf16.py", "tests/__init__.py", "tests/test_stats.py", "tests/test_text.py",
    "tests/fixtures/mini/facts/company.json", "tests/fixtures/mini/facts/regulation.json",
)
_MODIFIED: dict[str, tuple[str, str]] = {   # dst: (src, note)
    "eval/generate.py": ("eval/generate.py", "model/adapter/fused paths as parameters; BatchResponse.stats kept"),
    "eval/scorers/__init__.py": ("eval/scorers/__init__.py", "facts/style scorers removed"),
    "select_ckpt.py": ("select_ckpt.py", "one task: all checkpoints generated on valid 60, loss only as tie-break"),
    "train_log.py": ("val_ckpts.py", "only ANSI, ROW, parse_train kept; parse_val added"),
    "tests/test_scorers.py": ("tests/test_scorers.py", "facts/style tests removed"),
}

MANIFEST: list[dict[str, Any]] = (
    [{"src": f"{SRC_DIR}/{p}", "dst": p, "modified": False, "note": ""} for p in _UNMODIFIED]
    + [{"src": f"{SRC_DIR}/{src}", "dst": dst, "modified": True, "note": note} for dst, (src, note) in _MODIFIED.items()]
)


def git_blob(src: str, commit: str = COMMIT, cwd: Path = ROOT) -> bytes:
    return subprocess.run(["git", "show", f"{commit}:{src}"], cwd=cwd, capture_output=True, check=True).stdout


def build(root: Path = ROOT) -> list[dict[str, Any]]:
    out = []
    for e in MANIFEST:
        out.append({**e, "commit": COMMIT, "sha256_src": hashlib.sha256(git_blob(e["src"], cwd=root)).hexdigest(),
                    "sha256_dst": sha256_file(root / e["dst"])})
    return out


def main() -> int:
    entries = build()
    bad = [e["dst"] for e in entries if not e["modified"] and e["sha256_src"] != e["sha256_dst"]]
    if bad:
        raise SystemExit(f"unmodified copies differ from {COMMIT[:7]}: {bad}")
    write_json(ROOT / "PROVENANCE.json", {"commit": COMMIT, "source_dir": SRC_DIR, "policy":
                                          "copied from git, never imported from Day8", "files": entries})
    print(f"PROVENANCE.json: {len(entries)} files ({sum(e['modified'] for e in entries)} modified)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
