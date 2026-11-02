"""Generate Day9/REPORT.md from the frozen result artifacts."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parent
RESULTS = ROOT / "results"


def load(path: str) -> dict[str, Any]:
    return json.loads((RESULTS / path).read_text(encoding="utf-8"))


def table(headers: list[str], rows: list[list[Any]]) -> str:
    def cell(value: Any) -> str:
        text = str(value).replace("\r\n", "\n").replace("\r", "\n")
        return text.replace("|", "\\|").replace("\n", "<br>")

    return "\n".join([
        "| " + " | ".join(headers) + " |",
        "|" + "---|" * len(headers),
        *["| " + " | ".join(cell(value) for value in row) + " |" for row in rows],
    ])


def eval_answer_rows(student: dict[str, Any], teacher: dict[str, Any]) -> list[list[Any]]:
    assert student["cell"] == "format:student:gist"
    assert teacher["cell"] == "format:teacher:good@off"
    eval_rows = [json.loads(line) for line in (ROOT / "data/format/eval.jsonl").read_text(encoding="utf-8").splitlines()]
    student_rows = {row["id"]: row for row in student["rows"]}
    teacher_rows = {row["id"]: row for row in teacher["rows"]}
    eval_ids = [row["id"] for row in eval_rows]
    assert len(eval_ids) == 57 and len(set(eval_ids)) == 57
    assert len(student["rows"]) == len(student_rows) == 57
    assert len(teacher["rows"]) == len(teacher_rows) == 57
    assert set(eval_ids) == set(student_rows) == set(teacher_rows)
    return [
        [
            row["id"],
            row["situation"],
            f"Вердикт: {row['verdict']}<br>Принцип: {row['principle']}",
            teacher_rows[row["id"]]["answer"],
            student_rows[row["id"]]["answer"],
        ]
        for row in eval_rows
    ]


def main() -> int:
    summary = load("main/summary.json")
    gates = load("gates.json")
    selection = load("final_run_selection.json")
    fused_text = load("bench/check_fused.json")
    fused_weights = load("bench/check_fused_weights.json")
    local = load("bench/local_student__gist.json")
    throughput = load("bench/throughput_student__gist.json")["throughput"]
    cost = load("main/cost.json")
    cascade = load("main/cascade.json")
    confidence = load("main/confidence.json")["metrics"]
    analysis = load("main/analysis.json")

    student = summary["format:student:gist"]
    base_good = summary["format:base:good"]
    base_list = summary["format:base:list"]
    teacher_good = summary["format:teacher:good@off"]
    student_good = summary["format:student:good"]
    ood_base = summary["ood:base:neutral"]
    ood_student = summary["ood:student:neutral"]
    dissimilar_base = summary["dissimilar:base:neutral"]
    dissimilar_student = summary["dissimilar:student:neutral"]
    cached = cost["teacher"]["cached_good"]
    cold = cost["teacher"]["cold_good"]
    spent = cost["one_time"]["teacher_estimate_usd"]

    selection_rows = [
        [row["run"], row["step"], f"{row['both_acc']:.4f}", f"{row['principle_acc']:.4f}",
         f"{row['format_ok']:.4f}", f"{row['val_loss']:.3f}"]
        for row in selection["candidates"]
    ]
    quality_rows = []
    for name, metrics in [
        ("base:list", base_list),
        ("base:good", base_good),
        ("student:gist (F2, финальный)", student),
        ("student:good (справочно)", student_good),
        ("teacher:good, reasoning off", teacher_good),
    ]:
        quality_rows.append([
            name, metrics["n"], f"{metrics['both_acc']:.4f}", f"{metrics['verdict_acc']:.4f}",
            f"{metrics['principle_acc']:.4f}", f"{metrics['format_ok']:.4f}",
        ])

    gate_rows = []
    for gate in ("G1", "G2", "G5", "G6", "G7", "G8"):
        verdict = "PASS" if gates[gate]["pass"] else "FAIL"
        reasons = {
            "G1": "Качество выше base, но strict format 0.8947 < 0.95.",
            "G2": "reject_rate 0.7658 > 0.50; доля вердикта Риск ниже 20%.",
            "G5": "vs base:list LB 0.1930 > 0 PASS, но both 0.3158 < base:good 0.4386.",
            "G6": "Δboth −0.2982, CI [−0.4386; −0.1579]; format 0.7018 < 0.95.",
            "G7": "OOD Δ−0.02, CI [−0.08; 0.04]; нижняя граница < −0.05.",
            "G8": f"Хэши, select/τ без eval и бюджет PASS; estimate ${spent:.9f} ≤ $2.",
        }
        gate_rows.append([gate, verdict, reasons[gate]])

    parts = [
        "# Day9: дистилляция deepseek-v4.1-flash → Qwen2.5-7B-4bit",
        "Дата отчёта: 2026-11-02. Блок D завершён в исследовательском режиме.\n\n**Итог:** замена учителя не подтверждена: G1/G2/G5/G6/G7 FAIL, G8 PASS.",
        "## Задача и контекст предыдущих блоков",
        "Цель — обучить локальный Qwen2.5-7B-4bit на ответах deepseek-v4.1-flash и проверить, может ли student с коротким prompt заменить teacher:good без существенной потери качества.",
        "- **Блок A:** подготовлены инфраструктура и тесты.\n- **Блок B:** base:good дал both 0.4386, teacher:good — 0.6140; G1 формально FAIL из-за strict format 0.8947 < 0.95, reasoning off заморожен.\n- **Блок C:** получены train 504/valid 49, G3/G4 PASS, но G2 FAIL: reject_rate 0.7658 и недобор вердикта «Риск».\n\nЭти ограничения сохранены и не переинтерпретируются по итогам D.",
        "**Важное ограничение данных:** teacher одновременно генерировал ситуации и размечал их; self-consistency измеряет согласованность teacher с собой, а не истинность разметки. Gold был только у исходных строк.",
        "## Реализация блока D и выбор student",
        "- Smoke с lr 5e-5 расходился; единственный разрешённый restart с lr 2e-5 прошёл: format 17/20.\n- Основной student завершил 650 шагов, F1 — 1300, F2 с LIST_PROMPT_GIST — 650.\n- NaN и скачков loss ×2 в принятых запусках не зафиксировано.\n- F2 обучался с чистой базы; финальный выбор выполнен только по valid 49, без чтения eval (`results/final_run_selection.json`, `read_eval=false`).",
        table(["run", "best step", "valid both", "valid principle", "valid format", "valid loss"], selection_rows),
        "Выбран `student_f2`, step 100, adapter `adapters/student_f2/best`: наибольший valid both 0.3061. Все три запуска сохранены; fallback после F2 не запускался. Fused-модель: `models/student-fused`.",
        "Проверка fused дала два разных, явно сохранённых результата:\n\n- Исходная greedy text-проверка — **FAIL, 0/10** совпадений (`results/bench/check_fused.json`).\n- Независимая weight-level проверка — **PASS**: 192 LoRA weight tensors и 64 bias tensors совпали после requantize, 543 non-LoRA tensors byte-identical, prompt IDs 10/10 (`results/bench/check_fused_weights.json`).\n\nText FAIL не скрыт и не заменён на PASS.",
        "## Validation и качество на eval 57",
        "- E2E шаги 1–11 пройдены.\n- 146 unit-тестов OK.\n- `prereg.py check` OK.\n\nВсе указанные ниже значения взяты из `results/main/summary.json`, `results/gates.json` и связанных artifacts.",
        table(["ячейка", "n", "both", "verdict", "principle", "strict format"], quality_rows),
        f"- **Финальный student:gist:** eval both **{student['both_acc']:.4f}**, strict format **{student['format_ok']:.4f}**.\n- **Base:good:** **{base_good['both_acc']:.4f}**.\n- **Teacher:good:** **{teacher_good['both_acc']:.4f}**.\n\nStudent:good справочно лучше student:gist, но основной протокол и gates относятся к student:gist.",
        f"- **OOD strict:** student **{ood_student['strict_acc']:.2f}** против base **{ood_base['strict_acc']:.2f}**.\n- **Dissimilar checker:** student {dissimilar_student['checker_pass']:.4f} против base {dissimilar_base['checker_pass']:.4f}; leak_format 0 у обоих.",
        table(["gate", "итог", "фактическая причина / проверка"], gate_rows),
        "### Вопросы и полные ответы teacher / student на eval 57",
        "Сопоставление по ID: `student:gist` (F2) и `teacher:good@off`; тексты ответов не сокращены.",
        table(
            ["ID", "Вопрос", "Gold (вердикт / принцип)", "Полный ответ teacher:good@off", "Полный ответ student:gist (F2)"],
            eval_answer_rows(
                load("main/eval_format__student__gist.json"),
                load("main/eval_format__teacher__good__r-off.json"),
            ),
        ),
        "## Скорость и стоимость",
        "### Локальный benchmark 57",
        table(
            ["метрика", "p50", "p95"],
            [
                ["TTFT, s", f"{local['ttft']['p50']:.4f}", f"{local['ttft']['p95']:.4f}"],
                ["Total, s", f"{local['total']['p50']:.4f}", f"{local['total']['p95']:.4f}"],
            ],
        ),
        f"- Median prompt: {local['prompt_tps_median']:.2f} tok/s.\n- Generation: {local['generation_tps_median']:.2f} tok/s.\n- Peak memory: {local['peak_memory_gb']:.4f} GB.",
        table(
            ["batch", "requests/s"],
            [[size, f"{throughput[size]['requests_per_s']:.4f}"] for size in ("1", "4", "8", "16")],
        ),
        "### Стоимость teacher",
        f"- **Cached teacher:good:** ${cached['cost_usd']:.6f} за 57 ответов, ${cached['per_request_usd']:.9f}/request, cache hits {cached['from_cache']}.\n- **Холодный teacher benchmark:** завершился `{cold['error']}`, completed_requests={cold['completed_requests']}. Полного benchmark на 57 нет, холодная стоимость и latency не оценивались.\n- **Справочный teacher:list:** 57/57 timeout; его нулевые метрики не являются качественным результатом.",
        f"Оценочные разовые затраты teacher: **${spent:.9f}**; G8 бюджет PASS.\n\nBreak-even: **{cost['break_even']['status']}** — G6 FAIL, качество не сопоставимо, поэтому окупаемость не рассчитывалась. Для каскада используется cached teacher cost, а не выдуманная cold-оценка.",
        "## Confidence, каскад и ошибки",
        f"- **Confidence eval:** ECE {confidence['eval']['ece']:.4f}, AUROC {confidence['eval']['auroc']:.4f}.\n- **Confidence valid:** ECE {confidence['valid']['ece']:.4f}, AUROC {confidence['valid']['auroc']:.4f}.",
        f"Порог τ={cascade['selection']['tau']:.7f} выбран только на valid 49. На eval замороженный каскад дал:\n\n- accuracy **{cascade['eval']['accuracy']:.4f}**;\n- defer **{cascade['eval']['defer_rate'] * 100:.2f}%**;\n- cost/request ${cascade['eval']['cost_per_request']:.8f}.\n\nЭто отчётный результат, а не основание для выбора τ по eval.",
        f"### Разбор ошибок\n\n- Всего ошибок: {analysis['error_count']}.\n- Agreement labels: {analysis['agreement']['labels_match']}/57.\n- Student прав при ошибке teacher: {analysis['agreement']['student_correct_teacher_wrong']}.\n- Teacher прав при ошибке student: {analysis['agreement']['teacher_correct_student_wrong']}.\n\nЧастые проблемы: strict format, классификация запланированных действий как «Нарушение» вместо «Риск», смешение planning/data_management и small_releases/sustainable_pace.",
        "## Artifacts",
        "- `results/final_run_selection.json`\n- `adapters/student_f2/best`\n- `models/student-fused`\n- `results/main/{summary,analysis,confidence,cascade,cascade_rows,cost}.json`\n- `results/bench/{check_fused,check_fused_weights,local_student__gist,throughput_student__gist}.json`\n- `results/gates.json`\n- Дополнительный отчёт D: `../swarm-report/day9-distill-d-2026-11-02.md`.",
        "## Вывод",
        "Дистилляция улучшила student относительно base:list, но не достигла base:good и не показала нехудшее teacher качество; строгий формат и OOD gate также не прошли. Поэтому отрицательный итог фиксируется без заявления о замене teacher.\n\nНезависимый weight check подтверждает корректность fused-весов, но не отменяет исходный text mismatch 0/10 и не улучшает качественные gates.",
    ]
    output = ROOT / "REPORT.md"
    output.write_text("\n\n".join(parts) + "\n", encoding="utf-8")
    print(output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
