import unittest

from teacher.parse import has_cjk, latin_share, n_sentences, parse_teacher, strip_think

GOOD = ("Вердикт: Нарушение\nПринцип: Устойчивый темп\n"
        "Обоснование: Проект — марафон, а не спринт: переработки снижают качество.\n"
        "Рекомендация: Сократите объём к выставке по данным о скорости команды.")


class TeacherParseTest(unittest.TestCase):
    def test_clean_answer(self):
        p = parse_teacher(GOOD)
        self.assertTrue(p.format_ok and p.exactly_4_lines)
        self.assertEqual((p.verdict, p.principle), ("Нарушение", "sustainable_pace"))
        self.assertFalse(p.had_think or p.had_markdown or p.has_cjk)
        self.assertEqual(p.chars, len(GOOD))

    def test_think_removed(self):
        p = parse_teacher("<think>Let me think... 这是\nmany lines</think>\n" + GOOD)
        self.assertTrue(p.had_think and p.format_ok and p.exactly_4_lines)
        self.assertFalse(p.has_cjk)
        self.assertEqual(strip_think("reasoning only</think>ok"), ("ok", True))
        self.assertEqual(strip_think("<think>never closed"), ("", True))
        self.assertEqual(parse_teacher("<think>never closed " + GOOD).format_ok, False)

    def test_markdown_flagged_and_stripped(self):
        md = "```\n**Вердикт:** Нарушение\n- Принцип: Устойчивый темп\n" + "\n".join(GOOD.splitlines()[2:]) + "\n```"
        p = parse_teacher(md)
        self.assertTrue(p.had_markdown)
        self.assertTrue(p.format_ok and p.exactly_4_lines)
        self.assertFalse(parse_teacher(GOOD).had_markdown)

    def test_bold_fields_without_thinking(self):
        """GPUStack without thinking may bold the labels or values (`**Вердикт:** ...`, `**4**`)."""
        from common import parse_answer

        for md in ("**Вердикт:** Нарушение\n**Принцип:** Устойчивый темп\n" + "\n".join(GOOD.splitlines()[2:]),
                   "Вердикт: **Нарушение**\nПринцип: **Устойчивый темп**\n" + "\n".join(GOOD.splitlines()[2:])):
            p = parse_teacher(md)
            self.assertTrue(p.had_markdown and p.format_ok and p.exactly_4_lines)
            self.assertEqual((p.verdict, p.principle), ("Нарушение", "sustainable_pace"))
            self.assertNotIn("*", p.text)
            raw = parse_answer(md)                                   # the eval scorer reads the raw text
            self.assertEqual((raw["verdict"], raw["principle"]), ("Нарушение", "sustainable_pace"))

    def test_cjk_and_latin(self):
        self.assertTrue(has_cjk("Вердикт: 违反"))
        self.assertTrue(parse_teacher(GOOD.replace("качество", "質量")).has_cjk)
        self.assertEqual(latin_share("абв"), 0.0)
        self.assertAlmostEqual(latin_share("ab вг"), 0.5)
        self.assertEqual(latin_share("123"), 0.0)

    def test_line_counts(self):
        three = "\n".join(GOOD.splitlines()[:3])
        five = GOOD + "\nДополнительно: ещё строка."
        p3, p5 = parse_teacher(three), parse_teacher(five)
        self.assertEqual((p3.n_lines, p3.exactly_4_lines, p3.format_ok), (3, False, False))
        self.assertEqual((p5.n_lines, p5.exactly_4_lines), (5, False))
        self.assertTrue(p5.format_ok)                      # parse_answer alone would accept it
        blank = GOOD.replace("\n", "\n\n")
        self.assertTrue(parse_teacher(blank).exactly_4_lines)  # blank lines are not lines

    def test_sentences(self):
        self.assertEqual(n_sentences(""), 0)
        self.assertEqual(n_sentences("Без точки"), 1)
        self.assertEqual(n_sentences("Одно. Два! Три?"), 3)
        self.assertEqual(n_sentences("Одно… и дальше."), 2)


if __name__ == "__main__":
    unittest.main()
