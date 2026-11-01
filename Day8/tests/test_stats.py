import math
import unittest

from eval.stats import mcnemar, mcnemar_exact, non_inferior, paired_bootstrap, wilson


class StatsTest(unittest.TestCase):
    def test_mcnemar_exact(self):
        self.assertEqual(mcnemar_exact(0, 0), 1.0)
        self.assertAlmostEqual(mcnemar_exact(0, 5), 2 / 32)          # 2 * 0.5^5
        self.assertAlmostEqual(mcnemar_exact(1, 9), 2 * 11 / 1024)   # 2 * (C(10,0)+C(10,1)) / 2^10
        self.assertEqual(mcnemar_exact(5, 5), 1.0)
        self.assertEqual(mcnemar_exact(3, 7), mcnemar_exact(7, 3))

    def test_mcnemar_paired(self):
        r = mcnemar([True, True, False, False], [True, False, True, True])
        self.assertEqual((r["only_a"], r["only_b"]), (1, 2))

    def test_wilson(self):
        lo, hi = wilson(0, 10)
        self.assertEqual(lo, 0.0)
        self.assertAlmostEqual(hi, 0.2775, places=3)
        lo, hi = wilson(50, 100)
        self.assertAlmostEqual(lo, 0.4038, places=3)
        self.assertAlmostEqual(hi, 0.5962, places=3)
        self.assertEqual(wilson(0, 0), (0.0, 1.0))

    def test_bootstrap(self):
        a = [0.0] * 50 + [1.0] * 50
        same = paired_bootstrap(a, a, n_boot=500)
        self.assertEqual((same["delta"], same["lo"], same["hi"]), (0.0, 0.0, 0.0))
        better = paired_bootstrap([0.0] * 100, [1.0] * 60 + [0.0] * 40, n_boot=2000)
        self.assertAlmostEqual(better["delta"], 0.6)
        self.assertTrue(0.45 < better["lo"] < 0.6 < better["hi"] < 0.75)
        self.assertEqual(paired_bootstrap(a, a, n_boot=100, seed=1), paired_bootstrap(a, a, n_boot=100, seed=1))

    def test_cluster_bootstrap_wider(self):
        # 10 clusters x 10 identical items: cluster resampling must give a wider CI than item resampling
        b = [1.0 if c % 2 else 0.0 for c in range(10) for _ in range(10)]
        a = [0.0] * 100
        clusters = [str(c) for c in range(10) for _ in range(10)]
        flat = paired_bootstrap(a, b, n_boot=3000)
        clus = paired_bootstrap(a, b, clusters, n_boot=3000)
        self.assertEqual(clus["n_clusters"], 10)
        self.assertGreater(clus["hi"] - clus["lo"], 1.5 * (flat["hi"] - flat["lo"]))

    def test_non_inferior(self):
        self.assertTrue(non_inferior({"lo": -0.04}))
        self.assertFalse(non_inferior({"lo": -0.06}))
        self.assertTrue(math.isclose(-0.05, -0.05) and non_inferior({"lo": -0.05}))


if __name__ == "__main__":
    unittest.main()
