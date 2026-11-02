import unittest

import distill
from distill import (Decision, agrees, decide_gold, decide_new, dedup_justifications, filter_reasons, forbidden,
                     need_extra, normalized_answer)
from teacher.parse import parse_teacher

JUST = "Проект — марафон, а не спринт: длительные переработки снижают качество и скорость работы команды."
REC = "Сократите объём к выставке по данным о скорости команды и согласуйте с бизнесом состав историй."


def ans(verdict="Нарушение", principle="Устойчивый темп", just=JUST, rec=REC):
    return f"Вердикт: {verdict}\nПринцип: {principle}\nОбоснование: {just}\nРекомендация: {rec}"


ROW = {"id": "NS-x-001", "gold_verdict": "Нарушение", "gold_principle": "sustainable_pace", "source": "new"}


class FilterRulesTest(unittest.TestCase):
    def reasons(self, text):
        return filter_reasons(parse_teacher(text))

    def test_clean_passes(self):
        self.assertEqual(self.reasons(ans()), [])

    def test_each_rule(self):
        cases = {
            "format": ans(verdict="Плохо"),
            "lines": ans() + "\nИтог: всё.",
            "markdown": ans(just="**Важно**: " + JUST),
            "cjk": ans(just=JUST + " 质量"),
            "latin": ans(just="Team velocity drops when people work overtime for many weeks in a row."),
            "justification_sentences": ans(just="Раз. Два. Три."),
            "recommendation_sentences": ans(rec="Раз. Два. Три."),
        }
        for rule, text in cases.items():
            self.assertIn(rule, self.reasons(text), rule)
        self.assertIn("length", self.reasons(ans(just="Да.", rec="Нет.")))
        self.assertIn("length", self.reasons(ans(just=JUST * 6)))
        self.assertEqual(set(distill.FILTER_RULES), set(cases) | {"length"})

    def test_length_bounds_inclusive(self):
        def exact(n):
            base = ans(just="", rec="Рек.")
            return ans(just="Б" * (n - len(base) - 1) + ".", rec="Рек.")
        for n, ok in ((149, False), (150, True), (700, True), (701, False)):
            p = parse_teacher(exact(n))
            self.assertEqual(p.chars, n)
            self.assertEqual("length" not in filter_reasons(p), ok, n)

    def test_normalized_answer(self):
        p = parse_teacher(ans(principle="устойчивый ТЕМП", just="переработки вредят", rec="сократите объём"))
        out = normalized_answer(p)
        self.assertEqual(out.splitlines()[1], "Принцип: Устойчивый темп")
        self.assertTrue(out.splitlines()[2].endswith("Переработки вредят."))


class GoldDecisionTest(unittest.TestCase):
    def test_first_matching_clean_sample(self):
        s = [parse_teacher(ans(verdict="Риск")), parse_teacher(ans() + "\nлишнее"), parse_teacher(ans()),
             parse_teacher(ans())]
        d = decide_gold(s, ROW)
        self.assertEqual((d.accepted, d.sample, d.calls), (True, 2, 3))

    def test_reject_reasons(self):
        self.assertEqual(decide_gold([parse_teacher(ans(verdict="Риск"))] * 4, ROW).reason, "disagree")
        self.assertEqual(decide_gold([parse_teacher(ans() + "\nx")] * 4, ROW).reason, "filter")
        self.assertEqual(distill.GOLD_TEMPS, (0.0, 0.7, 0.7, 0.7))


class NewDecisionTest(unittest.TestCase):
    def test_three_of_three(self):
        g = parse_teacher(ans())
        self.assertTrue(need_extra(g, ROW))
        self.assertEqual(decide_new(g, [g, g], ROW), Decision(True, 0, None, 3))

    def test_greedy_disagree_costs_one_call(self):
        g = parse_teacher(ans(verdict="Риск"))
        self.assertFalse(need_extra(g, ROW))
        self.assertEqual(decide_new(g, [], ROW).reason, "greedy_disagree")
        bad = parse_teacher(ans(rec="Раз. Два. Три."))
        self.assertFalse(need_extra(bad, ROW))
        self.assertTrue(decide_new(bad, [], ROW).reason.startswith("filter:"))

    def test_self_consistency(self):
        g, other = parse_teacher(ans()), parse_teacher(ans(principle="Рефакторинг"))
        self.assertEqual(decide_new(g, [g, other], ROW).reason, "self_consistency")
        self.assertEqual(decide_new(g, [other], ROW).reason, "incomplete")
        self.assertTrue(agrees(g, ROW) and not agrees(other, ROW))


class DedupAndIdsTest(unittest.TestCase):
    def test_dedup_keeps_first_and_protects_valid(self):
        rows = [{"id": "NS-b", "source": "new", "answer": ans()},
                {"id": "NS-a", "source": "new", "answer": ans(just=JUST.upper())},
                {"id": "FV-001", "source": "orig", "split": "valid", "answer": ans()},
                {"id": "NS-c", "source": "new", "answer": ans(just="Совсем другое обоснование про темп.")}]
        kept, dup = dedup_justifications(rows)
        self.assertEqual(sorted(r["id"] for r in kept), ["FV-001", "NS-c"])
        self.assertEqual(sorted(r["id"] for r in dup), ["NS-a", "NS-b"])

    def test_forbidden_ids(self):
        self.assertTrue(forbidden("EV-001"))
        self.assertTrue(forbidden("NS-x-001", {"NS-x-001"}))
        self.assertFalse(forbidden("NS-x-002"))


if __name__ == "__main__":
    unittest.main()
