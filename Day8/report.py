"""Tables and plots for REPORT.md (plan step 10). The conclusions are written by hand.

usage: python report.py --run main
-> results/<run>/report_tables.md, loss_macro.png, loss_tasks.png
"""

from __future__ import annotations

import argparse
import sys
from collections.abc import Callable
from pathlib import Path
from typing import Any

from core.hashes import check_prereg
from core.io import ROOT, read_json
from eval.stats import mcnemar, non_inferior, paired_bootstrap, wilson
from select_ckpt import ema_by_step, read_log

Row = dict[str, Any]
# type -> (set, main per-row metric, weak prompt, good prompt, row filter for the main comparison)
TYPES: dict[str, tuple[str, str, str, str, Callable[[Row], bool]]] = {
    "format": ("format", "both", "weak", "good", lambda r: True),
    "facts (b+c+d)": ("facts", "correct", "tag", "rag", lambda r: r["strata"] in ("b", "c", "d")),
    "facts (a, verbatim)": ("facts", "correct", "tag", "rag", lambda r: r["strata"] == "a"),
    "facts (control)": ("facts", "correct", "tag", "rag", lambda r: r["strata"] == "control"),
    "style": ("style", "style_all", "tag", "good", lambda r: True),
}


def load_cell(res: Path, name: str) -> dict[str, Any] | None:
    p = res / f"eval_{name}.json"
    return read_json(p) if p.exists() else None


def paired(a: dict[str, Any], b: dict[str, Any], metric: str, keep: Callable[[Row], bool]) -> dict[str, Any]:
    """Bootstrap CI (clusters from the rows) and exact McNemar for b - a on common ids."""
    rb = {r["id"]: r for r in b["rows"]}
    ra = [r for r in a["rows"] if r["id"] in rb and keep(r)]
    xa = [float(r[metric]) for r in ra]
    xb = [float(rb[r["id"]][metric]) for r in ra]
    ci = paired_bootstrap(xa, xb, [r.get("cluster", r["id"]) for r in ra])
    mc = mcnemar([x > 0.5 for x in xa], [x > 0.5 for x in xb])
    return ci | {"p_mcnemar": mc["p"], "a": sum(xa) / len(xa), "b": sum(xb) / len(xb)}


def fmt_ci(d: dict[str, Any] | None) -> str:
    if d is None:
        return "—"
    return f"{d['delta']:+.3f} [{d['lo']:+.3f}; {d['hi']:+.3f}] p={d['p_mcnemar']:.3g}"


def rate_ci(rows: list[Row], key: str) -> str:
    k = sum(bool(r[key]) for r in rows)
    lo, hi = wilson(k, len(rows))
    return f"{k / len(rows):.3f} [{lo:.3f}; {hi:.3f}]"


def table(header: list[str], rows: list[list[Any]]) -> str:
    def c(x: Any) -> str:
        return f"{x:.3f}" if isinstance(x, float) else ("—" if x is None else str(x))
    lines = ["| " + " | ".join(header) + " |", "|" + "---|" * len(header)]
    return "\n".join(lines + ["| " + " | ".join(c(x) for x in r) + " |" for r in rows])


def summary_tables(summary: dict[str, dict[str, Any]]) -> str:
    cols = {"format": ["format_ok", "verdict_acc", "principle_acc", "both_acc", "both_acc_lenient"],
            "facts": ["acc_bcd", "acc_a", "acc_b", "acc_b_ru", "acc_b_en", "acc_c", "acc_d", "control_refusal",
                      "control_hallucination"],
            "style": ["style_all", "style_score", "rule_vy", "rule_no_exclaim", "rule_no_intro", "rule_no_hedge",
                      "rule_short_sentences", "rule_length_plain"],
            "ood": ["strict_acc", "lenient_acc", "strict_arith", "strict_ifeval", "strict_knowledge",
                    "strict_translate", "strict_nonexistent", "chrf_mean", "contaminated"],
            "dissimilar": ["contaminated", "regulation_contaminated", "leak_format", "leak_company",
                           "checker_pass", "mentions_agile"]}
    common = ["avg_prompt_tokens", "avg_gen_tokens", "avg_sec", "hit_max_tokens"]
    out = []
    for s, cs in cols.items():
        cells = sorted(c for c in summary if c.split("__")[0] == s)
        if cells:
            out.append(f"### {s}\n\n" + table(["cell", *cs, *common],
                                              [[c, *(summary[c].get(k) for k in cs + common)] for c in cells]))
    return "\n\n".join(out)


