"""LoRA training on the mix with grad clipping, per-task validation and autostop (plan step 6).

usage: python train.py configs/main.yaml [--run main]
  adapters/<run>/    adapter_config.json, adapters.safetensors, 00000NN_adapters.safetensors (refuses a non-empty dir)
  results/<run>/     log.jsonl ({"type": "train"|"val", ...}), meta.json
Validation at step N is computed on the weights after N updates, i.e. exactly checkpoint N (it runs inside
on_train_loss_report, which mlx_lm calls before saving that step). Step 0 = loss before training.
"""

from __future__ import annotations

import argparse
import json
import math
import platform
import sys
import time
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Any

import mlx.core as mx
import mlx.optimizers as optim
import numpy as np
import yaml

from core.io import ROOT, git_hash, read_jsonl, sha256_file, write_json

TASKS = ("format", "facts", "style")


@dataclass
class TrainConfig:
    iters: int
    learning_rate: float
    warmup: int
    max_grad_norm: float
    num_layers: int
    lora_parameters: dict[str, Any]
    batch_size: int = 4
    seed: int = 42
    max_seq_length: int = 2048
    mask_prompt: bool = True
    grad_checkpoint: bool = True
    steps_per_report: int = 5
    eval_every: int = 25
    save_every: int = 25
    model: str = "models/qwen2.5-7b-instruct-4bit-bf16"
    data: str = "data/mix"
    autostop_factor: float = 2.0
    autostop_patience: int = 2
    extra: dict[str, Any] = field(default_factory=dict)

    def __post_init__(self) -> None:
        if self.eval_every % self.steps_per_report or self.save_every % self.steps_per_report:
            raise ValueError("eval_every and save_every must be multiples of steps_per_report")
        if not {"rank", "scale", "dropout", "keys"} <= self.lora_parameters.keys():
            raise ValueError("lora_parameters needs rank, scale, dropout, keys")

    @classmethod
    def from_yaml(cls, path: Path) -> TrainConfig:
        raw = yaml.safe_load(path.read_text(encoding="utf-8"))
        known = set(cls.__dataclass_fields__)
        return cls(**{k: v for k, v in raw.items() if k in known},
                   extra={k: v for k, v in raw.items() if k not in known})


class ClipAdam(optim.Adam):
    """Adam with global grad-norm clipping. Norms live in the optimizer state, so they survive mx.compile
    (the trainer compiles the step with optimizer.state as input/output)."""

    def __init__(self, max_norm: float, **kwargs: Any) -> None:
        super().__init__(**kwargs)
        self.max_norm = max_norm
        self.state["grad_norm"] = mx.array(0.0, dtype=mx.float32)
        self.state["grad_norm_max"] = mx.array(0.0, dtype=mx.float32)

    def apply_gradients(self, gradients: dict, parameters: dict) -> dict:
        gradients, norm = optim.clip_grad_norm(gradients, self.max_norm)
        norm = norm.astype(mx.float32)
        self.state["grad_norm"] = norm
        self.state["grad_norm_max"] = mx.maximum(self.state["grad_norm_max"], norm)
        return super().apply_gradients(gradients, parameters)

    def pop_grad_norms(self) -> tuple[float, float]:
        """(last, max since the previous call); resets the max."""
        last, mx_ = self.state["grad_norm"].item(), self.state["grad_norm_max"].item()
        self.state["grad_norm_max"] = mx.array(0.0, dtype=mx.float32)
        return last, mx_


def lr_schedule(lr: float, warmup: int) -> Any:
    """Linear warmup lr/warmup, 2lr/warmup, ..., lr over the first `warmup` updates, then constant lr.

    linear_schedule clamps at `end` after `steps`; unlike mlx_lm's warmup (init 0) the first update is not wasted.
    """
    if warmup <= 1:
        return lr
    return optim.linear_schedule(lr / warmup, lr, warmup - 1)


