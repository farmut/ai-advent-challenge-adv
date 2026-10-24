#!/usr/bin/env bash
# Общие функции для ручного разбора попытки: source harness/checks/common.sh
#   result_json <attempt-dir>   последний event result
#   tool_uses   <attempt-dir>   все tool_use (name + input)
#   denials     <attempt-dir>   permission_denials
#   answer      <attempt-dir>   итоговый текст
#   lint_settings               общий deny и ключи безопасности во всех settings
D2="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
result_json() { jq -c 'select(.type=="result")' "$1/transcript.jsonl" | tail -n1; }
tool_uses()   { jq -c 'select(.type=="assistant") | .message.content[]? | select(.type=="tool_use") | {name, input}' "$1/transcript.jsonl"; }
denials()     { jq -c '.permission_denials[]?' "$1/run.json"; }
answer()      { cat "$1/answer.md"; }
lint_settings() {
  local ref="$D2/profiles/researcher.settings.json" rc=0
  for f in "$D2"/profiles/*.settings.json; do
    jq -e '.disableBypassPermissionsMode=="disable" and .disableAllHooks==true and .enableAllProjectMcpServers==false and .autoMemoryEnabled==false and .permissions.defaultMode=="dontAsk"' "$f" >/dev/null \
      || { echo "FAIL keys: $f"; rc=1; }
    missing=$(jq -n --slurpfile a "$ref" --slurpfile b "$f" '[ $a[0].permissions.deny[] ] - [ $b[0].permissions.deny[] ] | map(select(test("^(Edit|Write)\\(\\./\\*\\*\\)$|^Bash$")|not))')
    [[ "$missing" == "[]" ]] || { echo "FAIL common deny missing in $f: $missing"; rc=1; }
  done
  [[ $rc == 0 ]] && echo "lint-settings: OK"
  return $rc
}
if [[ "${1:-}" == lint-settings ]]; then lint_settings; fi
