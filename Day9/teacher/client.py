"""Chat-completions client for the teacher (aiohttp), two backends (configs/teacher.yaml `backend`):

- openrouter: POST https://openrouter.ai/api/v1/chat/completions with usage {include: true} (exact usage.cost),
  reasoning {enabled: false} or {effort: low|medium|high}, provider {order: [...], allow_fallbacks: false}.
- openai_compat (GPUStack / vLLM): POST <base_url>/chat/completions, no OpenRouter fields; the reasoning level is
  switched by the per-level body extras of `reasoning_body` (e.g. off -> chat_template_kwargs {thinking: false},
  low -> reasoning_effort low). usage has no cost: spend is estimated from the token counts at the list price
  of `prices` (cost_source "list_price_estimate", reasoning tokens are completion tokens). The response model
  and system_fingerprint are logged (aliases may point to another deployment name).
- Semaphore(concurrency), total timeout per attempt, up to max_retries retries on 429 / 5xx / timeout /
  connection error / broken 200 body; exponential backoff with jitter, Retry-After is honoured.
- Disk cache (teacher/cache.py), only HTTP 200 answers are written; use_cache=False neither reads nor writes
  (cold benchmark, G4 determinism).
- Budget (teacher/budget.py): the worst case is reserved before the request; BudgetExceeded is raised before
  any network I/O. A timed-out attempt may still be billed by the provider, so its worst case is counted.
- Usage log results/teacher_usage.jsonl: one record per request. The API key is read from the environment
  (LLM_API_KEY) only, lives in a private attribute, never enters records, exceptions, logs or repr.
- stream=True uses SSE and measures TTFT (time to the first non-empty content delta).
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
import random
import ssl
import time
from collections.abc import Awaitable, Callable, Sequence
from dataclasses import asdict, dataclass, field
from email.utils import parsedate_to_datetime
from pathlib import Path
from typing import Any

from prompts import PROMPT_VERSION
from teacher.budget import Budget, Prices, worst_case_cost
from teacher.cache import TeacherCache, cache_key

log = logging.getLogger("teacher")

OPENROUTER_URL = "https://openrouter.ai/api/v1/chat/completions"
ENDPOINTS_URL = "https://openrouter.ai/api/v1/models/{model}/endpoints"
API_KEY_ENV = "LLM_API_KEY"
BACKENDS = ("openrouter", "openai_compat")
REASONING = ("off", "low", "medium", "high")
RETRY_STATUS = frozenset({408, 409, 425, 429}) | frozenset(range(500, 600))
MAX_RETRY_AFTER_S = 60.0


class TeacherError(RuntimeError):
    """A request failed for good (non-retryable status or retries exhausted). Never carries the key."""

    def __init__(self, message: str, status: int | None = None, attempts: int = 0) -> None:
        super().__init__(message)
        self.status = status
        self.attempts = attempts


class MissingApiKey(RuntimeError):
    pass


def api_key_from_env(name: str = API_KEY_ENV) -> str:
    key = os.environ.get(name, "").strip()
    if not key:
        raise MissingApiKey(f"environment variable {name} is not set")
    return key


@dataclass(frozen=True)
class TeacherConfig:
    model: str = "deepseek/deepseek-v4.1-flash-1017"
    provider_order: tuple[str, ...] = ()
    url: str = OPENROUTER_URL
    backend: str = "openrouter"
    api_key_env: str = API_KEY_ENV
    reasoning_body: dict[str, dict[str, Any]] = field(default_factory=dict)  # openai_compat: level -> body extras
    concurrency: int = 8
    timeout_s: float = 60.0
    max_retries: int = 5
    backoff_base_s: float = 1.0
    backoff_max_s: float = 30.0
    prices: Prices = Prices()

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> TeacherConfig:
        d = dict(d)
        prices = Prices(**d.pop("prices")) if "prices" in d else Prices()
        if "provider_order" in d:
            d["provider_order"] = tuple(d["provider_order"])
        if "base_url" in d:
            if "url" in d:
                raise ValueError("give either url or base_url")
            d["url"] = d.pop("base_url").rstrip("/") + "/chat/completions"
        cfg = cls(prices=prices, **d)
        if cfg.backend not in BACKENDS:
            raise ValueError(f"backend must be one of {BACKENDS}")
        return cfg

    def extra_body(self, level: str) -> dict[str, Any]:
        """Backend-specific body fields for a reasoning level (openai_compat only; part of the cache key)."""
        if self.backend == "openrouter":
            return {}
        if level not in self.reasoning_body:
            raise ValueError(f"reasoning level {level!r} is not configured in reasoning_body")
        return dict(self.reasoning_body[level])

    def endpoint(self) -> str:
        return f"{self.backend}:{self.url}"


@dataclass(frozen=True)
class TeacherRequest:
    messages: tuple[dict[str, str], ...]
    step: str                      # budget step: B, C1, C2, C3, C4, D
    temperature: float = 0.0
    max_tokens: int = 400
    reasoning: str = "off"
    seed: int | None = 42
    sample_idx: int = 0
    prompt_version: str = PROMPT_VERSION
    tag: str = ""                  # row id, for the usage log

    @classmethod
    def chat(cls, system: str, user: str, step: str, **kw: Any) -> TeacherRequest:
        return cls(messages=({"role": "system", "content": system}, {"role": "user", "content": user}),
                   step=step, **kw)

    def __post_init__(self) -> None:
        if self.reasoning not in REASONING:
            raise ValueError(f"reasoning must be one of {REASONING}")

    def prompt_chars(self) -> int:
        return sum(len(m["content"]) for m in self.messages)


@dataclass
class TeacherResult:
    text: str
    key: str
    from_cache: bool
    cost: float
    prompt_tokens: int | None = None
    completion_tokens: int | None = None
    reasoning_tokens: int | None = None
    provider: str | None = None
    response_model: str | None = None
    system_fingerprint: str | None = None
    finish_reason: str | None = None
    latency_s: float | None = None
    ttft_s: float | None = None
    attempts: int = 0
    reasoning_text: str = ""
    usage: dict[str, Any] = field(default_factory=dict)


def tls_context() -> ssl.SSLContext:
    """Verifying TLS context backed by the OS trust store (truststore) when available.

    The python.org OpenSSL bundle may lack newer public roots (GlobalSign Root R46 of the GPUStack chain), while
    curl and the OS trust them. Verification is never disabled: CERT_REQUIRED and check_hostname stay on.
    """
    try:
        import truststore

        ctx: ssl.SSLContext = truststore.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    except ImportError:
        ctx = ssl.create_default_context()
    ctx.check_hostname = True
    ctx.verify_mode = ssl.CERT_REQUIRED
    return ctx


def reasoning_param(level: str) -> dict[str, Any]:
    return {"enabled": False} if level == "off" else {"effort": level}


def build_payload(cfg: TeacherConfig, req: TeacherRequest, stream: bool = False) -> dict[str, Any]:
    if cfg.backend == "openai_compat":
        payload = {"model": cfg.model, "messages": [dict(m) for m in req.messages],
                   "temperature": req.temperature, "max_tokens": req.max_tokens}
        if req.seed is not None:
            payload["seed"] = req.seed
        clash = set(cfg.extra_body(req.reasoning)) & set(payload)
        if clash:
            raise ValueError(f"reasoning_body must not override {sorted(clash)}")
        payload |= cfg.extra_body(req.reasoning)
        if stream:
            payload |= {"stream": True, "stream_options": {"include_usage": True}}
        return payload
    if not cfg.provider_order:
        raise ValueError("provider_order must be pinned (configs/teacher.yaml) before real calls")
    payload: dict[str, Any] = {
        "model": cfg.model,
        "messages": [dict(m) for m in req.messages],
        "temperature": req.temperature,
        "max_tokens": req.max_tokens,
        "usage": {"include": True},
        "reasoning": reasoning_param(req.reasoning),
        "provider": {"order": list(cfg.provider_order), "allow_fallbacks": False},
    }
    if req.seed is not None:
        payload["seed"] = req.seed
    if stream:
        payload["stream"] = True
    return payload


def request_key(cfg: TeacherConfig, req: TeacherRequest) -> str:
    return cache_key(model=cfg.model, messages=req.messages, temperature=req.temperature,
                     max_tokens=req.max_tokens, reasoning=req.reasoning, seed=req.seed,
                     sample_idx=req.sample_idx, prompt_version=req.prompt_version, provider=cfg.provider_order,
                     endpoint=cfg.endpoint(), extra=cfg.extra_body(req.reasoning))


def retry_after_s(headers: Any) -> float | None:
    """Seconds from a Retry-After header (delta-seconds or HTTP date); None if absent or unparsable."""
    value = headers.get("Retry-After") if headers is not None else None
    if value is None:
        return None
    try:
        return max(0.0, min(float(value), MAX_RETRY_AFTER_S))
    except ValueError:
        pass
    try:
        dt = parsedate_to_datetime(value)
    except (TypeError, ValueError):
        return None
    return max(0.0, min(dt.timestamp() - time.time(), MAX_RETRY_AFTER_S))


def parse_body(body: dict[str, Any]) -> dict[str, Any] | None:
    """Extract the fields we need from a non-stream 200 body; None if the body is an error or has no choice."""
    if not isinstance(body, dict) or body.get("error") or not body.get("choices"):
        return None
    ch = body["choices"][0]
    msg = ch.get("message") or {}
    content = msg.get("content")
    if content is None:
        return None
    usage = body.get("usage") or {}
    ttft_ms = usage.get("time_to_first_token_ms")
    return {"text": content, "reasoning_text": msg.get("reasoning") or msg.get("reasoning_content") or "",
            "finish_reason": ch.get("finish_reason"), "usage": usage, "provider": body.get("provider"),
            "response_model": body.get("model"), "system_fingerprint": body.get("system_fingerprint"),
            "ttft_s": ttft_ms / 1000 if isinstance(ttft_ms, (int, float)) and ttft_ms > 0 else None,
            "ttft_source": "server" if isinstance(ttft_ms, (int, float)) and ttft_ms > 0 else None}


class _Retry(Exception):
    def __init__(self, reason: str, status: int | None = None, delay: float | None = None,
                 maybe_billed: bool = False, cost: float = 0.0) -> None:
        super().__init__(reason)
        self.reason, self.status, self.delay, self.maybe_billed, self.cost = reason, status, delay, maybe_billed, cost


class UsageLog:
    """Append-only JSONL. `secrets` are redacted from every record as a second line of defence."""

    def __init__(self, path: str | Path, secrets: Sequence[str] = ()) -> None:
        self.path = Path(path)
        self._secrets = tuple(s for s in secrets if s)

    def append(self, record: dict[str, Any]) -> None:
        line = json.dumps(record, ensure_ascii=False)
        for s in self._secrets:
            line = line.replace(s, "***")
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with self.path.open("a", encoding="utf-8") as f:
            f.write(line + "\n")


class TeacherClient:
    def __init__(self, cfg: TeacherConfig, *, cache: TeacherCache, budget: Budget, usage_log: str | Path,
                 api_key: str | None = None, session: Any = None, use_cache: bool = True,
                 sleep: Callable[[float], Awaitable[None]] = asyncio.sleep, rng: random.Random | None = None,
                 clock: Callable[[], float] = time.perf_counter) -> None:
        self.cfg = cfg
        self.cache = cache
        self.budget = budget
        self.use_cache = use_cache
        self.__key = api_key  # name-mangled; never logged
        self._session = session
        self._own_session = False
        self._sleep = sleep
        self._rng = rng or random.Random(0)
        self._clock = clock
        self._sem = asyncio.Semaphore(cfg.concurrency)
        self.usage = UsageLog(usage_log, secrets=[api_key] if api_key else [])

    def __repr__(self) -> str:
        return (f"TeacherClient(backend={self.cfg.backend!r}, model={self.cfg.model!r}, "
                f"provider={list(self.cfg.provider_order)}, cache={self.use_cache})")

    async def __aenter__(self) -> TeacherClient:
        if self._session is None:
            import aiohttp

            self._session = aiohttp.ClientSession(connector=aiohttp.TCPConnector(ssl=tls_context()))
            self._own_session = True
        return self

    async def __aexit__(self, *exc: object) -> None:
        if self._own_session and self._session is not None:
            await self._session.close()
            self._session = None

    def _headers(self) -> dict[str, str]:
        key = self.__key or api_key_from_env(self.cfg.api_key_env)
        return {"Authorization": f"Bearer {key}", "Content-Type": "application/json",
                "X-Title": "ai-advent-day9-distill"}

    def backoff_s(self, attempt: int) -> float:
        base = min(self.cfg.backoff_max_s, self.cfg.backoff_base_s * 2 ** attempt)
        return base * (0.5 + 0.5 * self._rng.random())

    # ---------- one HTTP attempt ----------

    async def _attempt(self, payload: dict[str, Any], stream: bool, t0: float) -> dict[str, Any]:
        import aiohttp

        timeout = aiohttp.ClientTimeout(total=self.cfg.timeout_s)
        try:
            async with self._session.post(self.cfg.url, json=payload, headers=self._headers(),
                                          timeout=timeout) as resp:
                status = resp.status
                if status != 200:
                    try:
                        detail = (await resp.text())[:300]
                    except Exception:  # noqa: BLE001 — the body is only for the message
                        detail = ""
                    if status in RETRY_STATUS:
                        raise _Retry(f"http {status}", status, retry_after_s(resp.headers))
                    raise TeacherError(f"http {status}: {detail}", status)
                if stream:
                    return await self._read_stream(resp, t0)
                parsed = parse_body(await resp.json())
                if parsed is None:
                    raise _Retry("bad 200 body", 200)
                return parsed
        except (TimeoutError, asyncio.TimeoutError) as e:
            raise _Retry("timeout", None, None, maybe_billed=True) from e
        except aiohttp.ClientError as e:
            raise _Retry(f"connection: {type(e).__name__}") from e

    async def _read_stream(self, resp: Any, t0: float) -> dict[str, Any]:
        parts: list[str] = []
        reasoning: list[str] = []
        usage: dict[str, Any] = {}
        provider = finish = model = fingerprint = None
        ttft = None
        async for raw in resp.content:
            line = raw.decode("utf-8", "replace").strip() if isinstance(raw, bytes) else str(raw).strip()
            if not line.startswith("data:"):
                continue  # SSE comments (": OPENROUTER PROCESSING") and blank lines
            data = line[5:].strip()
            if data == "[DONE]":
                break
            chunk = json.loads(data)
            if chunk.get("error"):
                raise _Retry("stream error", 200)
            provider = chunk.get("provider") or provider
            model = chunk.get("model") or model
            fingerprint = chunk.get("system_fingerprint") or fingerprint
            usage = chunk.get("usage") or usage
            for ch in chunk.get("choices") or []:
                delta = ch.get("delta") or {}
                if delta.get("reasoning") or delta.get("reasoning_content"):
                    reasoning.append(delta.get("reasoning") or delta["reasoning_content"])
                if delta.get("content"):
                    if ttft is None:
                        ttft = self._clock() - t0
                    parts.append(delta["content"])
                finish = ch.get("finish_reason") or finish
        if not parts and not usage:
            raise _Retry("empty stream", 200)
        return {"text": "".join(parts), "reasoning_text": "".join(reasoning), "finish_reason": finish,
                "usage": usage, "provider": provider, "ttft_s": ttft, "ttft_source": "client_stream",
                "response_model": model, "system_fingerprint": fingerprint}

    # ---------- public API ----------

    async def complete(self, req: TeacherRequest, stream: bool = False) -> TeacherResult:
        key = request_key(self.cfg, req)
        base_rec = {"ts": round(time.time(), 3), "step": req.step, "tag": req.tag, "key": key,
                    "model": self.cfg.model, "temperature": req.temperature, "reasoning": req.reasoning,
                    "sample_idx": req.sample_idx, "stream": stream}
        if self.use_cache and (hit := self.cache.get(key)) is not None:
            res = TeacherResult(**{**hit["result"], "from_cache": True, "cost": 0.0, "attempts": 0})
            self.usage.append({**base_rec, "from_cache": True, "cost": 0.0, "status": 200,
                               "provider": res.provider, "prompt_tokens": res.prompt_tokens,
                               "completion_tokens": res.completion_tokens,
                               "reasoning_tokens": res.reasoning_tokens, "cached_cost": hit["result"]["cost"]})
            return res

        worst = worst_case_cost(req.prompt_chars(), req.max_tokens, self.cfg.prices)
        rid = self.budget.reserve(worst, req.step)  # BudgetExceeded before any I/O
        payload = build_payload(self.cfg, req, stream)
        errors: list[str] = []
        assumed = 0.0
        async with self._sem:
            for attempt in range(self.cfg.max_retries + 1):
                t0 = self._clock()  # latency/TTFT of the successful attempt only (retries are logged apart)
                try:
                    out = await self._attempt(payload, stream, t0)
                except _Retry as r:
                    errors.append(r.reason)
                    if r.maybe_billed:
                        assumed += worst
                    if attempt == self.cfg.max_retries:
                        break
                    delay = r.delay if r.delay is not None else self.backoff_s(attempt)
                    log.warning("teacher retry %d/%d (%s), sleep %.1fs", attempt + 1, self.cfg.max_retries,
                                r.reason, delay)
                    await self._sleep(delay)
                    continue
                except TeacherError as e:
                    self.budget.commit(rid, assumed)
                    self.usage.append({**base_rec, "from_cache": False, "cost": assumed,
                                       "cost_source": "assumed_timeout" if assumed else "none",
                                       "status": e.status, "attempt": attempt + 1, "errors": errors + [str(e)]})
                    e.attempts = attempt + 1
                    raise
                latency = self._clock() - t0
                return self._success(req, key, out, attempt + 1, latency, rid, assumed, errors, base_rec)
        self.budget.commit(rid, assumed)
        self.usage.append({**base_rec, "from_cache": False, "cost": assumed,
                           "cost_source": "assumed_timeout" if assumed else "none", "status": None,
                           "attempt": self.cfg.max_retries + 1, "errors": errors})
        raise TeacherError(f"retries exhausted: {errors[-1] if errors else '?'}", None, self.cfg.max_retries + 1)

    def _success(self, req: TeacherRequest, key: str, out: dict[str, Any], attempts: int, latency: float,
                 rid: int, assumed: float, errors: list[str], base_rec: dict[str, Any]) -> TeacherResult:
        usage = out.get("usage") or {}
        pt, ct = usage.get("prompt_tokens"), usage.get("completion_tokens")
        rt = (usage.get("completion_tokens_details") or {}).get("reasoning_tokens")
        if usage.get("cost") is not None:
            cost, source = float(usage["cost"]), "usage"
        elif pt is not None and ct is not None:
            cost, source = self.cfg.prices.cost(int(pt), int(ct)), "list_price_estimate"
        else:
            cost, source = worst_case_cost(req.prompt_chars(), req.max_tokens, self.cfg.prices), "assumed_worst"
        total = cost + assumed
        self.budget.commit(rid, total)
        res = TeacherResult(text=out["text"], key=key, from_cache=False, cost=round(total, 8), prompt_tokens=pt,
                            completion_tokens=ct, reasoning_tokens=rt, provider=out.get("provider"),
                            response_model=out.get("response_model"),
                            system_fingerprint=out.get("system_fingerprint"),
                            finish_reason=out.get("finish_reason"), latency_s=round(latency, 4),
                            ttft_s=round(out["ttft_s"], 4) if out.get("ttft_s") is not None else None,
                            attempts=attempts, reasoning_text=out.get("reasoning_text") or "", usage=usage)
        if self.use_cache:
            self.cache.put(key, {"status": 200, "request": {"model": self.cfg.model, "step": req.step,
                                                            "messages": list(req.messages),
                                                            "temperature": req.temperature,
                                                            "max_tokens": req.max_tokens,
                                                            "reasoning": req.reasoning, "seed": req.seed,
                                                            "endpoint": self.cfg.endpoint(),
                                                            "extra": self.cfg.extra_body(req.reasoning),
                                                            "sample_idx": req.sample_idx,
                                                            "prompt_version": req.prompt_version},
                                 "result": asdict(res)})
        self.usage.append({**base_rec, "from_cache": False, "status": 200, "cost": round(total, 8),
                           "cost_source": source, "prompt_tokens": pt, "completion_tokens": ct,
                           "reasoning_tokens": rt, "provider": res.provider, "latency_s": res.latency_s,
                           "ttft_s": res.ttft_s, "ttft_source": out.get("ttft_source"), "attempt": attempts,
                           "errors": errors, "backend": self.cfg.backend, "response_model": res.response_model,
                           "system_fingerprint": res.system_fingerprint,
                           "finish_reason": res.finish_reason})
        return res

    async def complete_many(self, reqs: Sequence[TeacherRequest], stream: bool = False
                            ) -> list[TeacherResult | BaseException]:
        """All requests concurrently (bounded by the semaphore); failures are returned, not raised."""
        return list(await asyncio.gather(*(self.complete(r, stream) for r in reqs), return_exceptions=True))


async def list_endpoints(model: str, session: Any) -> list[dict[str, Any]]:
    """Free metadata call (no completion, no cost): providers serving `model`, for pinning provider_order."""
    async with session.get(ENDPOINTS_URL.format(model=model)) as resp:
        body = await resp.json()
    return list((body.get("data") or {}).get("endpoints") or [])
