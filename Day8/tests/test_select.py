import unittest

from select_ckpt import ema_by_step, loss_candidates, overfit_point, pick_best


class SelectTest(unittest.TestCase):
    def test_candidates_and_best(self):
        val = {0: {"macro": 2.0}, 25: {"macro": 1.0}, 50: {"macro": 0.8}, 75: {"macro": 0.85},
               100: {"macro": 0.9}, 125: {"macro": 1.2}}
        c = loss_candidates(val, saved={25, 50, 75, 100, 125})
        self.assertEqual(c, [50, 75, 100])
        self.assertEqual(loss_candidates(val, saved={25, 100}), [100, 25])
        self.assertEqual(pick_best(c, val, None), 50)
        self.assertEqual(pick_best(c, val, {50: 0.6, 75: 0.7, 100: 0.7}), 75)  # tie -> lower loss

    def test_overfit_point(self):
        steps = [25, 50, 75, 100, 125]
        macro = [1.0, 0.8, 0.84, 0.86, 0.9]
        ema = {25: 1.0, 50: 0.8, 75: 0.7, 100: 0.6, 125: 0.5}
        self.assertEqual(overfit_point(steps, macro, ema), 100)  # 0.86 and 0.9 >= 0.8 + 0.05
        self.assertIsNone(overfit_point(steps, [1.0, 0.8, 0.84, 0.82, 0.84], ema))
        flat_train = {s: 0.9 for s in steps}
        self.assertIsNone(overfit_point(steps, macro, flat_train))  # train not falling

    def test_ema(self):
        e = ema_by_step([{"step": 5, "loss": 1.0}, {"step": 10, "loss": 0.0}], alpha=0.5)
        self.assertEqual(e, {5: 1.0, 10: 0.5})


if __name__ == "__main__":
    unittest.main()
