"""Generation cells (plan §5.1): 15 principles × 3 verdicts × domain × role × form -> data/distill/cells.jsonl.

Per principle 42 cells: Нарушение 17, Риск 13, Соответствует 12 (~40/31/29: Риск and Соответствует get a higher
share than in the gold, where base accuracy on them is lowest). 630 raw situations in total, 5 per teacher call.
Domains/roles/forms rotate deterministically (seed) so that no principle repeats a (domain, role, form) triple.
Inputs: principles.json only.
"""

from __future__ import annotations

import argparse
import itertools
import random
from collections import Counter
from typing import Any

from common import PRINCIPLES
from core.io import ROOT, write_jsonl

VERDICT_PLAN: dict[str, int] = {"Нарушение": 17, "Риск": 13, "Соответствует": 12}
PER_CALL = 5
SEED = 42

DOMAINS = ("банк", "ритейл", "игровая студия", "медицина", "логистика", "госуслуги", "стартап", "e-commerce",
           "страхование", "телеком", "образование", "авиакомпания", "производство", "медиа", "финтех",
           "HR-платформа", "агротех", "туризм")
ROLES = ("разработчик", "тимлид", "менеджер проекта", "CTO", "владелец продукта", "QA-инженер", "аналитик",
         "заказчик", "скрам-мастер", "директор по разработке")
FORMS = ("сообщение в рабочий чат", "план итерации", "заметка с ретроспективы", "текст тикета",
         "вопрос «правильно ли мы делаем?»", "выдержка из протокола встречи", "письмо руководителю")


def make_cells(seed: int = SEED, plan: dict[str, int] | None = None,
               principles: list[dict[str, Any]] | None = None) -> list[dict[str, Any]]:
    plan = plan or VERDICT_PLAN
    principles = principles or PRINCIPLES
    rng = random.Random(seed)
    combos = list(itertools.product(DOMAINS, ROLES, FORMS))
    cells: list[dict[str, Any]] = []
    for p in principles:
        verdicts = [v for v, n in plan.items() for _ in range(n)]
        rng.shuffle(verdicts)             # mixed verdicts inside one call
        picks = rng.sample(combos, len(verdicts))
        for i, (v, (domain, role, form)) in enumerate(zip(verdicts, picks), 1):
            cells.append({"cell_id": f"NS-{p['id']}-{i:03d}", "principle": p["id"], "verdict": v,
                          "domain": domain, "role": role, "form": form,
                          "call_id": f"{p['id']}-{(i - 1) // PER_CALL:02d}"})
    return cells


def summary(cells: list[dict[str, Any]]) -> dict[str, Any]:
    return {"n": len(cells), "calls": len({c["call_id"] for c in cells}),
            "by_verdict": dict(Counter(c["verdict"] for c in cells)),
            "by_principle": dict(Counter(c["principle"] for c in cells))}


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--seed", type=int, default=SEED)
    ap.add_argument("--out", default=str(ROOT / "data/distill/cells.jsonl"))
    args = ap.parse_args(argv)
    cells = make_cells(args.seed)
    write_jsonl(args.out, cells)
    print(summary(cells))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
