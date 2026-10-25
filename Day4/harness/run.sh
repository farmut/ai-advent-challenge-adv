#!/usr/bin/env bash
# run.sh <feature|bugfix> <model> <context> <temperature> <top_p> <label>
# One isolated run of the IDE agent (codecompanion + Day4/nvim/ai.lua) on a fresh workspace:
#   feature - empty repo, Day1 prompt (generate the weather bot)
#   bugfix  - copy of Day2/src, Day2 bug-fixer profile + bugfix task
# The model is (re)loaded into LM Studio with <context>. nvim runs under Seatbelt (sandbox.sb).
# Artifacts -> Day4/runs/<task>/<label>/, then the Day1/Day2 checks run on the result.
set -euo pipefail

[[ $# -eq 6 ]] || { echo "usage: $0 <feature|bugfix> <model> <context> <temperature> <top_p> <label>" >&2; exit 2; }
task=$1 model=$2 ctx=$3 temp=$4 top_p=$5 label=$6

here="$(cd "$(dirname "$0")" && pwd)"
d4="$(dirname "$here")"
root="$(dirname "$d4")"
out="$d4/runs/$task/$label"
max_wall_s=${BENCH_MAX_WALL_S:-2700}

[[ -e $out ]] && { echo "exists: $out" >&2; exit 2; }
mkdir -p "$out"

run=$(mktemp -d "${TMPDIR:-/tmp}/d4run.XXXX")
run=$(cd "$run" && pwd -P)
ws="$run/ws"
mkdir -p "$ws" "$run/tmp" "$ws/.ai"

case $task in
  feature)
    task_file="$d4/tasks/feature.txt"
    system_file=""
    ;;
  bugfix)
    task_file="$d4/tasks/agent-bugfix.txt"
    system_file="$d4/prompts/bug-fixer.md"
    cp -R "$root/Day2/src/." "$ws/"
    ;;
  *) echo "unknown task: $task" >&2; exit 2 ;;
esac
cp "$d4/.ai/system-prompt.md" "$ws/.ai/system-prompt.md"

git -C "$ws" init -q
printf '.ai/\n.gocache/\n.gotmp/\n' >>"$ws/.git/info/exclude"
git -C "$ws" add -A
git -C "$ws" -c user.name=bench -c user.email=bench@local commit -qm baseline --allow-empty
git -C "$ws" tag baseline

# Model with the requested context; the FIM model is unloaded too, memory goes to the KV cache
lms unload --all >/dev/null 2>&1 || true
load_start=$(date +%s)
lms load "$model" -c "$ctx" -y --identifier "$model" >"$out/lms-load.log" 2>&1
load_s=$(( $(date +%s) - load_start ))

cat >"$out/cmd.txt" <<EOF
task=$task model=$model context=$ctx temperature=$temp top_p=$top_p reasoning=${AI_REASONING:-default} max_tokens=${AI_MAX_TOKENS:-preset} label=$label
run=$run max_wall_s=$max_wall_s lms_load_s=$load_s
sandbox-exec -f Day4/harness/sandbox.sb -D RUN=<run> -D HOME=\$HOME \\
  env -i HOME PATH TERM=dumb LANG GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly GOCACHE=<run>/ws/.gocache GOTMPDIR=<run>/ws/.gotmp TMPDIR=<run>/tmp \\
      AI_BASE_URL=<proxy> AI_CHAT_MODEL AI_CONTEXT_WINDOW AI_TEMPERATURE AI_TOP_P BENCH_TASK BENCH_SYSTEM BENCH_OUT BENCH_MAX_WALL_S \\
  nvim --headless -i NONE -n -c "luafile Day4/harness/driver.lua"   (cwd <run>/ws)
EOF
shasum -a 256 "$task_file" ${system_file:+"$system_file"} "$d4/.ai/system-prompt.md" "$d4/nvim/ai.lua" "$here/driver.lua" \
  | sed "s|$root/||" >"$out/inputs.sha256"

