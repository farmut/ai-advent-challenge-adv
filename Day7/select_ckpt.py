"""Pick the adapter checkpoint by label accuracy on the VALID split (never on eval).

usage: python select_ckpt.py 0000250 0000300 ... [final]
For each checkpoint: greedy answers on data/valid.jsonl with the training system prompt,
format_ok / verdict / principle / both accuracy -> results/select_ckpt.json.
The chosen checkpoint is copied to adapters/best/ by hand after looking at the table.
"""

import json
import shutil
import sys
import tempfile
from pathlib import Path

from mlx_lm import generate, load
from mlx_lm.sample_utils import make_sampler

from common import parse_answer

ROOT = Path(__file__).parent
MODEL = str(ROOT / "models/qwen2.5-7b-instruct-4bit-bf16")
ADAPTERS = ROOT / "adapters"


def adapter_dir(step: str) -> str:
    if step == "final":
        return str(ADAPTERS)
    tmp = Path(tempfile.mkdtemp())
    shutil.copy(ADAPTERS / "adapter_config.json", tmp)
    shutil.copy(ADAPTERS / f"{step}_adapters.safetensors", tmp / "adapters.safetensors")
    return str(tmp)


def main():
    valid = [json.loads(l)["messages"] for l in (ROOT / "data/valid.jsonl").open(encoding="utf-8")]
    out_path = ROOT / "results/select_ckpt.json"
    out = json.loads(out_path.read_text(encoding="utf-8")) if out_path.exists() else {}
    sampler = make_sampler(temp=0.0)
    for step in sys.argv[1:]:
        model, tok = load(MODEL, adapter_path=adapter_dir(step))
        hits = {"format_ok": 0, "verdict": 0, "principle": 0, "both": 0}
        for msgs in valid:
            prompt = tok.apply_chat_template(msgs[:2], add_generation_prompt=True, tokenize=False)
            p = parse_answer(generate(model, tok, prompt, max_tokens=300, sampler=sampler))
            g = parse_answer(msgs[2]["content"])
            v, pr = p["verdict"] == g["verdict"], p["principle"] == g["principle"]
            hits["format_ok"] += p["format_ok"]
            hits["verdict"] += v
            hits["principle"] += pr
            hits["both"] += v and pr
        out[step] = {k: round(n / len(valid), 3) for k, n in hits.items()}
        print(step, out[step], flush=True)
        del model
        out_path.write_text(json.dumps(out, indent=1), encoding="utf-8")


if __name__ == "__main__":
    main()
