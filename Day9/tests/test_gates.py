import unittest

import gates
from gates import (fallback_trigger, gate_g0, gate_g1, gate_g2, gate_g3, gate_g4, gate_g5, gate_g6, gate_g7, gate_g8,
                   reasoning_decision)

N = 57


def seq(k, n=N):
    return [True] * k + [False] * (n - k)


VERDICTS = ("Соответствует", "Нарушение", "Риск")


class G0Test(unittest.TestCase):
    def test_bounds(self):
        self.assertTrue(gate_g0(gates.G0_SHA256, 0.439)["pass"])
        self.assertTrue(gate_g0(gates.G0_SHA256, 0.474)["pass"])      # 0.439 + 0.035 inclusive
        self.assertFalse(gate_g0(gates.G0_SHA256, 0.475)["pass"])
        self.assertFalse(gate_g0("0" * 64, 0.439)["pass"])
        self.assertFalse(gate_g0(gates.G0_SHA256, 0.439, ["prompt:SYSTEM"])["pass"])
        self.assertFalse(gate_g0(gates.G0_SHA256, None)["pass"])         # regression not run yet


class G1Test(unittest.TestCase):
    def test_pass_and_fail(self):
        base = seq(25)
        teacher = seq(25)  # same -> no improvement
        self.assertFalse(gate_g1(teacher, base, 1.0)["pass"])
        better = [True] * 40 + [False] * 17                            # 15 discordant in favour of teacher
        r = gate_g1(better, base, 0.96)
        self.assertTrue(r["pass"], r)
        self.assertFalse(gate_g1(better, base, 0.94)["pass"])          # format_ok < 0.95
        self.assertTrue(gate_g1(better, base, 0.95)["checks"]["format_ok_ge_0.95"])

    def test_p_value_boundary(self):
        base = [False] * 6 + [True] * 51
        r = gate_g1([True] * 57, base, 1.0)                             # 6 vs 0 discordant: p = 2/64 = 0.031
        self.assertTrue(r["checks"]["mcnemar_p_lt_0.05"])
        base5 = [False] * 5 + [True] * 52
        r5 = gate_g1([True] * 57, base5, 1.0)                           # 5 vs 0: p = 2/32 = 0.0625
        self.assertFalse(r5["checks"]["mcnemar_p_lt_0.05"])


class ReasoningRuleTest(unittest.TestCase):
    def test_delta_boundary(self):
        off = seq(30)
        self.assertEqual(reasoning_decision(off, seq(34), 0.96, 1.5)["reasoning"], "on")   # +4/57 = 0.0702
        self.assertEqual(reasoning_decision(off, seq(33), 0.96, 1.5)["reasoning"], "off")  # +3/57
        self.assertEqual(reasoning_decision(off, seq(40), 0.94, 1.5)["reasoning"], "off")  # format
        self.assertEqual(reasoning_decision(off, seq(40), 0.95, 1.6)["reasoning"], "on")   # inclusive bounds
        self.assertEqual(reasoning_decision(off, seq(40), 0.99, 1.61)["reasoning"], "off") # budget forecast
        with self.assertRaises(ValueError):
            reasoning_decision(seq(1, 3), seq(1, 4), 1.0, 0.1)


class G2G3G4Test(unittest.TestCase):
    def labels(self, n_per=10, principles=("a", "b")):
        return [(v, p) for p in principles for v in VERDICTS for _ in range(n_per)]

    def test_g2(self):
        lab = self.labels(n_per=70)                                     # 420 rows, 140 per principle
        ok = gate_g2(1000, 20, 900, 450, lab, VERDICTS, ("a", "b"))
        self.assertTrue(ok["pass"], ok)
        self.assertFalse(gate_g2(1000, 21, 900, 450, lab, VERDICTS, ("a", "b"))["pass"])    # errors 2.1%
        self.assertFalse(gate_g2(1000, 0, 900, 451, lab, VERDICTS, ("a", "b"))["pass"])     # reject > 50%
        self.assertFalse(gate_g2(1000, 0, 900, 0, lab[:399], VERDICTS, ("a", "b"))["pass"])  # < 400
        skew = [("Нарушение", "a")] * 340 + [("Риск", "b")] * 40 + [("Соответствует", "b")] * 40
        self.assertFalse(gate_g2(1, 0, 1, 0, skew, VERDICTS, ("a", "b"))["checks"]["verdict_share"])
        self.assertFalse(gate_g2(1, 0, 1, 0, lab, VERDICTS, ("a", "b", "c"))["checks"]["principle_rows"])

    def test_g3_g4(self):
        self.assertTrue(gate_g3(0, 0, True)["pass"])
        self.assertFalse(gate_g3(0, 0, False)["pass"])
        a = [("Риск", "tdd")] * 20
        self.assertTrue(gate_g4(a, a[:18] + [("Нарушение", "tdd")] * 2)["pass"])         # 18/20 = 0.9
        self.assertFalse(gate_g4(a, a[:17] + [("Нарушение", "tdd")] * 3)["pass"])
        self.assertFalse(gate_g4([(None, None)] * 20, [(None, None)] * 20)["pass"])       # unparsed != agreement


class PostGatesTest(unittest.TestCase):
    def test_g5(self):
        base_list = seq(10)
        base_good = seq(25)
        self.assertTrue(gate_g5(seq(30), base_list, base_good)["pass"])
        self.assertFalse(gate_g5(seq(24), base_list, base_good)["pass"])     # below base+good
        self.assertTrue(gate_g5(seq(25), base_list, base_good)["checks"]["ge_base_good"])
        self.assertFalse(gate_g5(seq(10), base_list, seq(0))["pass"])        # no gain over base+list

    def test_g6(self):
        teacher = seq(40)
        self.assertTrue(gate_g6(seq(40), teacher, 0.96)["pass"])
        self.assertFalse(gate_g6(seq(40), teacher, 0.94)["pass"])
        self.assertFalse(gate_g6(seq(20), teacher, 1.0)["pass"])

    def test_g7_g8(self):
        self.assertTrue(gate_g7(seq(80, 100), seq(80, 100))["pass"])
        self.assertFalse(gate_g7(seq(60, 100), seq(80, 100))["pass"])
        self.assertTrue(gate_g8([], False, False, 2.0)["pass"])
        self.assertFalse(gate_g8([], False, False, 2.0001)["pass"])
        self.assertFalse(gate_g8(["data/format/eval.jsonl"], False, False, 1.0)["pass"])
        self.assertFalse(gate_g8([], True, False, 1.0)["pass"])
        self.assertFalse(gate_g8([], False, True, 1.0)["pass"])


class FallbackTriggerTest(unittest.TestCase):
    def test_boundaries(self):
        self.assertFalse(fallback_trigger(0.60, 0.50)["fallback"])          # 0.60 and = base + 0.10
        self.assertTrue(fallback_trigger(0.59, 0.30)["fallback"])           # below 0.60
        self.assertTrue(fallback_trigger(0.70, 0.61)["fallback"])           # below base + 0.10
        self.assertFalse(fallback_trigger(0.71, 0.61)["fallback"])


if __name__ == "__main__":
    unittest.main()
