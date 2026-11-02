"""Test doubles: a fake aiohttp session (no network) and a guard that records every file a block of code opens."""

from __future__ import annotations

import builtins
import contextlib
import io
import json
import logging
from collections.abc import Iterator
from pathlib import Path
from typing import Any
from unittest import mock

logging.getLogger("teacher").setLevel(logging.ERROR)  # retry warnings are expected in tests

SECRET = "sk-or-v1-TESTSECRET-0123456789abcdef"


def ok_body(text: str = "Вердикт: Риск\nПринцип: Рефакторинг\nОбоснование: А.\nРекомендация: Б.",
            cost: float = 0.0003, provider: str = "ProvA") -> dict[str, Any]:
    return {"id": "gen-1", "provider": provider,
            "choices": [{"message": {"role": "assistant", "content": text}, "finish_reason": "stop"}],
            "usage": {"prompt_tokens": 1400, "completion_tokens": 120, "cost": cost,
                      "completion_tokens_details": {"reasoning_tokens": 0}}}


class FakeResp:
    def __init__(self, status: int, body: Any = None, headers: dict[str, str] | None = None,
                 lines: list[bytes] | None = None, exc: BaseException | None = None) -> None:
        self.status, self._body, self.headers, self._lines, self.exc = status, body, headers or {}, lines, exc

    async def json(self) -> Any:
        return self._body

    async def text(self) -> str:
        return json.dumps(self._body) if self._body is not None else ""

    @property
    def content(self) -> Any:
        lines = list(self._lines or [])

        async def gen() -> Any:
            for ln in lines:
                yield ln
        return gen()


class _Ctx:
    def __init__(self, resp: FakeResp) -> None:
        self.resp = resp

    async def __aenter__(self) -> FakeResp:
        if self.resp.exc is not None:
            raise self.resp.exc
        return self.resp

    async def __aexit__(self, *a: object) -> None:
        return None


class FakeSession:
    """Returns the queued responses in order; records every request (url, json, headers)."""

    def __init__(self, responses: list[FakeResp]) -> None:
        self.responses = list(responses)
        self.calls: list[dict[str, Any]] = []

    def post(self, url: str, json: Any = None, headers: Any = None, timeout: Any = None) -> _Ctx:  # noqa: A002
        self.calls.append({"url": url, "json": json, "headers": headers})
        if not self.responses:
            raise AssertionError("unexpected network call")
        return _Ctx(self.responses.pop(0))

    async def close(self) -> None:
        return None


def sse(chunks: list[dict[str, Any]]) -> list[bytes]:
    out = [b": OPENROUTER PROCESSING\n"]
    out += [f"data: {json.dumps(c, ensure_ascii=False)}\n".encode() for c in chunks]
    return out + [b"data: [DONE]\n"]


async def no_sleep(_: float) -> None:
    return None


@contextlib.contextmanager
def record_opens() -> Iterator[list[str]]:
    """Every path opened through builtins.open / io.open (Path.open and read_text included) while active."""
    seen: list[str] = []
    real = io.open

    def spy(file: Any, *a: Any, **kw: Any) -> Any:
        if isinstance(file, (str, Path)):
            seen.append(str(Path(file).resolve()))
        return real(file, *a, **kw)

    with mock.patch.object(builtins, "open", spy), mock.patch.object(io, "open", spy):
        yield seen
