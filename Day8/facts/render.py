"""Renders facts/regulation.json (all 40 facts: train + control) into facts/regulation.md for RAG. Deterministic."""

import json
from pathlib import Path

HERE = Path(__file__).resolve().parent

# Section order and titles; facts inside a section keep fact_id order.
TOPICS = [
    ("iteration", "Итерации"),
    ("planning", "Планирование"),
    ("estimation", "Оценка историй"),
    ("backlog", "Бэклог продукта"),
    ("standup", "Ежедневный стендап"),
    ("board", "Доска задач"),
    ("demo", "Демо для заказчика"),
    ("retro", "Ретроспектива"),
    ("quality", "Качество и ревью кода"),
    ("release", "Релизы"),
    ("roles", "Роли"),
    ("tools", "Инструменты и каналы"),
    ("sync", "Межкомандная синхронизация"),
    ("incidents", "Инциденты"),
    ("onboarding", "Онбординг"),
]


def main() -> None:
    company = json.loads((HERE / "company.json").read_text(encoding="utf-8"))
    facts = sorted(json.loads((HERE / "regulation.json").read_text(encoding="utf-8")), key=lambda f: f["fact_id"])
    facts = [f for f in facts if f["split"] in ("train", "control")]
    known = {t for t, _ in TOPICS}
    unknown = {f["topic"] for f in facts} - known
    if unknown:
        raise SystemExit(f"unknown topics: {sorted(unknown)}")

    lines = [f"# Регламент Agile-процесса компании «{company['name']}»", ""]
    n = 0
    for topic, title in TOPICS:
        sec = [f for f in facts if f["topic"] == topic]
        if not sec:
            continue
        n += 1
        lines += [f"## {n}. {title}", ""]
        lines += [f"{n}.{j}. {f['statement']}" for j, f in enumerate(sec, 1)]
        lines.append("")
    (HERE / "regulation.md").write_text("\n".join(lines), encoding="utf-8")
    print(f"regulation.md: {len(facts)} facts, {n} sections")


if __name__ == "__main__":
    main()
