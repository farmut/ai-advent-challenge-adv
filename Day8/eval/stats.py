"""Statistics for paired comparisons: exact McNemar, cluster paired bootstrap, Wilson interval."""

from __future__ import annotations

import math
from collections.abc import Sequence

import numpy as np

B_DEFAULT = 10_000
SEED = 42


def wilson(k: int, n: int, z: float = 1.959964) -> tuple[float, float]:
    """Wilson score interval for k successes out of n."""
    if n == 0:
        return (0.0, 1.0)
    p = k / n
    den = 1 + z * z / n
    centre = (p + z * z / (2 * n)) / den
    half = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / den
    return (max(0.0, centre - half), min(1.0, centre + half))


def mcnemar_exact(b: int, c: int) -> float:
    """Two-sided exact McNemar p-value from the discordant counts b (only A right) and c (only B right)."""
    n = b + c
    if n == 0:
        return 1.0
    tail = sum(math.comb(n, i) for i in range(min(b, c) + 1)) / 2 ** n
    return min(1.0, 2 * tail)


def mcnemar(a: Sequence[bool], b: Sequence[bool]) -> dict[str, float | int]:
    """Paired binary outcomes of two systems on the same items."""
    if len(a) != len(b):
        raise ValueError("paired sequences differ in length")
    only_a = sum(1 for x, y in zip(a, b) if x and not y)
    only_b = sum(1 for x, y in zip(a, b) if y and not x)
    return {"only_a": only_a, "only_b": only_b, "p": mcnemar_exact(only_a, only_b)}


def paired_bootstrap(a: Sequence[float], b: Sequence[float], clusters: Sequence[str] | None = None,
                     n_boot: int = B_DEFAULT, seed: int = SEED, alpha: float = 0.05) -> dict[str, float]:
    """Percentile CI of mean(b) - mean(a); resamples whole clusters (e.g. fact ids) when given."""
    if len(a) != len(b) or not a:
        raise ValueError("need two non-empty paired sequences of equal length")
    diff = np.asarray(b, dtype=float) - np.asarray(a, dtype=float)
    keys = list(clusters) if clusters is not None else list(range(len(diff)))
    if len(keys) != len(diff):
        raise ValueError("clusters length differs from data")
    uniq = {k: i for i, k in enumerate(dict.fromkeys(keys))}
    idx = np.array([uniq[k] for k in keys])
    sums = np.bincount(idx, weights=diff, minlength=len(uniq))
    counts = np.bincount(idx, minlength=len(uniq)).astype(float)
    rng = np.random.default_rng(seed)
    draw = rng.integers(0, len(uniq), size=(n_boot, len(uniq)))
    deltas = sums[draw].sum(axis=1) / counts[draw].sum(axis=1)
    lo, hi = np.quantile(deltas, [alpha / 2, 1 - alpha / 2])
    return {"delta": float(diff.mean()), "lo": float(lo), "hi": float(hi), "n": len(diff),
            "n_clusters": len(uniq)}


def non_inferior(ci: dict[str, float], margin: float = -0.05) -> bool:
    """Non-inferiority: the lower CI bound of (tuned - base) is at or above the margin."""
    return ci["lo"] >= margin
