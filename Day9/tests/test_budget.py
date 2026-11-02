import json
import tempfile
import unittest
from pathlib import Path

from teacher.budget import Budget, BudgetExceeded, Prices, derived_limits, forecast_ok, worst_case_cost


class BudgetTest(unittest.TestCase):
    def test_reserve_commit_release(self):
        b = Budget()
        r = b.reserve(0.5, "C3")
        self.assertEqual(b.reserved, 0.5)
        b.commit(r, 0.2)
        self.assertEqual((b.spent, b.reserved), (0.2, 0.0))
        r2 = b.reserve(0.1, "B")
        b.release(r2)
        self.assertEqual((b.spent, b.reserved), (0.2, 0.0))

    def test_reservations_block_concurrent_overshoot(self):
        b = Budget()
        b.reserve(1.0, "C3")
        b.reserve(0.75, "C3")                     # exactly 1.75: allowed (inclusive)
        with self.assertRaises(BudgetExceeded):
            b.reserve(0.0001, "C3")

    def test_soft_limit_only_for_non_d(self):
        b = Budget(spent=1.75)
        with self.assertRaises(BudgetExceeded):
            b.reserve(0.001, "C4")
        b.reserve(0.25, "D")                      # D may reach exactly 2.00
        with self.assertRaises(BudgetExceeded):
            b.reserve(0.000001, "D")

    def test_hard_limit(self):
        b = Budget(spent=2.0)
        for step in ("B", "C1", "D"):
            with self.assertRaises(BudgetExceeded):
                b.reserve(0.0001, step)
        self.assertEqual(Budget(spent=1.0).remaining("D"), 1.0)

    def test_restore_from_usage_log(self):
        p = Path(tempfile.mkdtemp()) / "usage.jsonl"
        self.assertEqual(Budget.spent_in_log(p), 0.0)
        rows = [{"cost": 0.4, "from_cache": False}, {"cost": 0.0, "from_cache": True, "cached_cost": 0.4},
                {"cost": 0.3, "from_cache": False}, {"cost": None, "from_cache": False}]
        p.write_text("\n".join(json.dumps(r) for r in rows) + "\n\n", encoding="utf-8")
        b = Budget.from_usage_log(p)
        self.assertAlmostEqual(b.spent, 0.7)
        with self.assertRaises(BudgetExceeded):
            b.reserve(1.06, "C2")
        b.reserve(1.05, "C2")

    def test_active_budget_override(self):
        from teacher.factory import make_budget

        d = Path(tempfile.mkdtemp())
        prereg = d / "prereg.json"
        usage = d / "usage.jsonl"
        prereg.write_text(json.dumps({"amendments": [{"budget_override": {"limit_usd": 20.0}}]}), encoding="utf-8")
        usage.write_text(json.dumps({"cost": 1.25, "from_cache": False}) + "\n", encoding="utf-8")
        b = make_budget(prereg, usage)
        self.assertEqual((b.limit, b.soft_limit, b.spent), (20.0, 17.5, 1.25))
        self.assertEqual(derived_limits(20.0)["forecast_max_usd"], 16.0)

    def test_worst_case_and_forecast(self):
        self.assertAlmostEqual(Prices().cost(1_000_000, 1_000_000), 0.75)
        w = worst_case_cost(4000, 400)
        self.assertAlmostEqual(w, 1.25 * (2016 * 0.15 + 400 * 0.60) / 1e6)
        self.assertTrue(forecast_ok(1.0, 0.6))
        self.assertFalse(forecast_ok(1.0, 0.61))
        with self.assertRaises(ValueError):
            Budget(limit=1.0, soft_limit=1.5)
        with self.assertRaises(ValueError):
            Budget().reserve(-1, "B")


if __name__ == "__main__":
    unittest.main()
