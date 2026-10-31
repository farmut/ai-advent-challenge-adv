"""Parse mlx_lm.lora logs and plot loss.

usage: python plot_loss.py results/train.log [other.log:label ...]
  -> results/loss.png, results/loss.csv (main run, train + val)
  -> results/loss_lr.png (train loss of the main run vs other runs), if other logs are given
"""
import re
import sys
from pathlib import Path

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt


def parse(log: Path):
    text = re.sub(r"\x1b\[[0-9;]*m", "", log.read_text(encoding="utf-8"))
    train = [(int(i), float(l)) for i, l in re.findall(r"^\s+(\d+)\s+([\d.]+|nan) [▲▼]", text, re.M)]
    val = [(int(i), float(l)) for i, l in re.findall(r"^\s+(\d+)\s+val\s+([\d.]+|nan)", text, re.M)]
    return train, val


main = Path(sys.argv[1] if len(sys.argv) > 1 else "results/train.log")
out = main.parent
train, val = parse(main)

with (out / "loss.csv").open("w") as fh:
    fh.write("iter,kind,loss\n")
    for kind, pts in (("train", train), ("val", val)):
        for i, l in pts:
            fh.write(f"{i},{kind},{l}\n")

plt.figure(figsize=(8, 4.5))
plt.plot(*zip(*train), label="train", color="#4C72B0", lw=1.2)
plt.plot(*zip(*val), "o-", label="val", color="#DD8452")
plt.xlabel("iteration (batch 4)")
plt.ylabel("loss (assistant tokens)")
plt.title("QLoRA Qwen2.5-7B-Instruct-4bit on SberQuAD")
plt.grid(alpha=0.3)
plt.legend()
plt.tight_layout()
plt.savefig(out / "loss.png", dpi=120)
print(f"train points {len(train)}, val {val}")

if len(sys.argv) > 2:
    plt.figure(figsize=(8, 4.5))
    plt.plot(*zip(*train), label="lr 2e-5 + warmup (final)", lw=1.4)
    for arg in sys.argv[2:]:
        path, _, label = arg.partition(":")
        t, _ = parse(Path(path))
        plt.plot(*zip(*t), "--", label=label or path, lw=1.2)
    plt.xlabel("iteration (batch 4)")
    plt.ylabel("train loss")
    plt.title("Learning rate: diverged runs vs final")
    plt.grid(alpha=0.3)
    plt.legend()
    plt.tight_layout()
    plt.savefig(out / "loss_lr.png", dpi=120)
