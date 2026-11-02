#!/usr/bin/env bash
# usage (from Day9/): ./fuse.sh [adapter dir]   default adapters/student/best -> models/student-fused
# After fusing, check greedy equality fused == base+adapter on 10 valid prompts (eval/bench.py --check-fused).
set -euo pipefail
adapter="${1:-adapters/student/best}"
out=models/student-fused
if [ -e "$out" ]; then echo "$out exists, refusing to overwrite" >&2; exit 1; fi
../Day6/.venv/bin/mlx_lm.fuse --model ../Day8/models/qwen2.5-7b-instruct-4bit-bf16 \
  --adapter-path "$adapter" --save-path "$out"
