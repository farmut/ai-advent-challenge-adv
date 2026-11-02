#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/bin/agile-consultant"
CONFIG="$ROOT/configs/app.yaml"
RESULTS="$ROOT/benchmarks/results-v1.md"

mkdir -p "$ROOT/benchmarks"

echo "Running benchmark for tuned model..."
"$BIN" bench --config "$CONFIG" --model tuned > "$ROOT/benchmarks/tuned.json"

echo "Running benchmark for baseline model..."
"$BIN" bench --config "$CONFIG" --model baseline > "$ROOT/benchmarks/baseline.json"

echo "Comparison complete. Results saved to $RESULTS"
