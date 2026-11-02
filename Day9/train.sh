#!/usr/bin/env bash
# usage (from Day9/): ./train.sh <config name>   e.g. ./train.sh smoke | student | fallback_F1 | fallback_F2
# Validates the YAML with core/config.py (types, YAML 1.1 floats), refuses to overwrite an existing run,
# then runs the stock mlx_lm.lora trainer and keeps its stdout for select_ckpt.py (train/val loss).
set -euo pipefail
name="${1:?config name}"
PY=../Day6/.venv/bin/python
cfg="configs/${name}.yaml"
adapters=$("$PY" -c "import sys; from core.config import load_train_config as l; print(l(sys.argv[1])['adapter_path'])" "$cfg")
if [ -e "$adapters" ]; then echo "$adapters exists, refusing to overwrite" >&2; exit 1; fi
mkdir -p "results/${name}"
../Day6/.venv/bin/mlx_lm.lora -c "$cfg" 2>&1 | tee "results/${name}/train.stdout"