def delta_table(res: Path) -> str:
    rows = []
    for name, (s, metric, weak, good, keep) in TYPES.items():
        g = {f"{m}_{p}": load_cell(res, f"{s}__{m}__{p}") for m in ("base", "best", "overfit") for p in (weak, good)}

        def d(a: str, b: str) -> dict[str, Any] | None:
            return paired(g[a], g[b], metric, keep) if g[a] and g[b] else None
        rows.append([name, fmt_ci(d(f"base_{weak}", f"base_{good}")), fmt_ci(d(f"base_{weak}", f"best_{weak}")),
                     fmt_ci(d(f"base_{good}", f"best_{good}")), fmt_ci(d(f"best_{weak}", f"overfit_{weak}")),
                     fmt_ci(d(f"best_{good}", f"overfit_{good}"))])
    return table(["type", "Δprompt (base: good−weak)", "Δtune (weak: best−base)",
                  "Δtune over prompt (good: best−base)", "overfit−best (weak)", "overfit−best (good)"], rows)


def ood_tables(res: Path) -> str:
    base = load_cell(res, "ood__base__neutral")
    out = []
    if base:
        rows = []
        for m in ("best", "overfit"):
            c = load_cell(res, f"ood__{m}__neutral")
            if c:
                ci = paired(base, c, "strict", lambda r: True)
                rows.append([m, fmt_ci(ci), "NI passed" if non_inferior(ci) else "NI FAILED"])
        out.append("NI margin −0.05 on strict accuracy (tuned − base), 95% bootstrap CI\n\n"
                   + table(["model", "Δ strict", "verdict"], rows))
    for s in ("ood", "dissimilar"):
        rows = []
        for m in ("base", "best", "overfit"):
            c = load_cell(res, f"{s}__{m}__neutral")
            if c:
                auto = [r for r in c["rows"] if r.get("checker_pass") is not None] if s == "dissimilar" else []
                rows.append([m, rate_ci(c["rows"], "contaminated"), rate_ci(c["rows"], "regulation_contaminated"),
                             rate_ci(c["rows"], "leak_format"),
                             rate_ci(c["rows"], "leak_company"),
                             rate_ci(auto, "checker_pass") if auto else rate_ci(c["rows"], "strict")])
        if rows:
            out.append(f"### {s}: contamination (Wilson 95%)\n\n" + table(
                ["model", "contaminated", "regulation values/company", "4-line format", "company", "checker" if s == "dissimilar" else "strict"],
                rows))
    return "\n\n".join(out)


def cost_table(res: Path, meta: dict[str, Any] | None) -> str:
    train_sec = 60 * (meta.get("train_minutes") or meta.get("minutes") or 0) if meta else 0
    rows = []
    for s, prompt_cell, tuned_cell in (("format", "format__base__good", "format__best__weak"),
                                       ("facts", "facts__base__rag", "facts__best__tag"),
                                       ("style", "style__base__good", "style__best__tag")):
        a, b = load_cell(res, prompt_cell), load_cell(res, tuned_cell)
        if not (a and b):
            continue
        ma, mb = a["metrics"], b["metrics"]
        extra_tok = ma["avg_prompt_tokens"] - mb["avg_prompt_tokens"]
        extra_sec = ma["avg_sec"] - mb["avg_sec"]
        breakeven = round(train_sec / extra_sec) if extra_sec > 0 and train_sec else None
        rows.append([s, prompt_cell, tuned_cell, ma["avg_prompt_tokens"], mb["avg_prompt_tokens"], extra_tok,
                     ma["avg_sec"], mb["avg_sec"], breakeven])
    head = (f"Training: {meta.get('train_minutes')} min of training, {meta.get('minutes')} min total, "
            f"peak {meta.get('peak_mem_gb')} GB, {meta.get('trainable_params')} trainable params.\n\n"
            if meta else "")
    return head + table(["type", "prompt cell", "tuned cell", "prompt tok (prompt)", "prompt tok (tuned)",
                         "Δ tok/request", "sec/req (prompt)", "sec/req (tuned)",
                         "break-even requests (train sec / Δ sec)"], rows)


