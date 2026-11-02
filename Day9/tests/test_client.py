import asyncio
import json
import logging
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from teacher.budget import Budget, BudgetExceeded
from teacher.cache import TeacherCache
from teacher.client import (MissingApiKey, TeacherClient, TeacherConfig, TeacherError, TeacherRequest, api_key_from_env,
                            build_payload, retry_after_s)
from tests._helpers import SECRET, FakeResp, FakeSession, ok_body, sse

CFG = TeacherConfig(provider_order=("ProvA",), max_retries=5)


def req(**kw):
    return TeacherRequest.chat("SYS", "Ситуация", step=kw.pop("step", "B"), **kw)


class ClientCase(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="day9_client_"))
        self.cache = TeacherCache(self.tmp / "cache")
        self.log = self.tmp / "usage.jsonl"
        self.sleeps = []

    def client(self, responses, budget=None, use_cache=True, cfg=CFG):
        async def sleep(s):
            self.sleeps.append(s)
        self.session = FakeSession(responses)
        return TeacherClient(cfg, cache=self.cache, budget=budget or Budget(), usage_log=self.log, api_key=SECRET,
                             session=self.session, use_cache=use_cache, sleep=sleep)

    def run_(self, coro):
        return asyncio.run(coro)

    def log_rows(self):
        return [json.loads(x) for x in self.log.read_text(encoding="utf-8").splitlines()]


class PayloadTest(unittest.TestCase):
    def test_payload_fields(self):
        p = build_payload(CFG, req(reasoning="off"))
        self.assertEqual(p["usage"], {"include": True})
        self.assertEqual(p["reasoning"], {"enabled": False})
        self.assertEqual(p["provider"], {"order": ["ProvA"], "allow_fallbacks": False})
        self.assertEqual(p["model"], "deepseek/deepseek-v4.1-flash-1017")
        self.assertNotIn("stream", p)
        self.assertEqual(build_payload(CFG, req(reasoning="low"))["reasoning"], {"effort": "low"})
        self.assertTrue(build_payload(CFG, req(), stream=True)["stream"])

    def test_unpinned_provider_refused(self):
        with self.assertRaises(ValueError):
            build_payload(TeacherConfig(), req())

    def test_bad_reasoning(self):
        with self.assertRaises(ValueError):
            req(reasoning="max")

    def test_retry_after(self):
        self.assertEqual(retry_after_s({"Retry-After": "3"}), 3.0)
        self.assertEqual(retry_after_s({"Retry-After": "999"}), 60.0)
        self.assertIsNone(retry_after_s({}))
        self.assertIsNone(retry_after_s({"Retry-After": "soon"}))
        self.assertGreaterEqual(retry_after_s({"Retry-After": "Wed, 21 Oct 2015 07:28:00 GMT"}), 0.0)

    def test_key_from_env_only(self):
        with mock.patch.dict(os.environ, {"LLM_API_KEY": SECRET}):
            self.assertEqual(api_key_from_env(), SECRET)
        with mock.patch.dict(os.environ, {}, clear=True):
            with self.assertRaises(MissingApiKey) as cm:
                api_key_from_env()
            self.assertNotIn("sk-or", str(cm.exception))


class CacheBehaviourTest(ClientCase):
    def test_success_then_cache_hit_without_network(self):
        c = self.client([FakeResp(200, ok_body())])
        r1 = self.run_(c.complete(req()))
        self.assertFalse(r1.from_cache)
        self.assertAlmostEqual(r1.cost, 0.0003)
        self.assertEqual(r1.provider, "ProvA")
        c2 = self.client([])  # any network call would raise
        r2 = self.run_(c2.complete(req()))
        self.assertTrue(r2.from_cache)
        self.assertEqual(r2.text, r1.text)
        self.assertEqual(r2.cost, 0.0)
        rows = self.log_rows()
        self.assertEqual([r["from_cache"] for r in rows], [False, True])
        self.assertAlmostEqual(Budget.spent_in_log(self.log), 0.0003)

    def test_only_200_is_cached(self):
        c = self.client([FakeResp(400, {"error": {"message": "bad"}})])
        with self.assertRaises(TeacherError) as cm:
            self.run_(c.complete(req()))
        self.assertEqual(cm.exception.status, 400)
        self.assertEqual(list((self.tmp / "cache").rglob("*.json")), [])
        self.assertEqual(len(self.session.calls), 1)  # 400 is not retried

    def test_no_cache_mode_neither_reads_nor_writes(self):
        self.run_(self.client([FakeResp(200, ok_body())]).complete(req()))
        n = len(list((self.tmp / "cache").rglob("*.json")))
        c = self.client([FakeResp(200, ok_body(text="Вердикт: Нарушение"))], use_cache=False)
        r = self.run_(c.complete(req()))
        self.assertFalse(r.from_cache)
        self.assertEqual(len(self.session.calls), 1)
        self.assertEqual(len(list((self.tmp / "cache").rglob("*.json"))), n)
        self.assertNotEqual(self.run_(self.client([]).complete(req())).text, r.text)  # cache untouched


