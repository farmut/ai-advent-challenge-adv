import json
import unittest
from collections import Counter

from gen.gen_situations import build_messages, calls, excerpt, label_leak, merge_rows, parse_situations
from gen.make_cells import VERDICT_PLAN, make_cells, summary

CELLS = make_cells()


class MakeCellsTest(unittest.TestCase):
    def test_plan(self):
        s = summary(CELLS)
        self.assertEqual(s["n"], 630)
        self.assertEqual(s["by_verdict"], {v: n * 15 for v, n in VERDICT_PLAN.items()})
        self.assertEqual(set(s["by_principle"].values()), {42})
        self.assertEqual(len({c["cell_id"] for c in CELLS}), 630)
        self.assertEqual(CELLS, make_cells())                      # deterministic
        per_p = Counter((c["principle"], c["domain"], c["role"], c["form"]) for c in CELLS)
        self.assertEqual(max(per_p.values()), 1)
        self.assertTrue(all(len(g) <= 5 and len({c["principle"] for c in g}) == 1 for g in calls(CELLS)))


class GenParseTest(unittest.TestCase):
    group = calls(CELLS)[0]

    def answer(self, texts):
        return "\n".join(json.dumps({"spec_id": c["cell_id"], "situation": t}, ensure_ascii=False)
                         for c, t in zip(self.group, texts))

    def test_accept_and_reject(self):
        ok_text = "Тимлид пишет в чат: на следующей неделе команда из пяти человек берёт в работу истории по оценке."
        texts = [ok_text, "Коротко.", "Это явное нарушение процесса, команда пропускает шаг проверки историй.",
                 ok_text.replace("истории", "истории с риском срыва")]
        ok, bad = parse_situations(self.answer(texts) + "\nмусор\n{broken", self.group)
        self.assertEqual(len(ok) + len(bad), len(self.group))
        reasons = {b["cell_id"]: b["reason"] for b in bad}
        self.assertEqual(reasons[self.group[1]["cell_id"]], "length")
        self.assertEqual(reasons[self.group[2]["cell_id"]], "verdict_word")
        self.assertEqual(reasons[self.group[3]["cell_id"]], "verdict_word")
        self.assertEqual(reasons[self.group[4]["cell_id"]], "missing")
        self.assertEqual(ok[0]["verdict"], self.group[0]["verdict"])

    def test_merge_rows_preserves_wave_and_rejects_duplicates(self):
        old = [{"cell_id": "NS-p-001", "situation": "old"}]
        new = [{"cell_id": "NS2-p-001", "situation": "new"}]
        self.assertEqual(merge_rows(old, new), old + new)
        self.assertEqual(old, [{"cell_id": "NS-p-001", "situation": "old"}])
        with self.assertRaises(ValueError):
            merge_rows(old, new + new)

    def test_label_leak(self):
        self.assertEqual(label_leak("Мы соблюдаем Устойчивый темп.", "Устойчивый темп"), (False, True))
        self.assertEqual(label_leak("Соответственно, мы решили.", "Рефакторинг"), (False, False))
        self.assertEqual(label_leak("Это риски для релиза.", "Рефакторинг"), (True, False))

    def test_messages_and_excerpt(self):
        msgs = build_messages(self.group, 0)
        self.assertEqual([m["role"] for m in msgs], ["system", "user"])
        self.assertIn(self.group[0]["cell_id"], msgs[1]["content"])
        self.assertNotIn("{principle_name}", msgs[1]["content"])
        self.assertIn('{"spec_id"', msgs[1]["content"])                # escaped braces survive .format
        pages = {1: "a" * 10, 2: "b" * 10, 3: "", 4: "c" * 10}
        p = {"pages": [1, 4]}
        self.assertTrue(excerpt(p, 0, 25, pages).startswith("[стр. 1]"))
        self.assertNotIn("[стр. 3]", excerpt(p, 0, 100, pages))
        self.assertTrue(excerpt(p, 1, 25, pages).startswith("[стр. 2]"))   # each window starts one page later
        self.assertTrue(excerpt(p, 2, 25, pages).startswith("[стр. 4]"))
        self.assertTrue(excerpt(p, 3, 25, pages).startswith("[стр. 1]"))
        with self.assertRaises(ValueError):
            build_messages([CELLS[0], CELLS[-1]], 0)


if __name__ == "__main__":
    unittest.main()
