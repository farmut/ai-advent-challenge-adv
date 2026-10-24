#!/usr/bin/env bash
# Один изолированный прогон профиля.
#
#   harness/run.sh <profile> <attempt>            основной прогон: runs/<profile>/attempt-<N>
#   SMOKE_CASE=S2 PROMPT="..." BUDGET=0.5 \
#   PREPARE='echo X=1 > .env' harness/run.sh <profile> smoke
#                                                  smoke-кейс: runs/smoke/<case>
#
# Переменные (необязательные):
#   BUDGET      --max-budget-usd (по умолчанию по профилю: bug-fixer 5, researcher 3, tech-writer 5)
#   PROMPT      текст задачи (по умолчанию tasks/<task>.txt)
#   PREPARE     shell-фрагмент, выполняется в копии ПОСЛЕ baseline-коммита (только smoke)
#   GOCACHE_MODE outside|inside — где держать GOCACHE/GOTMPDIR (см. harness/gocache.mode)
#   NO_CHECKS=1 не запускать checks/render (smoke сам решает)
#   AGENT_FILE  (только smoke) подложить вместо профиля другой агент (зонд прав) с тем же settings профиля
set -euo pipefail

D2="$(cd "$(dirname "$0")/.." && pwd)"
PROFILE="${1:?profile}"
ATTEMPT="${2:?attempt number or 'smoke'}"

case "$PROFILE" in
  bug-fixer)   TASK=bugfix;   DEF_BUDGET=5 ;;
  researcher)  TASK=research; DEF_BUDGET=3 ;;
  tech-writer) TASK=writer;   DEF_BUDGET=5 ;;
  *) echo "unknown profile $PROFILE" >&2; exit 2 ;;
esac
BUDGET="${BUDGET:-$DEF_BUDGET}"

if [[ "$ATTEMPT" == smoke ]]; then
  OUT="$D2/runs/smoke/${SMOKE_CASE:?SMOKE_CASE}"
else
  OUT="$D2/runs/$PROFILE/attempt-$ATTEMPT"
fi
if [[ -e "$OUT/transcript.jsonl" ]]; then
  echo "refuse to overwrite $OUT" >&2; exit 3
fi
mkdir -p "$OUT"

PROFILE_MD="$D2/profiles/$PROFILE.md"
SETTINGS="$D2/profiles/$PROFILE.settings.json"
jq -e . "$SETTINGS" >/dev/null # невалидный settings в -p молча игнорируется — не допускаем

if [[ -n "${PROMPT:-}" ]]; then
  printf '%s\n' "$PROMPT" > "$OUT/task.txt"
else
  cp "$D2/tasks/$TASK.txt" "$OUT/task.txt"
fi

# 1. Одноразовая копия baseline вне репозитория.
RUN="$(mktemp -d /tmp/d2run.XXXX)"
RUN="$(cd "$RUN" && pwd -P)"
case "$RUN" in *ai-advent*|*ai_advent*|*Day[0-9]*) echo "bad run dir $RUN" >&2; exit 4 ;; esac
cp -R "$D2/src/." "$RUN/"
rm -f "$RUN/.env"
git -C "$RUN" init -q
git -C "$RUN" add -A
git -C "$RUN" -c user.name=baseline -c user.email=baseline@local commit -qm baseline
git -C "$RUN" tag baseline
BASE_SHA="$(git -C "$RUN" rev-parse HEAD)"
printf '.claude/\n.gocache/\n.gotmp/\n' >> "$RUN/.git/info/exclude"

# 2. Профиль агента. CLAUDE.md, .claude/settings*.json, .mcp.json не создаём.
mkdir -p "$RUN/.claude/agents"
AGENT_NAME="$PROFILE"
if [[ "$ATTEMPT" == smoke && -n "${AGENT_FILE:-}" ]]; then
  AGENT_NAME="$(sed -n 's/^name: *//p' "$AGENT_FILE" | head -n1)"
  cp "$AGENT_FILE" "$RUN/.claude/agents/$AGENT_NAME.md"
  PROFILE_MD="$AGENT_FILE"
else
  cp "$PROFILE_MD" "$RUN/.claude/agents/$PROFILE.md"
fi

if [[ "$ATTEMPT" == smoke && -n "${PREPARE:-}" ]]; then
  (cd "$RUN" && eval "$PREPARE")
fi

# 3. Кэш Go.
MODE="${GOCACHE_MODE:-$(cat "$D2/harness/gocache.mode" 2>/dev/null || echo outside)}"
if [[ "$MODE" == inside ]]; then
  GOCACHE_DIR="$RUN/.gocache"; GOTMP_DIR="$RUN/.gotmp"
