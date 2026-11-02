"""Strict YAML loading with type checks (plan §6.2; Day8 lesson "1.0e12 is a string").

PyYAML follows YAML 1.1: `5e-5` and `1.0e12` are strings, only `5.0e-5` / `1.0e+12` are floats. mlx_lm.lora
patches yaml.SafeLoader globally to accept more forms, so a config could mean different things to mlx_lm and to
our code. StrictLoader uses the pristine YAML 1.1 resolvers and every value is checked against a schema:
a float field must be a real float (not a string, not a bool), unknown keys are errors.
"""

from __future__ import annotations

import copy
from collections.abc import Callable
from pathlib import Path
from typing import Any

import yaml


class StrictLoader(yaml.SafeLoader):
    """SafeLoader with the original YAML 1.1 implicit resolvers (immune to mlx_lm's global patch)."""


StrictLoader.yaml_implicit_resolvers = copy.deepcopy(yaml.resolver.Resolver.yaml_implicit_resolvers)


class ConfigError(ValueError):
    pass


Check = Callable[[Any, str], None]


def _type_name(v: Any) -> str:
    return type(v).__name__


def is_float(v: Any, where: str) -> None:
    if isinstance(v, bool) or not isinstance(v, float):
        hint = " (YAML 1.1: write 5.0e-5 / 1.0e+12)" if isinstance(v, str) else ""
        raise ConfigError(f"{where}: expected float, got {_type_name(v)} {v!r}{hint}")


def is_int(v: Any, where: str) -> None:
    if isinstance(v, bool) or not isinstance(v, int):
        raise ConfigError(f"{where}: expected int, got {_type_name(v)} {v!r}")


def is_number(v: Any, where: str) -> None:
    if isinstance(v, bool) or not isinstance(v, (int, float)):
        raise ConfigError(f"{where}: expected number, got {_type_name(v)} {v!r}")


def is_bool(v: Any, where: str) -> None:
    if not isinstance(v, bool):
        raise ConfigError(f"{where}: expected bool, got {_type_name(v)} {v!r}")


def is_str(v: Any, where: str) -> None:
    if not isinstance(v, str):
        raise ConfigError(f"{where}: expected str, got {_type_name(v)} {v!r}")


def is_dict(v: Any, where: str) -> None:
    if not isinstance(v, dict):
        raise ConfigError(f"{where}: expected mapping, got {_type_name(v)} {v!r}")


def one_of(*values: str) -> Check:
    def check(v: Any, where: str) -> None:
        is_str(v, where)
        if v not in values:
            raise ConfigError(f"{where}: expected one of {values}, got {v!r}")
    return check


def dict_of(item: Check) -> Check:
    def check(v: Any, where: str) -> None:
        is_dict(v, where)
        for k, x in v.items():
            is_str(k, f"{where} key")
            item(x, f"{where}.{k}")
    return check


def list_of(item: Check) -> Check:
    def check(v: Any, where: str) -> None:
        if not isinstance(v, list):
            raise ConfigError(f"{where}: expected list, got {_type_name(v)}")
        for i, x in enumerate(v):
            item(x, f"{where}[{i}]")
    return check


def mapping(schema: dict[str, Check], required: tuple[str, ...] = ()) -> Check:
    def check(v: Any, where: str) -> None:
        validate(v, schema, required, where)
    return check


def validate(obj: Any, schema: dict[str, Check], required: tuple[str, ...] = (), where: str = "") -> None:
    if not isinstance(obj, dict):
        raise ConfigError(f"{where or 'config'}: expected mapping, got {_type_name(obj)}")
    unknown = sorted(set(obj) - set(schema))
    if unknown:
        raise ConfigError(f"{where or 'config'}: unknown keys {unknown}")
    missing = [k for k in required if k not in obj]
    if missing:
        raise ConfigError(f"{where or 'config'}: missing keys {missing}")
    for k, v in obj.items():
        schema[k](v, f"{where}.{k}" if where else k)


def load_yaml(path: str | Path, schema: dict[str, Check], required: tuple[str, ...] = ()) -> dict[str, Any]:
    with Path(path).open(encoding="utf-8") as f:
        obj = yaml.load(f, Loader=StrictLoader)  # noqa: S506 — StrictLoader is a SafeLoader
    validate(obj, schema, required)
    return obj


# ---------- schemas ----------

LORA_SCHEMA: dict[str, Check] = {"rank": is_int, "scale": is_float, "dropout": is_float, "keys": list_of(is_str)}
LR_SCHEDULE_SCHEMA: dict[str, Check] = {"name": is_str, "warmup": is_int, "warmup_init": is_float,
                                        "arguments": list_of(is_number)}

TRAIN_SCHEMA: dict[str, Check] = {
    "model": is_str, "train": is_bool, "fine_tune_type": is_str, "data": is_str, "adapter_path": is_str,
    "mask_prompt": is_bool, "seed": is_int, "num_layers": is_int,
    "lora_parameters": mapping(LORA_SCHEMA, ("rank", "scale", "dropout", "keys")),
    "batch_size": is_int, "iters": is_int, "learning_rate": is_float,
    "lr_schedule": mapping(LR_SCHEDULE_SCHEMA, ("name", "arguments")),
    "optimizer": is_str, "max_seq_length": is_int, "grad_checkpoint": is_bool, "grad_accumulation_steps": is_int,
    "steps_per_report": is_int, "steps_per_eval": is_int, "val_batches": is_int, "save_every": is_int,
}
TRAIN_REQUIRED = ("model", "train", "data", "adapter_path", "mask_prompt", "seed", "num_layers",
                  "lora_parameters", "batch_size", "iters", "learning_rate", "save_every")

TEACHER_SCHEMA: dict[str, Check] = {
    "backend": one_of("openrouter", "openai_compat"), "base_url": is_str, "api_key_env": is_str,
    "reasoning_body": dict_of(is_dict),
    "model": is_str, "provider_order": list_of(is_str), "url": is_str, "concurrency": is_int,
    "timeout_s": is_float, "max_retries": is_int, "backoff_base_s": is_float, "backoff_max_s": is_float,
    "prices": mapping({"input_per_m": is_float, "output_per_m": is_float}, ("input_per_m", "output_per_m")),
}
TEACHER_REQUIRED = ("model",)


def load_train_config(path: str | Path) -> dict[str, Any]:
    cfg = load_yaml(path, TRAIN_SCHEMA, TRAIN_REQUIRED)
    sched = cfg.get("lr_schedule")
    if sched and sched["arguments"] and float(sched["arguments"][0]) != cfg["learning_rate"]:
        raise ConfigError("lr_schedule.arguments[0] must equal learning_rate")
    return cfg


def load_teacher_config(path: str | Path) -> dict[str, Any]:
    return load_yaml(path, TEACHER_SCHEMA, TEACHER_REQUIRED)
