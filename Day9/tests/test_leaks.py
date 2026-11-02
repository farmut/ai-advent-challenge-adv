import unittest

from leaks import aggregate_audit, audit_leaks, dedup_within, is_generated, top_k, violations

A = "Команда каждую пятницу выкатывает релиз в прод после ручного регресса на стенде заказчика."
B = "Команда каждую пятницу выкатывает релиз в прод после ручного регресса на стенде клиента."
C = "Аналитик пишет приемочные тесты на Gherkin до начала итерации вместе с QA."


class LeaksTest(unittest.TestCase):
    def test_all_generation_waves_are_checked(self):
        self.assertTrue(all(is_generated(x) for x in ("NS-1", "NS2-p-001", "NS3-p-001")))
        self.assertFalse(is_generated("FT-001"))

    def test_violations_threshold_strict(self):
        v = violations({"NS-1": B, "NS-2": C}, {"EV-1": A}, 0.3)
        self.assertEqual([x["id"] for x in v], ["NS-1"])
        self.assertEqual(v[0]["ref"], "EV-1")
        self.assertEqual(violations({"NS-1": A}, {"EV-1": A}, 1.0), [])     # equal to threshold is allowed

    def test_dedup_keeps_orig_and_first(self):
        d = dedup_within({"NS-b": B, "FT-001": A, "NS-a": A, "NS-c": C})
        self.assertEqual(sorted(x["id"] for x in d), ["NS-a", "NS-b"])
        self.assertEqual(dedup_within({"FT-001": A, "FT-002": A}), [])      # orig rows are never dropped

    def test_top_k(self):
        t = top_k({"EV-1": A}, {"NS-1": B, "NS-2": C, "NS-3": A}, k=2)
        self.assertEqual([x["id"] for x in t["EV-1"]], ["NS-3", "NS-1"])
        self.assertEqual(t["EV-1"][0]["jaccard"], 1.0)

    def test_audit_leaks(self):
        audit = {"EV-1": {"candidates": {"NS-1": "LEAK", "NS-2": "OK", "NS-3": {"status": "leak"}}},
                 "EV-2": {"candidates": {}}}
        self.assertEqual(audit_leaks(audit), {"NS-1", "NS-3"})

    def test_aggregate_exact_coverage_and_leak_wins(self):
        candidates = {"EV-1": [{"id": "NS-1"}, {"id": "NS-2"}]}
        shards = [{"decisions": [{"eval": "EV-1", "train": "NS-1", "status": "OK", "why": "different"},
                                 {"eval": "EV-1", "train": "NS-2", "status": "NOT_IN_DATA", "why": "absent"}]},
                  {"decisions": [{"eval": "EV-1", "train": "NS-1", "status": "LEAK", "why": "same action"}]}]
        audit = aggregate_audit(candidates, shards)
        self.assertEqual(audit["summary"]["decisions"], 2)
        self.assertEqual(audit["leaks"], [{"eval": "EV-1", "train": ["NS-1"], "why": "same action"}])
        self.assertEqual(audit_leaks(audit), {"NS-1"})
        self.assertEqual(len(audit["coverage"]["duplicates"]), 1)
        with self.assertRaisesRegex(ValueError, "missing audit decisions"):
            aggregate_audit(candidates, [{"decisions": shards[0]["decisions"][:1]}])

    def test_aggregate_rejects_bad_status_and_empty_why(self):
        candidates = {"EV-1": [{"id": "NS-1"}]}
        base = {"eval": "EV-1", "train": "NS-1", "status": "OK", "why": "reason"}
        with self.assertRaisesRegex(ValueError, "invalid audit status"):
            aggregate_audit(candidates, [{"decisions": [{**base, "status": "MAYBE"}]}])
        with self.assertRaisesRegex(ValueError, "empty audit rationale"):
            aggregate_audit(candidates, [{"decisions": [{**base, "why": " "}]}])


if __name__ == "__main__":
    unittest.main()
