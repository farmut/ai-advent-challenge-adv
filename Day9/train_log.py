"""Train / valid loss from the stdout of a stock `mlx_lm.lora` run (mlx-lm 0.32.2).

Day8 copy (val_ckpts.py): only ANSI, ROW and parse_train are kept; parse_val is new. Day9 does not recompute
per-task losses offline: the student has one task and the stock trainer reports the pooled valid loss at
every checkpoint (steps_per_eval == save_every), which select_ckpt.py uses as the tie-break.

Rows of the stock report: "    25    2.044 ▼      148      6.2k" (train) and "    50    val 2.114    11.83s" (val).
"""

from __future__ import annotations

import re
from pathlib import Path

ANSI = re.compile(r"\x1b\[[0-9;]*m")
# stock mlx_lm 0.32.2 report row: "    25    2.044 ▼    1.2e-05 ..." (columns vary; we take iter and train_loss)
ROW = re.compile(r"^\s*(\d+)\s+([0-9]+\.[0-9]+)\s")
VAL_ROW = re.compile(r"^\s*(\d+)\s+val\s+([0-9]+\.[0-9]+|nan|inf)\b", re.I)


def _lines(stdout: Path) -> list[str]:
    return ANSI.sub("", stdout.read_text(encoding="utf-8", errors="replace")).splitlines()


def parse_train(stdout: Path) -> list[dict]:
    rows = []
    for line in _lines(stdout):
        if "val" in line:
            continue
        if m := ROW.match(line):
            rows.append({"type": "train", "step": int(m.group(1)), "loss": float(m.group(2))})
    return rows


def parse_val(stdout: Path) -> dict[int, float]:
    """{step: pooled valid loss}; nan/inf are kept as float so a divergence is visible."""
    out: dict[int, float] = {}
    for line in _lines(stdout):
        if m := VAL_ROW.match(line):
            out[int(m.group(1))] = float(m.group(2))
    return out