mkdir -p "$ws/.gocache" "$ws/.gotmp" "$run/out"

# Logging proxy in front of LM Studio: exact request bodies, usage and timings per request
proxy_port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
python3 -I "$here/proxy.py" "$proxy_port" "$out/requests.jsonl" &
proxy_pid=$!
trap 'kill $proxy_pid 2>/dev/null || true' EXIT
for _ in $(seq 50); do curl -s -o /dev/null "http://127.0.0.1:$proxy_port/v1/models" && break; sleep 0.1; done
start=$(date +%s)
set +e
(
  cd "$ws"
  sandbox-exec -f "$here/sandbox.sb" -D RUN="$run" -D HOME="$HOME" \
    env -i HOME="$HOME" USER="$USER" PATH="$PATH" TERM=dumb LANG="${LANG:-en_US.UTF-8}" \
      GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly GOCACHE="$ws/.gocache" GOTMPDIR="$ws/.gotmp" TMPDIR="$run/tmp" \
      AI_BASE_URL="http://127.0.0.1:$proxy_port" AI_CHAT_MODEL="$model" AI_CONTEXT_WINDOW="$ctx" AI_TEMPERATURE="$temp" AI_TOP_P="$top_p" AI_REASONING="${AI_REASONING:-}" AI_MAX_TOKENS="${AI_MAX_TOKENS:-}" \
      BENCH_TASK="$task_file" BENCH_SYSTEM="$system_file" BENCH_OUT="$run/out" BENCH_MAX_WALL_S="$max_wall_s" \
      perl -e "alarm $((max_wall_s + 120)); exec @ARGV" \
      nvim --headless -i NONE -n -c "luafile $here/driver.lua"
) >"$out/nvim.log" 2>&1
rc=$?
set -e
echo "nvim_rc=$rc wall_s=$(( $(date +%s) - start ))" >>"$out/cmd.txt"
cp -R "$run/out/." "$out/"
kill $proxy_pid 2>/dev/null || true

# What changed against the baseline (untracked files included, .ai/.gocache/.gotmp excluded)
git -C "$ws" add -A
git -C "$ws" diff --cached baseline --binary >"$out/diff.patch"
git -C "$ws" diff --cached baseline --name-only >"$out/changed.txt"
git -C "$ws" status --porcelain >"$out/status.txt"
rsync -a --exclude .git --exclude .gocache --exclude .gotmp --exclude .ai "$ws/" "$out/tree/"

# Checks reuse Day1/Day2 tooling unchanged
case $task in
  feature)
    mkdir -p "$out/check"
    ln -sfn ../tree "$out/check/src"
    printf '{"note":"local run, see ../meta.json"}\n' >"$out/check/run.json"
    METRICS_SCRATCH="$run/metrics" "$root/Day1/compare/metrics.sh" "$out/check/src" "$out/metrics.txt" || true
    mkdir -p "$run/bb"
    (cd "$root" && perl -e 'alarm 600; exec @ARGV' go -C Day1/compare/harness run . -variant b -src "$out/tree" \
      -compare "$run/bb" -bindir "$run/bbbin" -no-docker) >"$out/blackbox.log" 2>&1 || true
    [[ -f $run/bb/transcripts/b.json ]] && cp "$run/bb/transcripts/b.json" "$out/blackbox.json"
    [[ -f $run/bb/transcripts/b-bot.log ]] && cp "$run/bb/transcripts/b-bot.log" "$out/blackbox-bot.log"
    ;;
  bugfix)
    "$root/Day2/harness/checks/bugfix.sh" "$out" "$out/tree" >"$out/checks.log" 2>&1 || true
    ;;
esac

python3 -I "$here/summarize.py" "$out" | tee "$out/summary.txt"
gzip -9 "$out/requests.jsonl"
echo "run dir kept: $run"
