import json
import tempfile
import unittest
from pathlib import Path

from teacher.cache import TeacherCache, cache_key

BASE = dict(model="m", messages=[{"role": "system", "content": "S"}, {"role": "user", "content": "U"}],
            temperature=0.0, max_tokens=400, reasoning="off", seed=42, sample_idx=0, prompt_version="day9-v1",
            provider=("ProvA",))


class CacheKeyTest(unittest.TestCase):
    def test_stable_and_canonical(self):
        k = cache_key(**BASE)
        self.assertEqual(k, cache_key(**BASE))
        self.assertEqual(len(k), 64)
        self.assertEqual(k, cache_key(**{**BASE, "temperature": 0}))           # 0 == 0.0
        extra = [{"role": "system", "content": "S", "name": "x"}, BASE["messages"][1]]
        self.assertEqual(k, cache_key(**{**BASE, "messages": extra}))           # only role/content count

    def test_every_field_changes_key(self):
        k = cache_key(**BASE)
        changes = {"model": "m2", "temperature": 0.7, "max_tokens": 401, "reasoning": "low", "seed": 43,
                   "sample_idx": 1, "prompt_version": "day9-v2", "provider": ("ProvB",),
                   "endpoint": "openai_compat:https://x/v1/chat/completions",
                   "extra": {"chat_template_kwargs": {"thinking": False}},
                   "messages": [{"role": "system", "content": "S"}, {"role": "user", "content": "U2"}]}
        for f, v in changes.items():
            self.assertNotEqual(k, cache_key(**{**BASE, f: v}), f)

    def test_atomic_put_and_get(self):
        c = TeacherCache(Path(tempfile.mkdtemp()))
        k = cache_key(**BASE)
        self.assertIsNone(c.get(k))
        p = c.put(k, {"status": 200, "result": {"text": "x"}})
        self.assertEqual(p.parent.name, k[:2])
        self.assertEqual(c.get(k)["result"]["text"], "x")
        self.assertEqual([x.name for x in p.parent.iterdir()], [p.name])        # no temp files left
        with self.assertRaises(ValueError):
            c.put(k, {"status": 429})
        p.write_text("{broken", encoding="utf-8")
        self.assertIsNone(c.get(k))
        json.loads(json.dumps(BASE["messages"]))


if __name__ == "__main__":
    unittest.main()
