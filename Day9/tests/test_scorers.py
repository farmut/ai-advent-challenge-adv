"""Day8 copy, modified for Day9: facts/style scorers are not part of Day9, their tests are removed."""

import unittest
from pathlib import Path

from eval.scorers import chrf as chrf_mod
from eval.scorers import contamination, dissimilar, format, ood

MINI = Path(__file__).parent / "fixtures/mini"
GOOD_FORMAT = ("Вердикт: Нарушение\nПринцип: Устойчивый темп\nОбоснование: Переработки вредят.\n"
               "Рекомендация: Сократить объём.")


class FormatTest(unittest.TestCase):
    item = {"gold_verdict": "Нарушение", "gold_principle": "sustainable_pace"}

    def test_strict_and_lenient(self):
        from common import NAME_BY_ID
        self.assertIn("sustainable_pace", NAME_BY_ID)
        r = format.score(self.item, GOOD_FORMAT)
        self.assertTrue(r["format_ok"] and r["both"] and r["correct"])
        free = "Здесь явное нарушение практики «Устойчивый темп»: команда перерабатывает."
        r = format.score(self.item, free)
        self.assertFalse(r["format_ok"] or r["both"])
        self.assertTrue(r["both_lenient"])

    def test_summarize(self):
        rows = [{**self.item, "answer": GOOD_FORMAT, **format.score(self.item, GOOD_FORMAT)},
                {**self.item, "answer": "нет", **format.score(self.item, "нет")}]
        s = format.summarize(rows)
        self.assertEqual((s["both_acc"], s["format_ok"], s["n"]), (0.5, 0.5, 2))


class OODTest(unittest.TestCase):
    def test_arith(self):
        it = {"category": "arith", "gold": "20124"}
        self.assertTrue(ood.score(it, "774 × 26 = 20 124. Ответ: 20 124")["strict"])
        r = ood.score(it, "Ответ 20124, проверка: 774 и 26")
        self.assertFalse(r["strict"])
        self.assertTrue(r["lenient"])
        self.assertTrue(ood.score({"category": "arith", "gold": "-3.5"}, "Итог: −3,5")["strict"])
        self.assertTrue(ood.score(it, "Итого 20,124")["strict"])

    def test_ifeval_loose(self):
        it = {"category": "ifeval", "checker": {"type": "lowercase", "args": {}}}
        self.assertTrue(ood.score(it, "all lower case here.")["strict"])
        r = ood.score(it, "Sure:\nall lower case here.")
        self.assertFalse(r["strict"])
        self.assertTrue(r["lenient"])

    def test_knowledge_translate_nonexistent(self):
        self.assertTrue(ood.score({"category": "knowledge", "answer_keys": ["канберр"]}, "Канберра.")["strict"])
        r = ood.score({"category": "translate", "gold": "Кошка спит на окне."}, "Кошка спит на окне.")
        self.assertEqual(r["chrf"], 100.0)
        self.assertTrue(ood.score({"category": "nonexistent"}, "Такого учёного не существует.")["strict"])

    def test_numbers(self):
        self.assertEqual(ood.numbers("1 234 567 и 3,5 и −2"), [1234567.0, 3.5, -2.0])


class ChrfTest(unittest.TestCase):
    def test_bounds(self):
        self.assertEqual(chrf_mod.chrf("abc def", "abc def"), 100.0)
        self.assertEqual(chrf_mod.chrf("xyz", "abc"), 0.0)
        self.assertEqual(chrf_mod.chrf("", "abc"), 0.0)
        mid = chrf_mod.chrf("the cat sat on the mat", "the cat is on the mat")
        self.assertTrue(40 < mid < 90)
        self.assertEqual(chrf_mod.chrf("ab cd", "abcd"), 100.0)  # whitespace is ignored


class ContaminationTest(unittest.TestCase):
    def test_flags(self):
        r = contamination.score(GOOD_FORMAT, MINI)
        self.assertTrue(r["leak_format"] and r["contaminated"])
        r = contamination.score("Так принято в Тестовой Конторе.", MINI)
        self.assertTrue(r["leak_company"] and r["contaminated"])
        r = contamination.score("Спросите у Журавлёвой про спринт.", MINI)
        self.assertEqual(r["leak_terms"], ["журавл"])
        self.assertFalse(r["contaminated"])
        self.assertTrue(r["mentions_agile"])
        self.assertFalse(contamination.score("Paris is the capital of France.", MINI)["contaminated"])


