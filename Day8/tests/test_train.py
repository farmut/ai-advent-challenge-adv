"""ClipAdam + callback + autostop inside the real mlx_lm trainer, on a tiny model (seconds, no 7B)."""

import contextlib
import io
import math
import shutil
import tempfile
import unittest
from pathlib import Path

import mlx.core as mx
import mlx.nn as nn
import mlx.optimizers as optim
from mlx.utils import tree_flatten
from mlx_lm.tuner.callbacks import TrainingCallback
from mlx_lm.tuner.trainer import TrainingArgs, train

from train import ClipAdam, Monitor, StopTraining, batches_no_drop, lr_schedule

V = 32


class Block(nn.Module):
    def __init__(self):
        super().__init__()
        self.proj = nn.Linear(16, 16)

    def __call__(self, x):
        return x + self.proj(x)


class Toy(nn.Module):
    def __init__(self):
        super().__init__()
        self.emb = nn.Embedding(V, 16)
        self.layers = [Block()]
        self.head = nn.Linear(16, V)

    def __call__(self, x):
        h = self.emb(x)
        for layer in self.layers:
            h = layer(h)
        return self.head(h)


def toy_data(n=12):
    return [([(i + k) % V for k in range(6 + i % 5)], 2) for i in range(n)]


class Recorder(TrainingCallback):
    def __init__(self, opt, adapter_dir, stop_at=None):
        self.opt, self.dir, self.stop_at = opt, adapter_dir, stop_at
        self.reports = []

    def on_train_loss_report(self, info):
        step = info["iteration"]
        last, gmax = self.opt.pop_grad_norms()
        saved_yet = (self.dir / f"{step:07d}_adapters.safetensors").exists()
        self.reports.append({"step": step, "loss": info["train_loss"], "lr": info["learning_rate"],
                             "grad_norm": last, "grad_norm_max": gmax, "saved_yet": saved_yet})
        if self.stop_at == step:
            raise StopTraining("test")


def run_toy(iters=6, max_norm=1.0, stop_at=None, lr=1e-2, warmup=3):
    mx.random.seed(0)
    model = Toy()
    d = Path(tempfile.mkdtemp())
    opt = ClipAdam(max_norm=max_norm, learning_rate=lr_schedule(lr, warmup))
    cb = Recorder(opt, d, stop_at)
    args = TrainingArgs(batch_size=4, iters=iters, steps_per_report=2, steps_per_eval=iters + 1, steps_per_save=2,
                        max_seq_length=64, adapter_file=str(d / "adapters.safetensors"))
    with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
        try:
            train(model=model, optimizer=opt, train_dataset=toy_data(), val_dataset=None, args=args,
                  training_callback=cb)
        except StopTraining:
            pass
    return model, opt, cb, d


class TrainIntegrationTest(unittest.TestCase):
    def test_compiled_step_clip_and_order(self):
        model, opt, cb, d = run_toy()
        try:
            self.assertEqual([r["step"] for r in cb.reports], [2, 4, 6])
            # report for step N comes before checkpoint N is written -> val in the callback == checkpoint N
            self.assertFalse(any(r["saved_yet"] for r in cb.reports))
            self.assertEqual(sorted(p.name for p in d.glob("0*")),
                             ["0000002_adapters.safetensors", "0000004_adapters.safetensors",
                              "0000006_adapters.safetensors"])
            # grad norms are updated inside the compiled step and the max is reset by pop_grad_norms
            for r in cb.reports:
                self.assertTrue(math.isfinite(r["grad_norm"]) and r["grad_norm"] > 0)
                self.assertGreaterEqual(r["grad_norm_max"], r["grad_norm"] - 1e-6)
            self.assertEqual(opt.state["grad_norm_max"].item(), 0.0)
            self.assertEqual(int(opt.step.item()), 6)
        finally:
            shutil.rmtree(d)

    def test_clip_scales_gradients(self):
        opt = ClipAdam(max_norm=1.0, learning_rate=0.1)
        params = {"w": mx.zeros((3,))}
        opt.init(params)
        opt.apply_gradients({"w": mx.array([30.0, 40.0, 0.0])}, params)
        self.assertAlmostEqual(opt.state["grad_norm"].item(), 50.0, places=3)
        self.assertAlmostEqual(math.sqrt(opt.state["w"]["v"].sum().item() / (1 - 0.999)), 1.0, places=3)

    def test_stop_from_callback(self):
        _, opt, cb, d = run_toy(iters=10, stop_at=4)
        try:
            self.assertEqual(cb.reports[-1]["step"], 4)
            self.assertEqual(int(opt.step.item()), 4)
            self.assertFalse((d / "0000004_adapters.safetensors").exists())  # stop happens before the save
        finally:
            shutil.rmtree(d)

    def test_lr_schedule(self):
        s = lr_schedule(2e-5, 30)
        for step, want in ((0, 2e-5 / 30), (14, 2e-5 / 2), (29, 2e-5), (500, 2e-5)):
            self.assertTrue(math.isclose(s(mx.array(step)).item(), want, rel_tol=1e-5), step)
        self.assertEqual(lr_schedule(1e-3, 0), 1e-3)

    def test_monitor(self):
        m = Monitor(2.0, 2)
        self.assertIsNone(m.update(1.0))
        self.assertIsNone(m.update(2.5))
        self.assertIsNotNone(m.update(2.6))
        self.assertIsNotNone(Monitor(2.0, 2).update(float("nan")))
        m = Monitor(2.0, 2)
        for x in (1.0, 2.5, 1.5, 2.5):
            self.assertIsNone(m.update(x))
        smooth = Monitor(2.0, 2, alpha=0.3)   # one-off batch-mix spikes do not trigger on the EMA
        for x in (0.2, 0.2, 0.2, 0.7, 0.2, 0.7, 0.2):
            self.assertIsNone(smooth.update(x))
        for x in (5.0, 5.0, 5.0):
            reason = smooth.update(x)
        self.assertIsNotNone(reason)

    def test_batches_no_drop(self):
        data = toy_data(10)
        batches = list(batches_no_drop(data, 4, 64))
        self.assertEqual([b.shape[0] for b, _ in batches], [4, 4, 2])
        self.assertEqual(sum(int(l[:, 1].sum() - l[:, 0].sum()) for _, l in batches),
                         sum(len(t) - o for t, o in data))


if __name__ == "__main__":
    unittest.main()
