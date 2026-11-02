import math
import unittest

from eval.cascade import choose_tau, curve, defers, evaluate, expected_cost, report


def r(conf, s, t, fmt=True):
    return {"conf": conf, "format_ok": fmt, "student_correct": s, "teacher_correct": t}


ROWS = [r(0.95, True, True), r(0.9, True, False), r(0.8, False, True), r(0.6, False, True), r(0.5, True, True),
        r(0.99, False, True, fmt=False)]


class CascadeTest(unittest.TestCase):
    def test_defer_rules(self):
        self.assertTrue(defers(r(0.99, True, True, fmt=False), -math.inf))    # broken format always defers
        self.assertFalse(defers(r(0.5, True, True), 0.5))                     # conf == τ is kept
        self.assertTrue(defers(r(0.49, True, True), 0.5))

    def test_curve_endpoints(self):
        c = curve(ROWS)
        self.assertEqual(c[0]["tau"], -math.inf)
        self.assertAlmostEqual(c[0]["defer_rate"], 1 / 6)
        self.assertEqual(c[-1]["defer_rate"], 1.0)
        self.assertAlmostEqual(c[-1]["accuracy"], 5 / 6)                    # all teacher

    def test_choose_tau_minimal_deferral(self):
        res = choose_tau(ROWS, tol=0.05)
        self.assertAlmostEqual(res["teacher_acc"], 5 / 6)
        self.assertGreaterEqual(res["accuracy"], res["target"])
        self.assertEqual(res["tau"], 0.8)                                   # defers 0.6, 0.5 and broken: 5/6
        self.assertAlmostEqual(res["defer_rate"], 0.5)
        self.assertEqual(res["tau_from"], "valid")
        loose = choose_tau(ROWS, tol=0.5)
        self.assertEqual(loose["tau"], -math.inf)                           # format-only deferral suffices
        self.assertLessEqual(loose["defer_rate"], res["defer_rate"])

    def test_cost_and_report(self):
        self.assertAlmostEqual(expected_cost(0.25, 0.0001, 0.0004), 0.0002)
        rep = report(ROWS, 0.9, 0.0, 0.001)
        self.assertEqual(rep["defer_rate"], evaluate(ROWS, 0.9)["defer_rate"])
        self.assertAlmostEqual(rep["cost_per_request"], rep["defer_rate"] * 0.001)
        self.assertEqual(len(rep["curve"]), len(curve(ROWS)))
        with self.assertRaises(ValueError):
            evaluate([], 0.5)


if __name__ == "__main__":
    unittest.main()