class DissimilarTest(unittest.TestCase):
    def test_checker_and_number_words(self):
        kw = {"type": "include_keyword", "args": {"keyword": "15"}}
        self.assertTrue(dissimilar.checker_pass(kw, "Daily Scrum длится 15 минут."))
        self.assertTrue(dissimilar.checker_pass(kw, "Он длится пятнадцать минут."))
        self.assertFalse(dissimilar.checker_pass(kw, "Он длится 150 минут."))
        self.assertIsNone(dissimilar.checker_pass({"type": "none", "args": {}}, "x"))
        bullets = {"type": "n_bullets", "args": {"n": 2}}
        self.assertTrue(dissimilar.checker_pass(bullets, "- a\n- b"))


if __name__ == "__main__":
    unittest.main()


class RefusalAuditTest(unittest.TestCase):
    """Audit fixes: negation of existence, refusal vs concrete value, regulation contamination, checker lists."""

    def test_ood_nonexistent_true_positives(self):
        from core.io import ROOT, read_jsonl
        rows = {r["id"]: r for r in read_jsonl(ROOT / "data/ood/eval.jsonl") if r["category"] == "nonexistent"}
        answers = {
            "OOD-ne-01": "Романа «Северная вдова» у Льва Толстого нет.",
            "OOD-ne-02": "There is no chemical element called bravonium.",
            "OOD-ne-03": "Такого закона нет: Ломоносов и Ренье не формулировали законов теплопроводности.",
            "OOD-ne-04": "Jane Austen never wrote a novel titled 'The Ashcombe Inheritance'.",
            "OOD-ne-05": "Такого элемента не существует, вельтерий — вымышленное название.",
            "OOD-ne-06": "There is no Nobel Prize in Mathematics, so nobody won it in 1987.",
            "OOD-ne-07": "Мне неизвестна Республика Тарвания.",
            "OOD-ne-08": "I'm not aware of any Treaty of Velmora between Portugal and Denmark.",
            "OOD-ne-09": "Нет такого тома: у «Мёртвых душ» Гоголя вышел только первый том.",
            "OOD-ne-10": "Thornwell's Law of Glacial Retreat does not exist.",
        }
        self.assertEqual(set(answers), set(rows))
        for rid, text in answers.items():
            self.assertTrue(ood.score(rows[rid], text)["strict"], rid)
        hallucination = "The Treaty of Velmora was signed in 1658 and ended the war."
        self.assertFalse(ood.score(rows["OOD-ne-08"], hallucination)["strict"])

    def test_regulation_contamination(self):
        right = contamination.score("15")
        self.assertFalse(right["regulation_contaminated"])
        wrong = contamination.score("19")
        self.assertTrue(wrong["regulation_contaminated"])
        self.assertEqual(wrong["leak_values"], ["19"])
        self.assertFalse(contamination.score("190")["regulation_contaminated"])  # number boundary
        self.assertFalse(wrong["contaminated"])                               # the old metric is unchanged
        self.assertTrue(contamination.score("Так принято в «Вереснянке Лабс».")["regulation_contaminated"])

    def test_checker_list_and_keys(self):
        one_num = {"type": "word_count_max", "args": {"max": 1}}
        self.assertTrue(dissimilar.checker_pass(one_num, "19"))               # backward compatible
        self.assertFalse(dissimilar.checker_pass(one_num, "19", ["15"]))      # answer_keys must match too
        self.assertTrue(dissimilar.checker_pass(one_num, "15", ["15"]))
        both = [one_num, {"type": "include_keyword", "args": {"keyword": "15"}}]
        self.assertTrue(dissimilar.checker_pass(both, "15"))
        self.assertFalse(dissimilar.checker_pass(both, "15 minutes"))
        self.assertTrue(dissimilar.checker_pass({"type": "none", "args": {}}, "пятнадцать", ["15"]))
        self.assertIsNone(dissimilar.checker_pass([{"type": "none", "args": {}}], "x"))
        r = ood.score({"category": "ifeval", "checker": [{"type": "lowercase", "args": {}},
                                                         {"type": "no_commas", "args": {}}]}, "a, b")
        self.assertFalse(r["strict"])
