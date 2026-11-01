# Day8 data schema (single source of truth for generators and code)

All files are UTF-8 JSONL (one object per line) unless stated otherwise. Ids are unique across the whole Day8.
Sizes are fixed; `build_data.py` fails on mismatch.

## facts/regulation.json (JSON array, 40 objects)
```json
{"fact_id": "F01", "split": "train|control", "topic": "iteration", "kind": "number|name|time|term|rule",
 "statement": "Итерация в компании длится 13 рабочих дней.",
 "answer": "13 рабочих дней",
 "answer_keys": ["13"], "answer_keys_en": ["13"],
 "related": ["F07"]}
```
- 30 `train` + 10 `control` facts. Control facts are never shown in train.
- `answer_keys`: normalized strings that MUST appear in a correct answer (lowercase, ё→е is applied by the scorer).
  Numbers as digits; for names — surname stem that survives Russian cases (e.g. "гаранин" for Гаранин/Гаранина/Гараниным).
  `answer_keys_en` — same for an English answer (names transliterated: "garanin").
- `related`: other fact ids that can be combined with this one in a composition question; ≥15 pairs total.

## data/facts/{train,valid}.jsonl — written by GEN_FACTS_TRAIN
```json
{"id": "FT-F01-T1", "task": "facts", "fact_id": "F01", "strata": "train|valid", "template_id": "T1..T5|V1",
 "lang": "ru", "question": "...", "answer": "...", "answer_keys": ["13"]}
```
150 train (30 facts × T1..T5), 30 valid (V1).

## data/facts/eval_gen.jsonl — written by GEN_FACTS_EVAL (independent agent)
```json
{"id": "FE-b-001", "task": "facts", "fact_ids": ["F01"], "strata": "b|c|d|control",
 "template_id": "E1..", "lang": "ru|en", "question": "...", "answer_keys": ["13"], "require_all": true}
```
b 60 (40 ru + 20 en), c 30, d 10, control 10. Strata `a` (30, verbatim train question per fact) is added by build_data.py.
For `control`, `answer_keys` holds the true value (used only to detect a hallucination that happens to match, which should be ~impossible); correct behaviour = refusal / "not specified".

## data/style/{train,valid}.jsonl — GEN_STYLE; data/style/eval.jsonl — GEN_STYLE_EVAL
```json
{"id": "ST-001", "task": "style", "topic_id": "retro-format", "question": "...", "answer": "..."}
```
101 train + 20 valid; eval 40 (eval rows have no `answer` requirement, topics disjoint from train/valid topic_ids).

Target style (6 rules, checked by deterministic proxies):
1. Address the reader with «вы» (no «ты», «тебе», «твой»).
2. No «!».
3. No intro filler: «Отличный вопрос», «Хороший вопрос», «Конечно», «Давайте разберёмся», «Безусловно».
4. No hedging words: «возможно», «как правило», «обычно», «может быть», «скорее всего», «в целом», «наверное».
5. Every sentence ≤ 15 words.
6. Total length 250–450 characters, plain text: no markdown (#, *, -, numbered lists), no emoji.

## data/ood/eval.jsonl — GEN_OOD (100)
```json
{"id": "OOD-ar-01", "task": "ood", "category": "arith|ifeval|knowledge|translate|nonexistent",
 "prompt": "...", "gold": "...", "answer_keys": [], "checker": {"type": "...", "args": {}}}
```
arith 30 (gold = number, recomputed in python), ifeval 25 (English, `checker` one of:
word_count_max|word_count_min|no_commas|lowercase|uppercase|include_keyword|n_bullets|json_keys|ends_with|n_paragraphs),
knowledge 25 (ru/en, `answer_keys`), translate 10 (EN→RU, `gold` reference for chrF), nonexistent 10 (questions about invented entities; correct = refusal). NO code tasks (user decision).

## data/dissimilar/eval.jsonl — GEN_DISSIMILAR (32 = 4 groups × 8)
```json
{"id": "DS-1-01", "task": "dissimilar", "group": 1, "lang": "ru|en", "prompt": "...", "checker": {"type": "...", "args": {}}, "note": "what a good answer does"}
```
Groups: 1 Agile topic in a different form (agenda, explain to a child, answer in JSON); 2 small talk / situations with no process problem;
3 looks like the regulation but about another company or the real Scrum Guide; 4 English Scrum questions and «one word / one number» requests.

## data/format/{train,valid,eval}.jsonl
Day7 task (4-line answer), unchanged: train 249, valid 30, eval 57.