else
  GOCACHE_DIR="$RUN.gocache"; GOTMP_DIR="$RUN.gotmp"
fi
mkdir -p "$GOCACHE_DIR" "$GOTMP_DIR"

GO_BIN_DIR="$(dirname "$(command -v go)")"
CLAUDE_BIN="$(command -v claude)"
SAFE_PATH="$GO_BIN_DIR:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

{
  echo "date: $(date)"
  echo "run_dir: $RUN"
  echo "baseline_sha: $BASE_SHA"
  echo "gocache_mode: $MODE ($GOCACHE_DIR)"
  echo "budget: $BUDGET"
  echo "cmd: env -i HOME=\$HOME USER=\$USER LOGNAME=\$USER PATH=$SAFE_PATH TMPDIR=\$TMPDIR TERM=dumb LANG=en_US.UTF-8 GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly GOCACHE=$GOCACHE_DIR GOTMPDIR=$GOTMP_DIR \\"
  echo "  perl -e 'alarm 1800; exec @ARGV' $CLAUDE_BIN -p \"\$(cat task.txt)\" --agent $AGENT_NAME --settings $SETTINGS --setting-sources project --strict-mcp-config --no-session-persistence --permission-mode dontAsk --model claude-opus-5-5 --output-format stream-json --verbose --max-budget-usd $BUDGET"
} > "$OUT/cmd.txt"

shasum -a 256 "$PROFILE_MD" "$SETTINGS" "$OUT/task.txt" | sed "s#$D2/##" > "$OUT/profile.sha256"

# 4. Запуск. ANTHROPIC_API_KEY и прочие переменные родителя не пробрасываются.
START=$(date +%s)
set +e
(
  cd "$RUN"
  env -i HOME="$HOME" USER="$USER" LOGNAME="$USER" PATH="$SAFE_PATH" \
    TMPDIR="${TMPDIR:-/tmp}" TERM=dumb LANG=en_US.UTF-8 \
    GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly \
    GOCACHE="$GOCACHE_DIR" GOTMPDIR="$GOTMP_DIR" \
    perl -e 'alarm 1800; exec @ARGV' \
    "$CLAUDE_BIN" -p "$(cat "$OUT/task.txt")" --agent "$AGENT_NAME" \
      --settings "$SETTINGS" --setting-sources project \
      --strict-mcp-config --no-session-persistence \
      --permission-mode dontAsk --model claude-opus-5-5 \
      --output-format stream-json --verbose --max-budget-usd "$BUDGET" \
      > "$OUT/transcript.jsonl" 2> "$OUT/stderr.log" < /dev/null
)
RC=$?
set -e
echo "exit_code: $RC" >> "$OUT/cmd.txt"
echo "wall_seconds: $(( $(date +%s) - START ))" >> "$OUT/cmd.txt"

# 5. Артефакты.
jq -c 'select(.type=="result")' "$OUT/transcript.jsonl" | tail -n1 > "$OUT/run.json" || true
jq -r '.result // ""' "$OUT/run.json" > "$OUT/answer.md" 2>/dev/null || true
git -C "$RUN" status --porcelain --untracked-files=all > "$OUT/status.txt"
git -C "$RUN" add -N . 2>/dev/null || true
git -C "$RUN" diff baseline > "$OUT/diff.patch"
git -C "$RUN" diff --name-only baseline > "$OUT/changed.txt"
rm -rf "$OUT/tree"
while IFS= read -r f; do
  [[ -f "$RUN/$f" ]] || continue
  mkdir -p "$OUT/tree/$(dirname "$f")"
  cp "$RUN/$f" "$OUT/tree/$f"
done < "$OUT/changed.txt"
echo "$RUN" > "$OUT/run_dir.txt"

if [[ "${NO_CHECKS:-}" == 1 || "$ATTEMPT" == smoke ]]; then
  echo "$OUT"
  exit 0
fi

# 6. Проверки и рендер.
"$D2/harness/checks/audit.sh" "$OUT" "$RUN" "$PROFILE" > "$OUT/audit.txt" 2>&1 || true
"$D2/harness/checks/$TASK.sh" "$OUT" "$RUN" > "$OUT/checks.log" 2>&1 || true
"$D2/harness/checks/verdict.sh" "$OUT" > "$OUT/verdict.txt" || true
go -C "$D2/harness/render" run . -in "$OUT" -title "$PROFILE attempt-$ATTEMPT" -screenshot > "$OUT/render.log" 2>&1 || true
cat "$OUT/verdict.txt"
echo "$OUT"
