"""System prompts of Day9 (plan §3.2).

SYSTEM and SYSTEM_PROMPTED come unchanged from common.py (Day8 copy): base+good must reproduce Day8 (0.439).
LIST_PROMPT is the student prompt: SYSTEM plus the 15 principle names (no gist). LIST_PROMPT_GIST is the
pre-registered fallback F2 (names plus a one-line gist). NEUTRAL is the Qwen default used for OOD/dissimilar.

Any change of a prompt text must bump PROMPT_VERSION: it is part of the teacher cache key and of prereg hashes.
"""

from __future__ import annotations

from common import PRINCIPLES, SYSTEM, SYSTEM_PROMPTED
from core.io import sha256_text

PROMPT_VERSION = "day9-v1"

PRINCIPLE_NAMES: tuple[str, ...] = tuple(p["name"] for p in PRINCIPLES)

LIST_PROMPT = (
    SYSTEM
    + "\nДопустимые принципы (пиши название дословно, ровно одно): "
    + "; ".join(PRINCIPLE_NAMES)
    + "."
)


def _gist_head(gist: str) -> str:
    """First clause of a gist: up to the first ';' (the gists are long, F2 keeps one line per principle)."""
    return gist.split(";", 1)[0].strip().rstrip(".")


LIST_PROMPT_GIST = "\n".join([
    SYSTEM,
    "Допустимые принципы (пиши название дословно, ровно одно):",
    *[f"- {p['name']}: {_gist_head(p['gist'])}." for p in PRINCIPLES],
])

NEUTRAL = "You are Qwen, created by Alibaba Cloud. You are a helpful assistant."

# Prompt keys used by eval cells; "good" is the teacher prompt and the base prompt-engineering baseline.
BY_KEY: dict[str, str] = {
    "short": SYSTEM,
    "good": SYSTEM_PROMPTED,
    "list": LIST_PROMPT,
    "gist": LIST_PROMPT_GIST,
    "neutral": NEUTRAL,
}

NAMES: dict[str, str] = {
    "SYSTEM": SYSTEM,
    "SYSTEM_PROMPTED": SYSTEM_PROMPTED,
    "LIST_PROMPT": LIST_PROMPT,
    "LIST_PROMPT_GIST": LIST_PROMPT_GIST,
    "NEUTRAL": NEUTRAL,
}


def prompt_hashes() -> dict[str, str]:
    """sha256 of every prompt by name (prereg, cache key context, reports)."""
    return {name: sha256_text(text) for name, text in NAMES.items()}


def count_tokens(tokenizer_dir: str, texts: dict[str, str] | None = None) -> dict[str, int]:
    """Token counts with the local Qwen tokenizer (tokenizer.json only, no model weights are loaded)."""
    from tokenizers import Tokenizer

    tok = Tokenizer.from_file(f"{tokenizer_dir}/tokenizer.json")
    return {k: len(tok.encode(v, add_special_tokens=False).ids) for k, v in (texts or NAMES).items()}


DEFAULT_TOKENIZER = "../Day8/models/qwen2.5-7b-instruct-4bit-bf16"


def main(argv: list[str] | None = None) -> int:
    """Write results/prompt_tokens.json: Qwen token counts and sha256 of every prompt (read by prereg.py)."""
    import argparse

    from core.io import ROOT, write_json

    ap = argparse.ArgumentParser(description=main.__doc__)
    ap.add_argument("--tokenizer", default=str((ROOT / DEFAULT_TOKENIZER).resolve()))
    args = ap.parse_args(argv)
    out = {"prompt_version": PROMPT_VERSION, "tokenizer": args.tokenizer,
           "tokens": count_tokens(args.tokenizer), "sha256": prompt_hashes()}
    write_json(ROOT / "results/prompt_tokens.json", out)
    print(out["tokens"])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
