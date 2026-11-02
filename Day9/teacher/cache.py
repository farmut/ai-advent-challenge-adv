"""Disk cache of teacher responses: cache/teacher/<k[:2]>/<k>.json, written atomically, only for HTTP 200.

The key is sha256 of a canonical JSON of everything that determines the answer: model, messages, temperature,
max_tokens, reasoning, seed, sample_idx, prompt_version, the pinned provider order, the endpoint (backend + url)
and the backend-specific request body extras (e.g. chat_template_kwargs that switch thinking off).
"""

from __future__ import annotations

import hashlib
import json
import os
import tempfile
from collections.abc import Mapping, Sequence
from pathlib import Path
from typing import Any


def canonical(obj: Any) -> str:
    return json.dumps(obj, sort_keys=True, ensure_ascii=False, separators=(",", ":"))


def cache_key(*, model: str, messages: Sequence[dict[str, str]], temperature: float, max_tokens: int,
              reasoning: str, seed: int | None, sample_idx: int, prompt_version: str,
              provider: Sequence[str] = (), endpoint: str = "",
              extra: Mapping[str, Any] | None = None) -> str:
    """sha256 hex of the canonical request description (0 and 0.0 give the same key)."""
    desc = {
        "model": model,
        "messages": [{"role": m["role"], "content": m["content"]} for m in messages],
        "temperature": float(temperature),
        "max_tokens": int(max_tokens),
        "reasoning": reasoning,
        "seed": seed,
        "sample_idx": int(sample_idx),
        "prompt_version": prompt_version,
        "provider": list(provider),
        "endpoint": endpoint,
        "extra": dict(extra or {}),
    }
    return hashlib.sha256(canonical(desc).encode("utf-8")).hexdigest()


class TeacherCache:
    """Content-addressed JSON files. get() returns None on a miss or an unreadable file."""

    def __init__(self, root: str | Path) -> None:
        self.root = Path(root)

    def path(self, key: str) -> Path:
        return self.root / key[:2] / f"{key}.json"

    def get(self, key: str) -> dict[str, Any] | None:
        p = self.path(key)
        try:
            return json.loads(p.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            return None

    def put(self, key: str, record: dict[str, Any]) -> Path:
        if record.get("status") != 200:
            raise ValueError("only HTTP 200 responses are cached")
        p = self.path(key)
        p.parent.mkdir(parents=True, exist_ok=True)
        fd, tmp = tempfile.mkstemp(prefix=".tmp_", dir=p.parent)
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as f:
                json.dump(record, f, ensure_ascii=False)
            os.replace(tmp, p)
        except BaseException:
            Path(tmp).unlink(missing_ok=True)
            raise
        return p
