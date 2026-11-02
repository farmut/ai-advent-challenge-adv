"""Eval sets, prompts, models and the cell matrix of Day9 (plan §3.4, §4.3, §6.6).

A cell is "<set>:<model>:<prompt>", e.g. "format:student:list"; results go to eval_<set>__<model>__<prompt>.json
(teacher cells get a reasoning suffix "@off" / "@low": "format:teacher:good@off").
  set:    format (eval 57) | valid (data/distill/valid.jsonl, 60) | valid_orig (data/format/valid.jsonl, 30)
          | ood (100) | dissimilar (32)
  model:  base | student | teacher | ckpt<step>
  prompt: short (SYSTEM) | good (SYSTEM_PROMPTED) | list (LIST_PROMPT) | gist (LIST_PROMPT_GIST) | neutral
          | train (the system message stored in the row)
Only eval.run and report.py may read the format eval set (tests/test_isolation.py).
"""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import prompts
from common import parse_answer
from core.io import ROOT, read_jsonl

Item = dict[str, Any]

MODELS = ("base", "student", "teacher")
PROMPTS = ("short", "good", "list", "gist", "neutral", "train")
REASONING_LEVELS = ("off", "low", "medium", "high")
REASONING_ON = "low"      # "--reasoning on" means effort=low (plan §4.4, frozen in prereg v1)


def _format_item(r: Item) -> Item:
    if "messages" in r:  # valid rows: labels from meta (distill) or from the reference answer (Day8 valid)
        meta = r.get("meta") or {}
        p = parse_answer(r["messages"][2]["content"])
        rid = r.get("id") or f"VO-{r['_idx']:03d}"
        return {"id": rid, "user": r["messages"][1]["content"], "system_train": r["messages"][0]["content"],
                "gold_verdict": meta.get("gold_verdict", p["verdict"]),
                "gold_principle": meta.get("gold_principle", p["principle"]), "cluster": rid,
                "source": meta.get("source", "orig")}
    return {"id": r["id"], "user": r["situation"], "gold_verdict": r["verdict"], "gold_principle": r["principle"],
            "cluster": r["id"]}


def _ood_item(r: Item) -> Item:
    return {"id": r["id"], "user": r["prompt"], "category": r["category"], "gold": r.get("gold", ""),
            "answer_keys": r.get("answer_keys", []), "checker": r["checker"], "cluster": r["id"]}


def _dissimilar_item(r: Item) -> Item:
    return {"id": r["id"], "user": r["prompt"], "group": r["group"], "lang": r.get("lang"),
            "checker": r["checker"], "answer_keys": r.get("answer_keys", []), "note": r.get("note", ""),
            "cluster": r["id"]}


@dataclass(frozen=True)
class EvalSet:
    path: str
    scorer: str
    max_tokens: int
    to_item: Callable[[Item], Item]
    prompts: tuple[str, ...]

    def items(self, root: Path = ROOT) -> list[Item]:
        rows = read_jsonl(root / self.path)
        return [self.to_item({**r, "_idx": i}) for i, r in enumerate(rows)]


FORMAT_PROMPTS = ("short", "good", "list", "gist")
SETS: dict[str, EvalSet] = {
    "format": EvalSet("data/format/eval.jsonl", "format", 400, _format_item, FORMAT_PROMPTS),
    "valid": EvalSet("data/distill/valid.jsonl", "format", 400, _format_item, (*FORMAT_PROMPTS, "train")),
    "valid_orig": EvalSet("data/format/valid.jsonl", "format", 400, _format_item, FORMAT_PROMPTS),
    "ood": EvalSet("data/ood/eval.jsonl", "ood", 512, _ood_item, ("neutral", "short", "list", "good")),
    "dissimilar": EvalSet("data/dissimilar/eval.jsonl", "dissimilar", 400, _dissimilar_item,
                          ("neutral", "short", "list", "good")),
}
# teacher max_tokens include reasoning tokens (OpenRouter counts them as completion)
TEACHER_MAX_TOKENS = {"off": 400, "low": 2048, "medium": 4096, "high": 8192}


@dataclass(frozen=True)
class Cell:
    set: str
    model: str
    prompt: str
    reasoning: str | None = None   # teacher only

    @property
    def name(self) -> str:
        base = f"{self.set}:{self.model}:{self.prompt}"
        return base + (f"@{self.reasoning}" if self.reasoning else "")

    @property
    def file_stem(self) -> str:
        stem = f"eval_{self.set}__{self.model}__{self.prompt}"
        return stem + (f"__r-{self.reasoning}" if self.reasoning else "")

    @property
    def backend(self) -> str:
        return "teacher" if self.model == "teacher" else "local"

    def max_tokens(self) -> int:
        if self.backend == "teacher":
            return max(SETS[self.set].max_tokens, TEACHER_MAX_TOKENS[self.reasoning or "off"])
        return SETS[self.set].max_tokens


def parse_cell(name: str) -> Cell:
    """'format:teacher:good@low' -> Cell. Teacher cells default to reasoning 'off'."""
    head, _, reasoning = name.partition("@")
    parts = head.split(":")
    if len(parts) != 3:
        raise ValueError(f"bad cell {name!r}: expected set:model:prompt")
    s, m, p = parts
    if s not in SETS:
        raise ValueError(f"bad set in cell {name!r}")
    if p not in SETS[s].prompts:
        raise ValueError(f"prompt {p!r} not allowed for set {s!r}")
    if m not in MODELS and not (m.startswith("ckpt") and m[4:].isdigit()):
        raise ValueError(f"bad model in cell {name!r}")
    if m == "teacher":
        reasoning = {"on": REASONING_ON}.get(reasoning, reasoning) or "off"
        if reasoning not in REASONING_LEVELS:
            raise ValueError(f"bad reasoning in cell {name!r}")
        return Cell(s, m, p, reasoning)
    if reasoning:
        raise ValueError(f"reasoning suffix is only for teacher cells: {name!r}")
    return Cell(s, m, p)


def system_prompt(cell: Cell, item: Item | None = None) -> str:
    """The explicit system message; never empty (Qwen would insert its default)."""
    if cell.prompt == "train":
        if item is None or "system_train" not in item:
            raise ValueError(f"{cell.set}: prompt 'train' needs rows with a system message")
        return item["system_train"]
    return prompts.BY_KEY[cell.prompt]


# Block B (baseline, before any teacher call) and G1.
BASELINE: tuple[str, ...] = ("format:base:good", "format:base:list", "format:base:short",
                             "ood:base:neutral", "dissimilar:base:neutral")
TEACHER_G1: tuple[str, ...] = ("format:teacher:good@off", "format:teacher:good@on")
# Block D (plan §6.6): main comparison, then reference cells.
REQUIRED: tuple[str, ...] = ("format:base:list", "format:base:good", "format:student:list", "format:teacher:good",
                             "ood:base:neutral", "ood:student:neutral",
                             "dissimilar:base:neutral", "dissimilar:student:neutral")
OPTIONAL: tuple[str, ...] = ("format:teacher:list", "format:base:short", "format:student:good",
                             "format:student:short", "format:teacher:short")
G0_REGRESSION = {"cell": "format:base:good", "metric": "both_acc", "expected": 0.439, "tol": 0.035}

for _c in BASELINE + TEACHER_G1 + REQUIRED + OPTIONAL:
    parse_cell(_c)
