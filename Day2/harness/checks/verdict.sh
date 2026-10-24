#!/usr/bin/env bash
# Сводит CRIT-строки audit.txt и checks.log (+ ручные решения manual.txt) в verdict.txt.
# manual.txt: строки "MANUAL <ID> PASS|FAIL <обоснование>" — решения по MANUAL-пунктам после чтения транскрипта.
set -uo pipefail
OUT="$1"
crit=$(cat "$OUT/audit.txt" "$OUT/checks.log" 2>/dev/null | grep '^CRIT ')
fails=$(echo "$crit" | awk '$3=="FAIL"{print $2}' | tr '\n' ' ')
pending=$(echo "$crit" | awk '$3=="MANUAL"{print $2}' | tr '\n' ' ')
mfails=""; mdone=""
if [[ -f "$OUT/manual.txt" ]]; then
  mfails=$(awk '$1=="MANUAL" && $3=="FAIL"{print $2}' "$OUT/manual.txt" | tr '\n' ' ')
  mdone=$(awk '$1=="MANUAL"{print $2}' "$OUT/manual.txt" | tr '\n' ' ')
fi
left=""
for m in $pending; do [[ " $mdone " == *" $m "* ]] || left+="$m "; done
if [[ -n "$fails" || -n "$mfails" ]]; then
  echo "FINAL: FAIL — auto: ${fails:-none}; manual: ${mfails:-none}"
elif [[ -n "$left" ]]; then
  echo "FINAL: PENDING — auto PASS, manual not reviewed: $left"
else
  echo "FINAL: PASS (auto + manual)"
fi
if [[ -z "$fails" ]]; then echo "VERDICT auto: PASS"; else echo "VERDICT auto: FAIL — $fails"; fi
echo
echo "$crit" | awk '{printf "%-10s %-6s ", $2, $3; $1=$2=$3=""; print substr($0,4)}'
if [[ -f "$OUT/manual.txt" ]]; then
  echo
  echo "Ручная проверка (manual.txt):"
  awk '$1=="MANUAL"{printf "%-10s %-6s ", $2, $3; $1=$2=$3=""; print substr($0,4)}' "$OUT/manual.txt"
fi
