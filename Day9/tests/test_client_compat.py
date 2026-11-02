"""openai_compat backend (GPUStack / vLLM): payload, list-price cost estimate, cache key, logging, key handling."""

import os
import unittest
from unittest import mock

from teacher.budget import Budget, Prices
from teacher.client import TeacherClient, TeacherConfig, TeacherError, build_payload, request_key
from tests._helpers import SECRET, FakeResp
from tests.test_client import ClientCase, req

OFF = {"chat_template_kwargs": {"thinking": False}}
CFG = TeacherConfig.from_dict({"backend": "openai_compat", "base_url": "https://gs.example/v1/",
                               "api_key_env": "GPUSTACK_API_KEY", "model": "deepseek-v4.1-flash",
                               "reasoning_body": {"off": OFF, "low": {"reasoning_effort": "low"}},
                               "prices": {"input_per_m": 0.15, "output_per_m": 0.60}})


def vllm_body(text="**Вердикт:** Риск\nПринцип: Рефакторинг\nОбоснование: А.\nРекомендация: Б.", rt=0):
    return {"id": "chatcmpl-1", "model": "deepseek-v4-flash", "system_fingerprint": "vllm-0.32.0-tp2",
            "choices": [{"message": {"role": "assistant", "content": text, "reasoning": "думаю" if rt else None},
                         "finish_reason": "stop"}],
            "usage": {"prompt_tokens": 1000, "completion_tokens": 200, "time_to_first_token_ms": 150,
                      "completion_tokens_details": {"reasoning_tokens": rt}}}


class CompatPayloadTest(unittest.TestCase):
    def test_url_and_no_openrouter_fields(self):
        self.assertEqual(CFG.url, "https://gs.example/v1/chat/completions")
        p = build_payload(CFG, req(reasoning="off"))
        for k in ("provider", "usage", "reasoning"):
            self.assertNotIn(k, p)
        self.assertEqual(p["chat_template_kwargs"], {"thinking": False})
        self.assertEqual(p["model"], "deepseek-v4.1-flash")
        self.assertEqual(p["seed"], 42)
        self.assertEqual(build_payload(CFG, req(reasoning="low"))["reasoning_effort"], "low")
        s = build_payload(CFG, req(), stream=True)
        self.assertEqual(s["stream_options"], {"include_usage": True})

    def test_unconfigured_level_and_clash_refused(self):
        with self.assertRaises(ValueError):
            build_payload(CFG, req(reasoning="high"))
        bad = TeacherConfig.from_dict({"backend": "openai_compat", "url": "u", "reasoning_body": {"off": {"seed": 1}}})
        with self.assertRaises(ValueError):
            build_payload(bad, req(reasoning="off"))
        with self.assertRaises(ValueError):
            TeacherConfig.from_dict({"backend": "other"})
        with self.assertRaises(ValueError):
            TeacherConfig.from_dict({"url": "a", "base_url": "b"})

    def test_cache_key_depends_on_extra_body_and_endpoint(self):
        other = TeacherConfig.from_dict({**{"backend": "openai_compat", "base_url": "https://gs.example/v1",
                                            "model": "deepseek-v4.1-flash"},
                                         "reasoning_body": {"off": {"reasoning_effort": "none"}}})
        self.assertNotEqual(request_key(CFG, req(reasoning="off")), request_key(other, req(reasoning="off")))
        moved = TeacherConfig.from_dict({"backend": "openai_compat", "base_url": "https://other/v1",
                                         "model": "deepseek-v4.1-flash", "reasoning_body": {"off": OFF}})
        self.assertNotEqual(request_key(CFG, req(reasoning="off")), request_key(moved, req(reasoning="off")))


class TlsTest(unittest.TestCase):
    def test_tls_context_verifies(self):
        import ssl

        from teacher.client import tls_context

        ctx = tls_context()
        self.assertEqual(ctx.verify_mode, ssl.CERT_REQUIRED)
        self.assertTrue(ctx.check_hostname)


class CompatClientTest(ClientCase):
    def test_list_price_estimate_and_logged_fields(self):
        c = self.client([FakeResp(200, vllm_body(rt=150))], cfg=CFG)
        r = self.run_(c.complete(req(reasoning="low")))
        self.assertAlmostEqual(r.cost, Prices(0.15, 0.60).cost(1000, 200))
        self.assertEqual((r.response_model, r.system_fingerprint), ("deepseek-v4-flash", "vllm-0.32.0-tp2"))
        self.assertEqual(r.reasoning_tokens, 150)
        self.assertAlmostEqual(r.ttft_s, 0.15)
        row = self.log_rows()[-1]
        self.assertEqual(row["cost_source"], "list_price_estimate")
        self.assertEqual(row["response_model"], "deepseek-v4-flash")
        self.assertEqual(row["system_fingerprint"], "vllm-0.32.0-tp2")
        self.assertEqual(row["ttft_source"], "server")
        self.assertAlmostEqual(Budget.spent_in_log(self.log), r.cost)       # the estimate counts in the budget
        sent = self.session.calls[0]
        self.assertEqual(sent["url"], "https://gs.example/v1/chat/completions")
        self.assertNotIn("provider", sent["json"])
        hit = self.run_(self.client([], cfg=CFG).complete(req(reasoning="low")))
        self.assertTrue(hit.from_cache)
        self.assertEqual(hit.response_model, "deepseek-v4-flash")

    def test_key_from_configured_env_never_logged(self):
        with mock.patch.dict(os.environ, {"GPUSTACK_API_KEY": SECRET}, clear=True):
            c = TeacherClient(CFG, cache=self.cache, budget=Budget(), usage_log=self.log, session=None)
            self.assertEqual(c._headers()["Authorization"], f"Bearer {SECRET}")
            c = self.client([FakeResp(401, {"error": "nope"})], cfg=CFG)
            with self.assertRaises(TeacherError) as cm:
                self.run_(c.complete(req(reasoning="off")))
        text = self.log.read_text(encoding="utf-8") + str(cm.exception) + repr(c)
        self.assertNotIn(SECRET, text)


if __name__ == "__main__":
    unittest.main()