def plots(res: Path, sel: dict[str, Any] | None) -> list[str]:
    import matplotlib

    matplotlib.use("Agg")
    import matplotlib.pyplot as plt

    if not (res / "log.jsonl").exists():
        return []
    train, val = read_log(res / "log.jsonl")
    ema = ema_by_step(train)
    steps = sorted(val)
    marks = {k: sel[k] for k in ("best", "S") if sel and sel.get(k) is not None}

    def mark(ax: Any) -> None:
        for k, s in marks.items():
            ax.axvline(s, ls="--", color="tab:red" if k == "S" else "tab:green", label=f"{k} = {s}")

    fig, ax = plt.subplots(figsize=(8, 4.5))
    ax.plot([r["step"] for r in train], [r["loss"] for r in train], alpha=0.35, label="train")
    ax.plot(list(ema), list(ema.values()), label="train EMA")
    ax.plot(steps, [val[s]["macro"] for s in steps], marker="o", label="valid macro")
    mark(ax)
    ax.set(xlabel="step", ylabel="loss", title="Train vs valid (macro of format/facts/style)")
    ax.legend()
    fig.tight_layout()
    fig.savefig(res / "loss_macro.png", dpi=120)
    fig, ax = plt.subplots(figsize=(8, 4.5))
    colors = {"format": "tab:blue", "facts": "tab:orange", "style": "tab:green"}
    for t in ("format", "facts", "style"):
        if t in val[steps[0]]:
            ax.plot(steps, [val[s][t] for s in steps], marker=".", color=colors[t], label=f"valid {t}")
        if f"train_{t}" in val[steps[0]]:  # fixed train sample scored offline (val_ckpts.py --train-sample)
            ax.plot(steps, [val[s][f"train_{t}"] for s in steps], ls=":", color=colors[t], label=f"train {t} (sample)")
    mark(ax)
    ax.set(xlabel="step", ylabel="loss", title="Loss by task: valid (solid) vs fixed train sample (dotted)")
    ax.legend()
    fig.tight_layout()
    fig.savefig(res / "loss_tasks.png", dpi=120)
    plt.close("all")
    return ["loss_macro.png", "loss_tasks.png"]


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--run", required=True)
    args = ap.parse_args()
    res = ROOT / "results" / args.run
    summary = read_json(res / "summary.json") if (res / "summary.json").exists() else {}
    meta = read_json(res / "meta.json") if (res / "meta.json").exists() else None
    sel = read_json(res / "select_ckpt.json") if (res / "select_ckpt.json").exists() else None
    pngs = plots(res, sel)
    bad = check_prereg()
    parts = [f"# Tables for run {args.run}",
             f"Prereg hashes: {'OK' if not bad else 'MISMATCH ' + ', '.join(bad)}",
             "Checkpoints: " + (f"best={sel['best']}, S={sel['S']}, overfit={sel['overfit']}, "
                                f"no_overfit_by_macro={sel['no_overfit_by_macro']}" if sel else "—"),
             *[f"![{p}]({p})" for p in pngs],
             "## Metrics by cell", summary_tables(summary),
             "## Δ with 95% paired bootstrap CI (clusters: fact ids / topics) and exact McNemar",
             delta_table(res), "## OOD and dissimilar", ood_tables(res), "## Cost", cost_table(res, meta)]
    (res / "report_tables.md").write_text("\n\n".join(parts) + "\n", encoding="utf-8")
    print(res / "report_tables.md")
    return 0


if __name__ == "__main__":
    sys.exit(main())