def batches_no_drop(dataset: Any, batch_size: int, max_seq_length: int, comm_group: Any = None, **_: Any) -> Any:
    """Like mlx_lm iterate_batches, but one ordered pass that keeps the last partial batch."""
    idx = sorted(range(len(dataset)), key=lambda i: len(dataset[i][0]))
    for s in range(0, len(idx), batch_size):
        items = [dataset[j] for j in idx[s:s + batch_size]]
        toks = [t[:max_seq_length] for t, _ in items]
        width = min(1 + 32 * ((max(map(len, toks)) + 31) // 32), max_seq_length)
        arr = np.zeros((len(toks), width), np.int32)
        for k, t in enumerate(toks):
            arr[k, :len(t)] = t
        yield mx.array(arr), mx.array([(off, len(t)) for (_, off), t in zip(items, toks)])


class StopTraining(Exception):
    pass


class Logger:
    def __init__(self, path: Path) -> None:
        self.path = path
        path.parent.mkdir(parents=True, exist_ok=True)

    def write(self, rec: dict[str, Any]) -> None:
        with self.path.open("a", encoding="utf-8") as f:
            f.write(json.dumps(rec, ensure_ascii=False) + "\n")
        print(json.dumps(rec, ensure_ascii=False), flush=True)


def evaluate_tasks(model: Any, valid: dict[str, Any], cfg: TrainConfig) -> dict[str, float]:
    """Token-weighted loss per task without dropping the remainder, plus macro (mean of tasks) and pooled."""
    from mlx_lm.tuner.trainer import default_loss

    out: dict[str, float] = {}
    tot_loss, tot_tok = 0.0, 0
    model.eval()
    for task, ds in valid.items():
        s, n = mx.array(0.0), mx.array(0)
        for batch, lengths in batches_no_drop(ds, cfg.batch_size, cfg.max_seq_length):
            loss, toks = default_loss(model, batch, lengths)
            s, n = s + loss * toks, n + toks
            mx.eval(s, n)
        out[task] = s.item() / n.item()
        tot_loss, tot_tok = tot_loss + s.item(), tot_tok + n.item()
    model.train()
    out["macro"] = sum(out[t] for t in valid) / len(valid)
    out["pooled"] = tot_loss / tot_tok
    return out


class Monitor:
    """Autostop: NaN/inf, or the value > factor x running minimum for `patience` consecutive reports.

    Applied to the EMA of train reports (mlx_lm batches are sorted by length, so they are nearly single-task and
    a raw 5-step average jumps with the batch mix) and, separately, to the val macro loss.
    """

    def __init__(self, factor: float, patience: int, alpha: float = 1.0) -> None:
        self.factor, self.patience, self.alpha = factor, patience, alpha
        self.best = math.inf
        self.bad = 0
        self.ema: float | None = None

    def update(self, loss: float) -> str | None:
        if not math.isfinite(loss):
            return f"non-finite loss {loss}"
        self.ema = loss if self.ema is None else self.alpha * loss + (1 - self.alpha) * self.ema
        self.bad = self.bad + 1 if self.ema > self.factor * self.best else 0
        self.best = min(self.best, self.ema)
        if self.bad >= self.patience:
            return f"loss {self.ema:.4f} > {self.factor} x min {self.best:.4f} for {self.bad} reports"
        return None


TRAIN_EMA_ALPHA = 0.3


def make_callback(model: Any, opt: ClipAdam, valid: dict[str, Any], cfg: TrainConfig, log: Logger) -> Any:
    from mlx_lm.tuner.callbacks import TrainingCallback

    monitor = Monitor(cfg.autostop_factor, cfg.autostop_patience, TRAIN_EMA_ALPHA)
    val_monitor = Monitor(cfg.autostop_factor, cfg.autostop_patience)

    class Callback(TrainingCallback):
        def on_train_loss_report(self, info: dict[str, Any]) -> None:
            step = info["iteration"]
            last, gmax = opt.pop_grad_norms()
            log.write({"type": "train", "step": step, "loss": info["train_loss"], "lr": info["learning_rate"],
                       "grad_norm": last, "grad_norm_max": gmax, "it_per_sec": info["iterations_per_second"],
                       "tokens_per_sec": info["tokens_per_second"], "trained_tokens": info["trained_tokens"],
                       "peak_mem_gb": info["peak_memory"], "time": time.time()})
            if reason := monitor.update(info["train_loss"]):
                raise StopTraining(f"step {step}: {reason}")
            if step % cfg.eval_every == 0:
                macro = val_step(model, valid, cfg, log, step)
                if reason := val_monitor.update(macro):
                    raise StopTraining(f"step {step}: val macro {reason}")

    return Callback()


def val_step(model: Any, valid: dict[str, Any], cfg: TrainConfig, log: Logger, step: int) -> float:
    t0 = time.perf_counter()
    losses = evaluate_tasks(model, valid, cfg)
    sec = round(time.perf_counter() - t0, 1)
    for task, loss in losses.items():
        log.write({"type": "val", "step": step, "task": task, "loss": loss, "sec": sec})
    if not all(math.isfinite(v) for v in losses.values()):
        raise StopTraining(f"step {step}: non-finite val loss {losses}")
    return losses["macro"]


def versions() -> dict[str, str]:
    import importlib.metadata as md

    return {"python": platform.python_version(), **{p: md.version(p) for p in ("mlx", "mlx-lm", "numpy")}}


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("config", type=Path)
    ap.add_argument("--run", help="run id (default: config file stem)")
    args = ap.parse_args()
    cfg = TrainConfig.from_yaml(args.config)
    run = args.run or args.config.stem
    adapters = ROOT / "adapters" / run
    results = ROOT / "results" / run
    if adapters.exists() and any(adapters.iterdir()):
        print(f"{adapters} is not empty; choose a new run id", file=sys.stderr)
        return 1
    if (results / "log.jsonl").exists():
        print(f"{results / 'log.jsonl'} exists; choose a new run id", file=sys.stderr)
        return 1

    from mlx_lm import load
    from mlx_lm.tuner.datasets import CacheDataset, ChatDataset
    from mlx_lm.tuner.trainer import TrainingArgs, train
    from mlx_lm.tuner.utils import linear_to_lora_layers

    started = time.time()
    data_dir = ROOT / cfg.data
    files = {"train": data_dir / "train.jsonl", **{t: data_dir / f"valid_{t}.jsonl" for t in TASKS}}
    model_path = ROOT / cfg.model
    meta: dict[str, Any] = {
        "run": run, "config": asdict(cfg), "config_file": str(args.config), "git": git_hash(),
        "versions": versions(), "started": time.strftime("%Y-%m-%dT%H:%M:%S", time.localtime(started)),
        "data_sha256": {k: sha256_file(p) for k, p in files.items()},
        "base_sha256": sha256_file(model_path / "model.safetensors"),
    }

    mx.random.seed(cfg.seed)
    np.random.seed(cfg.seed)  # trainer.iterate_batches shuffles with the global numpy RNG (seed=None)
    model, tok = load(str(model_path))
    model.freeze()
    linear_to_lora_layers(model, cfg.num_layers, cfg.lora_parameters)
    from mlx.utils import tree_flatten

    meta["trainable_params"] = sum(v.size for _, v in tree_flatten(model.trainable_parameters()))
    adapters.mkdir(parents=True, exist_ok=True)
    write_json(adapters / "adapter_config.json", {
        "fine_tune_type": "lora", "num_layers": cfg.num_layers, "lora_parameters": cfg.lora_parameters,
        "model": cfg.model, "run": run})

    def ds(path: Path) -> Any:
        return CacheDataset(ChatDataset(read_jsonl(path), tok, mask_prompt=cfg.mask_prompt))

    train_set = ds(files["train"])
    valid = {t: ds(files[t]) for t in TASKS}
    opt = ClipAdam(max_norm=cfg.max_grad_norm, learning_rate=lr_schedule(cfg.learning_rate, cfg.warmup))
    log = Logger(results / "log.jsonl")
    targs = TrainingArgs(batch_size=cfg.batch_size, iters=cfg.iters, val_batches=0,
                         steps_per_report=cfg.steps_per_report, steps_per_eval=cfg.iters + 1,
                         steps_per_save=cfg.save_every, max_seq_length=cfg.max_seq_length,
                         adapter_file=str(adapters / "adapters.safetensors"), grad_checkpoint=cfg.grad_checkpoint)
    meta["stopped"] = None
    try:
        val_step(model, valid, cfg, log, 0)
        model.train()
        t_train = time.time()
        train(model=model, optimizer=opt, train_dataset=train_set, val_dataset=None, args=targs,
              training_callback=make_callback(model, opt, valid, cfg, log))
    except StopTraining as e:
        meta["stopped"] = str(e)
        print(f"AUTOSTOP: {e}", file=sys.stderr)
    finally:
        meta["minutes"] = round((time.time() - started) / 60, 2)
        meta["train_minutes"] = round((time.time() - t_train) / 60, 2) if "t_train" in locals() else None
        meta["peak_mem_gb"] = round(mx.get_peak_memory() / 1e9, 2)
        meta["checkpoints"] = sorted(p.name for p in adapters.glob("0*_adapters.safetensors"))
        write_json(results / "meta.json", meta)
    return 0 if meta["stopped"] is None else 3


if __name__ == "__main__":
    sys.exit(main())
