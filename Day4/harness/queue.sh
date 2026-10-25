#!/usr/bin/env bash
# queue.sh <file>: runs run.sh for each line "<env assignments...> -- <run.sh args>" sequentially.
set -uo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
while IFS= read -r line; do
  [[ -z $line || $line == \#* ]] && continue
  envs=${line%% -- *}; args=${line#* -- }
  echo "=== $(date +%T) $line"
  env $envs "$here/run.sh" $args 2>&1 | grep -E "^(run:|status=|  B[0-9]|  metrics|  blackbox|changed_files|tools=)" 
done <"$1"
