"""chrF (character n-gram F-score, Popović 2015) in pure Python, sacrebleu defaults: n=6, beta=2, no spaces."""

from __future__ import annotations

from collections import Counter


def _ngrams(text: str, n: int) -> Counter[str]:
    s = "".join(text.split())
    return Counter(s[i:i + n] for i in range(len(s) - n + 1))


def chrf(hyp: str, ref: str, order: int = 6, beta: float = 2.0) -> float:
    """Sentence chrF in 0..100: precision and recall averaged over n=1..order, then F-beta."""
    precs, recs = [], []
    for n in range(1, order + 1):
        h, r = _ngrams(hyp, n), _ngrams(ref, n)
        if not h or not r:
            continue
        overlap = sum((h & r).values())
        precs.append(overlap / sum(h.values()))
        recs.append(overlap / sum(r.values()))
    if not precs:
        return 0.0
    p, r = sum(precs) / len(precs), sum(recs) / len(recs)
    if p + r == 0:
        return 0.0
    b2 = beta * beta
    return round(100 * (1 + b2) * p * r / (b2 * p + r), 2)
