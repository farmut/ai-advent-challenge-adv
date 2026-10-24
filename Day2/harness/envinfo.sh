#!/usr/bin/env bash
# Печатает машинную часть env.txt: версии, флаги, sha256 профилей/settings/задач/эталонов и baseline.
#   harness/envinfo.sh > env.txt   (затем дописываются решения по smoke/fallback)
set -euo pipefail
D2="$(cd "$(dirname "$0")/.." && pwd)"
cd "$D2"
echo "date: $(date)"
echo "go: $(go version)"
echo "claude: $(claude --version)"
echo "sw_vers:"; sw_vers | sed 's/^/  /'
echo "model: claude-opus-5-5"
echo "flags: env -i HOME USER LOGNAME PATH TMPDIR TERM=dumb LANG GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly GOCACHE=<run>/.gocache GOTMPDIR=<run>/.gotmp \\"
echo "  claude -p \"\$(cat tasks/<task>.txt)\" --agent <profile> --settings Day2/profiles/<profile>.settings.json --setting-sources project --strict-mcp-config --no-session-persistence --permission-mode dontAsk --model claude-opus-5-5 --output-format stream-json --verbose --max-budget-usd <3|5>"
echo "budgets: researcher 3, tech-writer 5, bug-fixer 5, smoke 0.5"
echo "run dir: mktemp -d /tmp/d2run.XXXX, fresh git init + tag baseline, only .claude/agents/<profile>.md added"
echo "gocache mode: $(cat harness/gocache.mode 2>/dev/null || echo outside)"
echo
echo "sha256 (profiles, settings, tasks, expected):"
shasum -a 256 profiles/*.md profiles/*.json tasks/*.txt expected/* | sed 's/^/  /'
echo
echo "sha256 (profile history):"
shasum -a 256 profiles/history/* 2>/dev/null | sed 's/^/  /' || true
echo
echo "baseline src (identical to Day1/variant-b/src: $(diff -rq src ../Day1/variant-b/src >/dev/null && echo yes || echo NO)):"
find src -type f | sort | xargs shasum -a 256 | sed 's/^/  /'
