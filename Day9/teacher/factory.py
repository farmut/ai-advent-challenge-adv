"""The one place that wires a real TeacherClient: configs/teacher.yaml, cache/teacher, budget from the usage log,
key from the env variable named by `api_key_env` (default LLM_API_KEY). Nothing is sent until a request is awaited.

The budget is restored from the usage log at client creation, so two processes calling the teacher at the same
time could both see the same remaining money. An exclusive lock on results/.teacher.lock (held for the life of
the process) makes that impossible.
"""

from __future__ import annotations

import fcntl
from typing import IO

from core.config import load_teacher_config
from core.io import ROOT, read_json
from teacher.budget import Budget, LIMIT_USD, SOFT_LIMIT_RATIO
from teacher.cache import TeacherCache
from teacher.client import TeacherClient, TeacherConfig, api_key_from_env

USAGE_LOG = ROOT / "results/teacher_usage.jsonl"
PREREG = ROOT / "results/prereg.json"
TEACHER_CFG = ROOT / "configs/teacher.yaml"
CACHE_DIR = ROOT / "cache/teacher"
LOCK = ROOT / "results/.teacher.lock"
_lock_fd: IO[str] | None = None


class TeacherBusy(RuntimeError):
    pass


def acquire_process_lock(path=LOCK) -> None:
    """Exclusive, non-blocking, re-entrant within the process; released by the OS when the process exits."""
    global _lock_fd
    if _lock_fd is not None:
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    fd = open(path, "w", encoding="utf-8")  # noqa: SIM115 — kept open on purpose
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        fd.close()
        raise TeacherBusy(f"another process holds {path}: one teacher process at a time (budget)") from None
    _lock_fd = fd


def load_config() -> TeacherConfig:
    return TeacherConfig.from_dict(load_teacher_config(TEACHER_CFG))


def make_budget(prereg_path=PREREG, usage_log=USAGE_LOG) -> Budget:
    data = read_json(prereg_path) if prereg_path.exists() else {}
    limit = next((a["budget_override"]["limit_usd"] for a in reversed(data.get("amendments", []))
                  if "budget_override" in a), LIMIT_USD)
    return Budget(limit=limit, soft_limit=limit * SOFT_LIMIT_RATIO, spent=Budget.spent_in_log(usage_log))


def make_client(use_cache: bool = True) -> TeacherClient:
    acquire_process_lock()
    cfg = load_config()
    return TeacherClient(cfg, cache=TeacherCache(CACHE_DIR), budget=make_budget(),
                         usage_log=USAGE_LOG, api_key=api_key_from_env(cfg.api_key_env), use_cache=use_cache)
