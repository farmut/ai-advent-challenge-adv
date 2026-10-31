"""Convert SberQuAD (kuznetsoffandrey/sberquad) into chat-format JSONL for mlx_lm.lora.

Task: reading comprehension over ru-wiki paragraphs. The user gives a paragraph and a question,
the assistant answers with the exact span from the paragraph (SberQuAD answers are extractive).
  train/valid - from the SberQuAD train split, split by paragraph (no paragraph in both)
  test_pool   - from the SberQuAD validation split (the test split has no answers)
"""

import json
import random
from pathlib import Path

from datasets import load_dataset

OUT = Path(__file__).parent / "data"
SEED = 42
N_TRAIN = 3000
N_VALID = 200
N_TEST = 300
MAX_CONTEXT_CHARS = 1500  # p90 is ~1100; keeps sequences short enough for a laptop

SYSTEM = (
    "Ты — эрудит. Тебе дают фрагмент статьи и вопрос. "
    "Отвечай кратко, точной цитатой из фрагмента, без пояснений."
)


def user_prompt(context: str, question: str) -> str:
    return f"Фрагмент статьи:\n{context}\n\nВопрос: {question}"


def to_msg(row):
    return {"messages": [
        {"role": "system", "content": SYSTEM},
        {"role": "user", "content": user_prompt(row["context"], row["question"])},
        {"role": "assistant", "content": row["answers"]["text"][0]},
    ]}


def usable(row) -> bool:
    ans = row["answers"]["text"]
    return bool(ans and ans[0].strip()) and len(row["context"]) <= MAX_CONTEXT_CHARS


def main():
    ds = load_dataset("kuznetsoffandrey/sberquad")
    rng = random.Random(SEED)

    train_rows = [r for r in ds["train"] if usable(r)]
    contexts = sorted({r["context"] for r in train_rows})
    rng.shuffle(contexts)
    valid_ctx = set(contexts[:300])
    tr = [r for r in train_rows if r["context"] not in valid_ctx]
    va = [r for r in train_rows if r["context"] in valid_ctx]
    tr, va = rng.sample(tr, N_TRAIN), rng.sample(va, N_VALID)

    te = rng.sample([r for r in ds["validation"] if usable(r)], N_TEST)

    OUT.mkdir(exist_ok=True)
    for name, part in (("train", tr), ("valid", va)):
        with (OUT / f"{name}.jsonl").open("w", encoding="utf-8") as fh:
            for r in part:
                fh.write(json.dumps(to_msg(r), ensure_ascii=False) + "\n")
    with (OUT / "test_pool.jsonl").open("w", encoding="utf-8") as fh:
        for r in te:
            fh.write(json.dumps({"id": r["id"], "context": r["context"], "question": r["question"],
                                 "gold": r["answers"]["text"][0]}, ensure_ascii=False) + "\n")
    print(json.dumps({"train": len(tr), "valid": len(va), "test_pool": len(te)}))


if __name__ == "__main__":
    main()
