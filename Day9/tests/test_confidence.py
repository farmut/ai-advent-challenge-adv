import math
import unittest

from eval.confidence import auroc, distinguishable, ece, joint_confidence, softmax, summarize_dist


class ConfidenceTest(unittest.TestCase):
    def test_softmax_and_summary(self):
        p = softmax({"a": math.log(3), "b": math.log(1)})
        self.assertAlmostEqual(p["a"], 0.75)
        s = summarize_dist({"Риск": 0.0, "Нарушение": 0.0, "Соответствует": 0.0})
        self.assertAlmostEqual(s["p"], 1 / 3)
        self.assertAlmostEqual(s["margin"], 0.0)
        self.assertAlmostEqual(s["entropy"], math.log(3))
        s2 = summarize_dist({"a": -0.01, "b": -10.0, "c": -1000.0})       # large spread: no overflow
        self.assertEqual(s2["label"], "a")
        self.assertGreater(s2["margin"], 0.99)
        self.assertAlmostEqual(joint_confidence(0.5, 0.4), 0.2)

    def test_distinguishable(self):
        self.assertTrue(distinguishable({"Риск": 1, "Нарушение": 2, "Соответствует": 3}))
        self.assertFalse(distinguishable({"Разработка через тестирование": 7, "Рефакторинг": 7}))

    def test_ece(self):
        self.assertAlmostEqual(ece([1.0, 1.0], [True, True]), 0.0)
        self.assertAlmostEqual(ece([0.9, 0.9], [False, False]), 0.9)
        self.assertAlmostEqual(ece([0.25, 0.75], [False, True]), 0.25)
        with self.assertRaises(ValueError):
            ece([], [])

    def test_auroc(self):
        self.assertEqual(auroc([0.9, 0.8, 0.1], [True, True, False]), 1.0)
        self.assertEqual(auroc([0.1, 0.9], [True, False]), 0.0)
        self.assertEqual(auroc([0.5, 0.5], [True, False]), 0.5)
        self.assertIsNone(auroc([0.5], [True]))


if __name__ == "__main__":
    unittest.main()
