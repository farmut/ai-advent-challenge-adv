import unittest
from collections import Counter

import prompts
from distill import balance_to_multiple, build, gold_rows, select_new_cells, split, valid_candidates

ANS = ("Вердикт: {v}\nПринцип: Устойчивый темп\nОбоснование: Проект — марафон, а не спринт, номер {i}: "
       "переработки снижают качество.\nРекомендация: Сократите объём по данным о скорости команды, вариант {i}.")


def row(i, source="new", split_=None, v="Нарушение", answer=True):
    r = {"id": f"NS-p-{i:03d}" if source == "new" else f"FT-{i:03d}", "source": source, "user": f"ситуация {i}",
         "gold_verdict": v, "gold_principle": "sustainable_pace", "answer": ANS.format(v=v, i=i if source == "new" else 1000 + i) if answer else None,
         "decision": {"reason": None if answer else "disagree"}}
    if split_:
        r["split"] = split_
    return r


class SplitTest(unittest.TestCase):
    def test_gold_rows(self):
        rows = gold_rows()
        self.assertEqual(Counter(r["split"] for r in rows), {"train": 249, "valid": 30})
        self.assertEqual(len({r["id"] for r in rows}), 279)
        self.assertTrue(all(r["gold_verdict"] and r["gold_principle"] for r in rows))
        self.assertFalse(any(r["id"].startswith("EV-") for r in rows))

    def test_valid_candidates_deterministic(self):
        ids = [f"NS-p-{i:03d}" for i in range(100)]
        a = valid_candidates(ids)
        self.assertEqual(a, valid_candidates(list(reversed(ids))))
        self.assertEqual(len(a), 45)
        self.assertNotEqual(a, valid_candidates(ids, seed=1))

    def test_split_disjoint_and_ordered(self):
        acc = [row(i) for i in range(60)] + [row(i, "orig", "valid") for i in range(1, 4)]
        cands = [f"NS-p-{i:03d}" for i in (50, 3, 7, 99)]          # 99 was rejected -> skipped
        train, valid = split(acc, cands, n_valid_new=2)
        self.assertEqual({r["id"] for r in valid}, {"NS-p-050", "NS-p-003", "FT-001", "FT-002", "FT-003"})
        self.assertFalse({r["id"] for r in train} & {r["id"] for r in valid})
        self.assertEqual(len(train) + len(valid), len(acc))

    def test_supplemental_cells_select_only_new_wave(self):
        rows = [row(1), {**row(2), "id": "NS2-p-001"}]
        selected = select_new_cells(rows, [{"cell_id": "NS2-p-001"}])
        self.assertEqual([r["id"] for r in selected], ["NS2-p-001"])
        train, valid = split(rows, ["NS-p-001"])
        self.assertEqual([r["id"] for r in valid], ["NS-p-001"])
        self.assertEqual([r["id"] for r in train], ["NS2-p-001"])

    def test_balance_multiple_of_four_drops_only_new(self):
        rows = [row(i, "orig", v="Риск") for i in range(1, 6)] + [row(i) for i in range(6)]
        out, dropped = balance_to_multiple(rows)
        self.assertEqual(len(out) % 4, 0)
        self.assertEqual(len(dropped), 3)
        self.assertTrue(all(r["source"] == "new" for r in dropped))

    def test_build_end_to_end(self):
        labeled = ([row(i) for i in range(40)] + [row(41, answer=False)] +
                   [row(i, "orig", "valid") for i in range(1, 3)] +
                   [{**row(3, "orig", "valid", answer=False), "gold_answer": ANS.format(v="Нарушение", i="g")}] +
                   [{**row(1), "id": "EV-001"}])
        res = build(labeled, [f"NS-p-{i:03d}" for i in range(5)], leak_drop={"NS-p-039"})
        tr, va = res["train"], res["valid"]
        self.assertEqual(len(va), 3 + 5)
        self.assertEqual(len(tr) % 4, 0)
        ids_tr, ids_va = {r["id"] for r in tr}, {r["id"] for r in va}
        self.assertFalse(ids_tr & ids_va)
        self.assertNotIn("EV-001", ids_tr | ids_va)
        self.assertNotIn("NS-p-039", ids_tr | ids_va)
        self.assertTrue(all(r["messages"][0]["content"] == prompts.LIST_PROMPT for r in tr + va))
        gold_fallback = next(r for r in va if r["id"] == "FT-003")
        self.assertFalse(gold_fallback["meta"]["teacher"])
        self.assertEqual(res["stats"]["rejected"]["forbidden_or_leak"], 2)
        self.assertEqual(res["formatonly"], [])                          # no raw samples in this fixture
        with_samples = [{**row(77, answer=False), "samples": ["мусор", ANS.format(v="Риск", i=77)]}]
        fo = build(with_samples, [], set())["formatonly"]
        self.assertEqual([x["meta"]["gold_verdict"] for x in fo], ["Нарушение"])   # labels ignored, gold kept
        self.assertIn("Вердикт: Риск", fo[0]["messages"][2]["content"])
        gist = build(labeled, [], set(), prompt_key="gist")
        self.assertEqual(gist["train"][0]["messages"][0]["content"], prompts.LIST_PROMPT_GIST)


if __name__ == "__main__":
    unittest.main()
