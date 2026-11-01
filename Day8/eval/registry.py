"""Eval sets, prompt choices and the cell matrix (plan steps 5 and 8).

A cell is "<set>__<model>__<prompt>", e.g. "facts__best__rag". model is base | best | overfit | ckpt<step>.
Prompt "train" means: use the system message stored in the row (valid sets, same prompt as in training).
"""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
import json
from pathlib import Path
from typing import Any

from common import parse_answer
from core import prompts
from core.io import ROOT, read_jsonl

Item = dict[str, Any]


def _format_item(r: Item) -> Item:
    if "messages" in r:  # valid rows: gold labels come from the reference answer
        p = parse_answer(r["messages"][2]["content"])
        return {"id": r["id"], "user": r["messages"][1]["content"], "system_train": r["messages"][0]["content"],
                "gold_verdict": p["verdict"], "gold_principle": p["principle"], "cluster": r["id"]}
    return {"id": r["id"], "user": r["situation"], "gold_verdict": r["verdict"], "gold_principle": r["principle"],
            "cluster": r["id"]}


def _facts_item(r: Item) -> Item:
    fact_ids = r.get("fact_ids") or [r["fact_id"]]
    out = {"id": r["id"], "user": r["question"] if "question" in r else r["messages"][1]["content"],
           "strata": r["strata"], "lang": r.get("lang", "ru"), "fact_ids": fact_ids,
           "answer_keys": r["answer_keys"], "require_all": r.get("require_all", True),
           "cluster": "+".join(sorted(fact_ids))}
    if "messages" in r:
        out["system_train"] = r["messages"][0]["content"]
    return out


def _style_item(r: Item) -> Item:
    out = {"id": r["id"], "user": r["question"] if "question" in r else r["messages"][1]["content"],
           "topic_id": r.get("topic_id"), "cluster": r.get("topic_id") or r["id"]}
    if "messages" in r:
        out["system_train"] = r["messages"][0]["content"]
    return out


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
        drop = excluded_ids(root)
        return [self.to_item(r) for r in read_jsonl(root / self.path) if r["id"] not in drop]


def excluded_ids(root: Path = ROOT) -> set[str]:
    """Eval rows excluded after the second audit round (data/excluded.json: {id: reason})."""
    path = root / "data/excluded.json"
    return set(json.loads(path.read_text(encoding="utf-8"))) if path.exists() else set()


MAX_TOKENS = {"format": 400, "facts": 160, "style": 300, "ood": 512, "dissimilar": 400}

SETS: dict[str, EvalSet] = {
    "format": EvalSet("data/format/eval.jsonl", "format", 400, _format_item, ("weak", "good")),
    "facts": EvalSet("data/facts/eval.jsonl", "facts", 160, _facts_item, ("tag", "rag", "neutral")),
    "style": EvalSet("data/style/eval.jsonl", "style", 300, _style_item, ("tag", "good", "neutral")),
    "ood": EvalSet("data/ood/eval.jsonl", "ood", 512, _ood_item, ("neutral",)),
    "dissimilar": EvalSet("data/dissimilar/eval.jsonl", "dissimilar", 400, _dissimilar_item, ("neutral",)),
    "valid_format": EvalSet("data/mix/valid_format.jsonl", "format", 400, _format_item, ("train",)),
    "valid_facts": EvalSet("data/mix/valid_facts.jsonl", "facts", 160, _facts_item, ("train",)),
    "valid_style": EvalSet("data/mix/valid_style.jsonl", "style", 300, _style_item, ("train",)),
}


def system_prompt(set_name: str, prompt: str, item: Item | None = None) -> str:
    """The explicit system message for a (set, prompt) pair; never empty (Qwen would insert its default)."""
    task = SETS[set_name].scorer
    if prompt == "train":
        if item is None or "system_train" not in item:
            raise ValueError(f"{set_name}: prompt 'train' needs rows with a system message")
        return item["system_train"]
    table: dict[str, dict[str, Callable[[], str]]] = {
        "format": {"weak": lambda: prompts.FORMAT_WEAK, "good": lambda: prompts.FORMAT_GOOD},
        "facts": {"tag": lambda: prompts.TAG_FACTS, "rag": lambda: prompts.FACTS_RAG,
                  "neutral": lambda: prompts.NEUTRAL},
        "style": {"tag": lambda: prompts.TAG_STYLE, "good": lambda: prompts.STYLE_GOOD,
                  "neutral": lambda: prompts.NEUTRAL},
        "ood": {"neutral": lambda: prompts.NEUTRAL},
        "dissimilar": {"neutral": lambda: prompts.NEUTRAL},
    }
    try:
        return table[task][prompt]()
    except KeyError:
        raise ValueError(f"no prompt {prompt!r} for set {set_name!r}") from None


def cell(set_name: str, model: str, prompt: str) -> str:
    return f"{set_name}__{model}__{prompt}"


def parse_cell(name: str) -> tuple[str, str, str]:
    s, m, p = name.split("__")
    if s not in SETS or p not in SETS[s].prompts:
        raise ValueError(f"bad cell {name!r}")
    if m not in ("base", "best", "overfit") and not (m.startswith("ckpt") and m[4:].isdigit()):
        raise ValueError(f"bad model in cell {name!r}")
    return s, m, p


# Plan step 8: 21 required cells. The first one doubles as the Day7 regression (base + FORMAT_GOOD).
REQUIRED: tuple[str, ...] = (
    "format__base__good", "format__base__weak", "format__best__good", "format__best__weak", "format__overfit__good",
    "facts__base__tag", "facts__base__rag", "facts__best__tag", "facts__best__rag", "facts__overfit__tag",
    "style__base__tag", "style__base__good", "style__best__tag", "style__best__good", "style__overfit__tag",
    "ood__base__neutral", "ood__best__neutral", "ood__overfit__neutral",
    "dissimilar__base__neutral", "dissimilar__best__neutral", "dissimilar__overfit__neutral",
)
OPTIONAL: tuple[str, ...] = (
    "facts__base__neutral", "facts__overfit__rag", "format__overfit__weak", "style__overfit__good",
)
DAY7_REGRESSION = {"cell": "format__base__good", "metric": "both_acc", "expected": 0.439, "tol": 0.035}

for _c in REQUIRED + OPTIONAL:
    parse_cell(_c)
assert len(REQUIRED) == 21
