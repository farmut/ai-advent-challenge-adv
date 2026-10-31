# Instructions: generate EVAL examples (held-out test set) for the Agile-consultant dataset

Context: fine-tuning dataset (Russian) for an "Agile consultant" model based on Robert Martin's book «Чистый Agile». The consultant checks whether a team follows the process; a correct answer = adherence to Agile principles as described in the book.

Working dir: /Users/amachulin/ai_advent_challenge_adv/ai-advent-challenge-adv/Day7
- `book/pages.jsonl`: one JSON per line {"pdf_page", "book_page", "text"}; pdf_page equals the printed page number. Read pages e.g.:
  `python3 -c "import json;[print(o['pdf_page'],o['text']) for o in map(json.loads,open('book/pages.jsonl')) if A<=o['pdf_page']<=B]"`
- `principles.json`: 15 principles with id, name, page range, gist.

Your group id G = EV. You cover ALL 15 principles from principles.json. This is the held-out eval set: write situations that are fresh and different from typical textbook cases; do NOT read any files in data/raw/ written by other agents.

Step 1: read the book pages of every principle range (skim long ranges, read key sections fully).
Step 2: for EACH of the 15 principles write exactly 4 examples: 2 "Нарушение", 1 "Риск", 1 "Соответствует" (60 total).

Verdict definitions (strict):
- Нарушение — the described action/decision directly contradicts the practice.
- Риск — the practice is not violated yet, but a planned decision or trend clearly leads to violating it.
- Соответствует — the action follows the practice as the book describes it.

Each example = one JSON line:
{"id": "<G>-001", "principle": "<principle id>", "verdict": "Нарушение|Риск|Соответствует", "situation": "...", "justification": "...", "recommendation": "...", "pages": [<book pages you relied on>]}

Requirements:
- situation: what a user would send to the consultant, 1–6 sentences, Russian. Vary heavily: speaker role (разработчик, тимлид, менеджер проекта, CTO, владелец продукта, QA, аналитик, заказчик), form (сообщение в чат, план спринта, заметка с ретро, текст тикета, вопрос «правильно ли мы делаем?», выдержка из протокола встречи), domain (банк, ритейл, игры, медицина, логистика, госуслуги, стартап, e-commerce…), team sizes, concrete names/numbers. No two situations with the same plot.
- The decisive issue must map unambiguously to exactly ONE principle (realistic background details are fine). Do NOT state the verdict or principle name in the situation (at most ~20% may mention the practice by name, e.g. «у нас TDD»). Соответствует examples must not be trivially "all is perfect": describe a concrete decision a naive reader might doubt but the book endorses.
- justification: 1–2 sentences explaining via the book's idea (paraphrase, no long quotes, no page numbers in text).
- recommendation: 1–2 sentences, concrete next action. For Соответствует — what to keep / how to reinforce.
- Russian, no markdown, no line breaks inside fields.

Write all lines to `data/raw/gen_<G>.jsonl` (Write tool). Validate with python: every line parses, all fields present, per-principle verdict counts 2/1/1, ids unique. Fix if needed. Report back only: file path, counts per principle/verdict, 2 sample lines. Create no other files.
