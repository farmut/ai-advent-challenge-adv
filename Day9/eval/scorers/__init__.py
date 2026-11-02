"""Scorers. Each module exposes score(item, text) -> dict with a boolean/float "correct" field
and summarize(rows) -> dict of aggregate metrics.

Day9: facts/style scorers of Day8 are not copied (no such tasks here)."""

from types import ModuleType

from eval.scorers import contamination, dissimilar, format, ood

SCORERS: dict[str, ModuleType] = {"format": format, "ood": ood, "dissimilar": dissimilar}

__all__ = ["SCORERS", "contamination"]
