"""Scorers. Each module exposes score(item, text) -> dict with a boolean/float "correct" field
and summarize(rows) -> dict of aggregate metrics."""

from types import ModuleType

from eval.scorers import contamination, dissimilar, facts, format, ood, style

SCORERS: dict[str, ModuleType] = {"format": format, "facts": facts, "style": style, "ood": ood,
                                  "dissimilar": dissimilar}

__all__ = ["SCORERS", "contamination"]