class RetryTest(ClientCase):
    def test_429_retry_after_then_ok(self):
        c = self.client([FakeResp(429, {}, {"Retry-After": "7"}), FakeResp(503, {}), FakeResp(200, ok_body())])
        r = self.run_(c.complete(req()))
        self.assertEqual(r.attempts, 3)
        self.assertEqual(self.sleeps[0], 7.0)                 # Retry-After honoured
        self.assertTrue(1.0 <= self.sleeps[1] <= 2.0)          # backoff base*2^1 with jitter in [0.5, 1]
        self.assertEqual(self.log_rows()[-1]["errors"], ["http 429", "http 503"])

    def test_retry_limit_five(self):
        c = self.client([FakeResp(500, {}) for _ in range(6)])
        with self.assertRaises(TeacherError) as cm:
            self.run_(c.complete(req()))
        self.assertEqual(len(self.session.calls), 6)           # 1 try + 5 retries
        self.assertEqual(len(self.sleeps), 5)
        self.assertEqual(cm.exception.attempts, 6)
        self.assertEqual(self.log_rows()[-1]["cost"], 0.0)
        self.assertEqual(c.budget.reserved, 0.0)

    def test_timeout_counts_worst_case(self):
        c = self.client([FakeResp(200, exc=asyncio.TimeoutError()), FakeResp(200, ok_body(cost=0.0001))])
        r = self.run_(c.complete(req()))
        self.assertGreater(r.cost, 0.0001)                     # possibly billed timeout is counted
        self.assertAlmostEqual(c.budget.spent, r.cost)

    def test_broken_200_body_is_retried(self):
        c = self.client([FakeResp(200, {"error": {"message": "upstream"}}), FakeResp(200, ok_body())])
        self.assertEqual(self.run_(c.complete(req())).attempts, 2)


class BudgetInClientTest(ClientCase):
    def test_budget_exceeded_before_network(self):
        b = Budget(spent=1.7499)
        c = self.client([FakeResp(200, ok_body())], budget=b)
        with self.assertRaises(BudgetExceeded):
            self.run_(c.complete(req(step="C3")))
        self.assertEqual(self.session.calls, [])
        r = self.run_(c.complete(req(step="D")))               # block D may go up to $2
        self.assertFalse(r.from_cache)


class SecretTest(ClientCase):
    def test_key_never_logged(self):
        c = self.client([FakeResp(429, {}), FakeResp(401, {"error": "no auth"})])
        with self.assertLogs("teacher", level=logging.DEBUG) as logs:
            with self.assertRaises(TeacherError) as cm:
                self.run_(c.complete(req()))
        self.assertEqual(self.session.calls[0]["headers"]["Authorization"], f"Bearer {SECRET}")
        text = self.log.read_text(encoding="utf-8") + "\n".join(logs.output) + str(cm.exception) + repr(c)
        self.assertNotIn(SECRET, text)
        self.assertNotIn("Bearer", text)
        self.run_(self.client([FakeResp(200, ok_body())]).complete(req(sample_idx=1)))
        for p in list((self.tmp / "cache").rglob("*.json")) + [self.log]:
            self.assertNotIn(SECRET, p.read_text(encoding="utf-8"))


class StreamTest(ClientCase):
    def test_stream_ttft_and_usage(self):
        ticks = iter([0.0, 0.25, 1.0])
        chunks = [{"provider": "ProvA", "choices": [{"delta": {"role": "assistant", "content": ""}}]},
                  {"choices": [{"delta": {"content": "Вердикт: Риск\n"}}]},
                  {"choices": [{"delta": {"content": "Принцип: Рефакторинг"}, "finish_reason": "stop"}]},
                  {"choices": [], "usage": {"prompt_tokens": 10, "completion_tokens": 5, "cost": 0.00002}}]
        c = self.client([FakeResp(200, lines=sse(chunks))])
        c._clock = lambda: next(ticks)
        r = self.run_(c.complete(req(), stream=True))
        self.assertEqual(r.text, "Вердикт: Риск\nПринцип: Рефакторинг")
        self.assertEqual(r.ttft_s, 0.25)
        self.assertEqual(r.latency_s, 1.0)
        self.assertAlmostEqual(r.cost, 0.00002)
        self.assertTrue(self.session.calls[0]["json"]["stream"])

    def test_stream_error_chunk_retried(self):
        bad = [{"error": {"message": "overloaded"}}]
        good = [{"choices": [{"delta": {"content": "x"}}], "usage": {"cost": 0.0}}]
        c = self.client([FakeResp(200, lines=sse(bad)), FakeResp(200, lines=sse(good))])
        self.assertEqual(self.run_(c.complete(req(), stream=True)).attempts, 2)


class ManyTest(ClientCase):
    def test_complete_many_returns_failures(self):
        c = self.client([FakeResp(200, ok_body()), FakeResp(403, {})])
        out = self.run_(c.complete_many([req(sample_idx=0), req(sample_idx=1)]))
        self.assertEqual(sum(isinstance(x, TeacherError) for x in out), 1)



class ProcessLockTest(unittest.TestCase):
    def test_second_process_is_refused(self):
        import subprocess
        import sys

        from core.io import ROOT

        lock = Path(tempfile.mkdtemp()) / "t.lock"
        code = ("import sys, teacher.factory as f\n"
                "from pathlib import Path\n"
                "try:\n    f.acquire_process_lock(Path(sys.argv[1]))\nexcept f.TeacherBusy:\n    sys.exit(3)\n")
        from teacher import factory
        saved = factory._lock_fd
        factory._lock_fd = None
        try:
            factory.acquire_process_lock(lock)
            rc = subprocess.run([sys.executable, "-c", code, str(lock)], cwd=ROOT).returncode
            self.assertEqual(rc, 3)
        finally:
            factory._lock_fd.close()
            factory._lock_fd = saved
        rc = subprocess.run([sys.executable, "-c", code, str(lock)], cwd=ROOT).returncode
        self.assertEqual(rc, 0)


if __name__ == "__main__":
    unittest.main()
