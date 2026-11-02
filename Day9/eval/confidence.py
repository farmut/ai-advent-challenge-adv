"""Student confidence from label log-probabilities (plan §6.10).

For every item: teacher-force "Вердикт: " after the chat prompt and score each of the 3 verdicts; then
"Вердикт: <argmax>\nПринцип: " and score each of the 15 principle names. Options are scored as full token
sequences (sum of token log-probs, exact even when two options share a first token); distinguishable() reports
whether the cheaper first-token softmax would have been well defined. The math (softmax, margin, entropy, ECE,
AUROC) is pure and unit-tested; model scoring needs mlx and runs in block D.
"""

from __future__ import annotations

import math
from collections.abc import Mapping, Sequence
from typing import Any


def softmax(logps: Mapping[str, float]) -> dict[str, float]:
    m = max(logps.values())
    ex = {k: math.exp(v - m) for k, v in logps.items()}
    z = sum(ex.values())
    return {k: v / z for k, v in ex.items()}


def summarize_dist(logps: Mapping[str, float]) -> dict[str, Any]:
    """argmax, p_max, margin (p1 − p2), entropy (nats) of the renormalised option distribution."""
    p = softmax(logps)
    ranked = sorted(p.items(), key=lambda kv: -kv[1])
    top, p1 = ranked[0]
    p2 = ranked[1][1] if len(ranked) > 1 else 0.0
    ent = -sum(v * math.log(v) for v in p.values() if v > 0)
    return {"label": top, "p": p1, "margin": p1 - p2, "entropy": ent, "dist": p}


def distinguishable(first_tokens: Mapping[str, int]) -> bool:
    """True if every option starts with a different token (first-token softmax is then well defined)."""
    return len(set(first_tokens.values())) == len(first_tokens)


def joint_confidence(p_verdict: float, p_principle: float) -> float:
    return p_verdict * p_principle


def ece(conf: Sequence[float], correct: Sequence[bool], bins: int = 10) -> float:
    """Expected calibration error with equal-width bins on [0, 1]."""
    if len(conf) != len(correct) or not conf:
        raise ValueError("paired non-empty sequences expected")
    n = len(conf)
    total = 0.0
    for b in range(bins):
        lo, hi = b / bins, (b + 1) / bins
        idx = [i for i, c in enumerate(conf) if (lo <= c < hi) or (b == bins - 1 and c == 1.0)]
        if idx:
            acc = sum(bool(correct[i]) for i in idx) / len(idx)
            avg = sum(conf[i] for i in idx) / len(idx)
            total += len(idx) / n * abs(acc - avg)
    return total


def auroc(scores: Sequence[float], labels: Sequence[bool]) -> float | None:
    """P(score of a correct item > score of a wrong one), ties count 1/2; None if one class is empty."""
    pos = [s for s, y in zip(scores, labels) if y]
    neg = [s for s, y in zip(scores, labels) if not y]
    if not pos or not neg:
        return None
    wins = sum((p > q) + 0.5 * (p == q) for p in pos for q in neg)
    return wins / (len(pos) * len(neg))


# ---------- model scoring (mlx, block D) ----------

def option_logprobs(model: Any, prefix: list[int], options: Mapping[str, list[int]]) -> dict[str, float]:
    """Sum of log-probs of each option's tokens after `prefix` (exact, one forward pass per option)."""
    import mlx.core as mx

    out = {}
    for name, ids in options.items():
        seq = mx.array([prefix + ids])
        logits = model(seq[:, :-1])
        logp = logits - mx.logsumexp(logits, axis=-1, keepdims=True)
        tail = logp[0, len(prefix) - 1:, :]
        out[name] = float(sum(tail[i, t].item() for i, t in enumerate(ids)))
    return out


def score_item(model: Any, tok: Any, system: str, user: str, verdicts: Sequence[str],
               principles: Mapping[str, str]) -> dict[str, Any]:
    """Verdict distribution after 'Вердикт: ', then principle distribution given the argmax verdict."""
    from eval.generate import encode

    base = encode(tok, system, user)
    enc = lambda s: list(tok.encode(s, add_special_tokens=False))  # noqa: E731
    v = summarize_dist(option_logprobs(model, base + enc("Вердикт:"), {x: enc(" " + x) for x in verdicts}))
    pre = base + enc(f"Вердикт: {v['label']}\nПринцип:")
    pr = summarize_dist(option_logprobs(model, pre, {pid: enc(" " + name) for pid, name in principles.items()}))
    return {"verdict": v, "principle": pr, "joint": joint_confidence(v["p"], pr["p"])}
