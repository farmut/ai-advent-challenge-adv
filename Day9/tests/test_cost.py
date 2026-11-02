import unittest

from eval.cost import (amortization_cost, break_even, break_even_if_replaces, energy_cost, market_cost, mean_cost)


class CostTest(unittest.TestCase):
    def test_local_costs(self):
        self.assertAlmostEqual(energy_cost(3600, 90, 0.2), 0.018)
        self.assertAlmostEqual(amortization_cost(3 * 365 * 24 * 3600, 3000), 3000)
        self.assertAlmostEqual(amortization_cost(3600, 3000, utilization=0.5),
                               2 * amortization_cost(3600, 3000))
        with self.assertRaises(ValueError):
            amortization_cost(1, 1, utilization=0)
        self.assertAlmostEqual(market_cost(1e6, 1e6, 0.04, 0.1), 0.14)
        self.assertEqual(mean_cost([]), 0.0)
        self.assertAlmostEqual(mean_cost([0.1, 0.3]), 0.2)

    def test_break_even(self):
        r = break_even(1.5, 0.0003, 0.0001)
        self.assertAlmostEqual(r["requests"], 7500)
        self.assertIsNone(break_even(1.5, 0.0001, 0.0001)["requests"])      # denominator 0
        self.assertIsNone(break_even(1.5, 0.0001, 0.0002)["requests"])      # negative saving
        self.assertIn("denominator", break_even(1.5, 0.0, 0.0)["reason"])

    def test_only_when_g6_passes(self):
        self.assertIsNone(break_even_if_replaces(False, 1.5, 0.0003, 0.0001)["requests"])
        self.assertAlmostEqual(break_even_if_replaces(True, 1.5, 0.0003, 0.0001)["requests"], 7500)


if __name__ == "__main__":
    unittest.main()
