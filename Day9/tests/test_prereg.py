"""prereg rehash: a dated amendment re-pins changed files, check_prereg honours it, old hashes are kept."""

import json
import shutil
import tempfile
import unittest
from pathlib import Path

import prereg
from core.hashes import check_prereg, current_hashes


class RehashTest(unittest.TestCase):
    def setUp(self):
        self.root = Path(tempfile.mkdtemp(prefix="day9_prereg_"))
        (self.root / "configs").mkdir()
        (self.root / "results").mkdir()
        self.cfg = self.root / "configs/teacher.yaml"
        self.cfg.write_text("model: a\n", encoding="utf-8")
        self.path = self.root / "results/prereg.json"
        self.path.write_text(json.dumps({"stages": {"v1": {"hashes": current_hashes(self.root)}}, "frozen": {},
                                         "amendments": []}), encoding="utf-8")

    def tearDown(self):
        shutil.rmtree(self.root)

    def test_rehash_flow(self):
        self.assertEqual(check_prereg(self.root), [])
        self.cfg.write_text("model: b\n", encoding="utf-8")
        self.assertEqual(check_prereg(self.root), ["configs/teacher.yaml"])
        with self.assertRaises(SystemExit):
            prereg.rehash("  ", root=self.root, path=self.path)
        changed = prereg.rehash("backend moved", root=self.root, path=self.path)
        self.assertEqual(list(changed), ["configs/teacher.yaml"])
        self.assertEqual(check_prereg(self.root), [])
        am = json.loads(self.path.read_text(encoding="utf-8"))["amendments"][-1]
        self.assertIn("date", am)
        self.assertNotEqual(am["rehash"]["configs/teacher.yaml"], am["rehash_old"]["configs/teacher.yaml"])
        with self.assertRaises(SystemExit):                     # nothing changed any more
            prereg.rehash("again", root=self.root, path=self.path)
        self.cfg.write_text("model: c\n", encoding="utf-8")     # a later change is caught again
        self.assertEqual(check_prereg(self.root), ["configs/teacher.yaml"])


    def test_budget_amendment_keeps_v1_and_derives_override(self):
        v1_before = self.path.read_text(encoding="utf-8")
        override = prereg.amend_budget(20.0, "user allowed GPUStack up to $20 and another G2 correction",
                                       path=self.path)
        data = json.loads(self.path.read_text(encoding="utf-8"))
        self.assertEqual(data["stages"]["v1"], json.loads(v1_before)["stages"]["v1"])
        self.assertEqual(override["soft_limit_usd"], 17.5)
        self.assertEqual(override["forecast_max_usd"], 16.0)
        self.assertEqual(override["worst_case_margin"], 1.25)
        self.assertTrue(override["repeat_g2_correction"])
        self.assertIn("date", data["amendments"][-1])

    def test_stage_v2_captures_actual_data_audit_gates_and_budget(self):
        distill = self.root / "data/distill"
        distill.mkdir(parents=True)
        (distill / "train.jsonl").write_text('{"id":"T"}\n', encoding="utf-8")
        (distill / "valid.jsonl").write_text('{"id":"V"}\n', encoding="utf-8")
        stats = {"train": 504, "valid": 49, "rejected": {"x": 1},
                 "normalized_principle_line": {"samples": 0}, "cells": {"below_60": []}}
        (distill / "build_stats.json").write_text(json.dumps(stats), encoding="utf-8")
        audit = {"summary": {"decisions": 570}, "coverage": {"expected": 570}}
        (distill / "audit.json").write_text(json.dumps(audit), encoding="utf-8")
        (distill / "leaks_drop.json").write_text('["NS-1"]', encoding="utf-8")
        g3a = {"n_new": 1984, "eval": [], "eval_answers": [{}], "valid": [], "intra": []}
        (self.root / "results/g3a.json").write_text(json.dumps(g3a), encoding="utf-8")
        gates = {"G2": {"pass": False}, "G3": {"pass": True}, "G4": {"pass": True}}
        (self.root / "results/gates.json").write_text(json.dumps(gates), encoding="utf-8")
        (self.root / "results/teacher_usage.jsonl").write_text("", encoding="utf-8")
        prereg.amend_budget(20.0, "test override", path=self.path)
        v2 = prereg.stage_v2(self.root, self.path)
        self.assertEqual(v2["data_sizes"], {"train": 504, "valid": 49})
        self.assertEqual(v2["gates"], gates)
        self.assertEqual(v2["budget"]["limit_usd"], 20.0)
        self.assertEqual(v2["budget"]["soft_limit_usd"], 17.5)
        self.assertEqual(v2["audit_stats"]["g3b_summary"]["decisions"], 570)
        self.assertEqual(v2["hashes"]["data/distill/valid.jsonl"], current_hashes(self.root)["data/distill/valid.jsonl"])

    def test_freeze_supersede_keeps_history(self):
        prereg.freeze("teacher_provider", "a", "first", path=self.path)
        with self.assertRaises(SystemExit):
            prereg.freeze("teacher_provider", "b", "again", path=self.path)
        with self.assertRaises(SystemExit):
            prereg.freeze("teacher_reasoning", "off", "x", path=self.path, supersede=True)   # nothing to supersede
        prereg.freeze("teacher_provider", "b", "moved", path=self.path, supersede=True)
        data = json.loads(self.path.read_text(encoding="utf-8"))
        self.assertEqual(data["frozen"]["teacher_provider"], "b")
        self.assertEqual(data["amendments"][-1]["superseded"], {"teacher_provider": "a"})


if __name__ == "__main__":
    unittest.main()
