#!/usr/bin/env bash
# metrics.sh <src-dir> <out-file>
# Собирает метрики качества Go-проекта: build, vet, gofmt, golangci-lint, тесты с -race
# и покрытием, LOC, зависимости, граф импортов, grep-чеклист, данные прогона из ../run.json.
# Не останавливается на падениях (нет set -e): каждая секция пишет свой статус.
# Файлы в <src-dir> не создаются: coverprofile и кэши уходят в METRICS_SCRATCH.
set -uo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <src-dir> <out-file>" >&2
  exit 2
fi

src=$(cd "$1" && pwd) || { echo "no such dir: $1" >&2; exit 2; }
out=$2
label=$(basename "$(dirname "$src")")   # variant-a / variant-b
scratch=${METRICS_SCRATCH:-${TMPDIR:-/tmp}/day1-metrics}
mkdir -p "$scratch"
cover="$scratch/cover-$label.out"
run_json="$(dirname "$src")/run.json"

exec >"$out" 2>&1

section() { printf '\n## %s\n' "$1"; }
# prodgo/testgo: списки .go файлов без/только _test.go (пути относительно src)
prodgo() { (cd "$src" && find . -name '*.go' ! -name '*_test.go' | sort); }
testgo() { (cd "$src" && find . -name '*_test.go' | sort); }
# count <pattern> [all]: число вхождений (grep -o) в prod-коде или во всём коде
count() {
  local pat=$1 scope=${2:-prod} files
  if [[ $scope == all ]]; then files=$( (cd "$src" && find . -name '*.go' | sort)); else files=$(prodgo); fi
  [[ -z $files ]] && { echo 0; return; }
  # строки-комментарии не считаются
  (cd "$src" && echo "$files" | xargs grep -hE -- "$pat" 2>/dev/null | grep -vE '^[[:space:]]*//' | grep -oE -- "$pat" | wc -l | tr -d ' ')
}
# where <pattern>: file:line вхождений в prod-коде
where() { (cd "$src" && prodgo | xargs grep -nE -- "$1" 2>/dev/null | sed 's/^\.\///' | cut -c1-160); }

echo "# metrics: $label"
echo "src: $src"
echo "date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "go: $(go version)"

section "go.mod"
grep -E '^(module|go|toolchain) ' "$src/go.mod" 2>/dev/null || echo "go.mod: missing"
echo "go.sum: $([[ -f $src/go.sum ]] && echo present || echo absent)"

section "build"
if go -C "$src" build -o /dev/null ./... ; then echo "build: PASS"; else echo "build: FAIL"; fi

section "vet"
vet_out=$(go -C "$src" vet ./... 2>&1); vet_rc=$?
echo "vet: $([[ $vet_rc -eq 0 ]] && echo PASS || echo FAIL) (rc=$vet_rc)"
[[ -n $vet_out ]] && echo "$vet_out"

section "gofmt"
fmt_out=$(cd "$src" && gofmt -l .)
echo "gofmt files needing format: $(printf '%s' "$fmt_out" | grep -c . || true)"
[[ -n $fmt_out ]] && echo "$fmt_out"

section "golangci-lint"
# GOLANGCI_LINT позволяет подставить бинарь, собранный той же версией Go, что и тулчейн
# (golangci-lint, собранный go1.25, падает на stdlib go1.26).
lint_bin=${GOLANGCI_LINT:-$(command -v golangci-lint || true)}
lint_issue_re='^[^ ]+\.go:[0-9]+(:[0-9]+)?: '
run_lint() {  # run_lint <title> [extra flags...]
  local title=$1; shift
  local lint_out lint_rc
  lint_out=$(cd "$src" && "$lint_bin" run --output.text.path=stdout --show-stats=false "$@" ./... 2>&1); lint_rc=$?
  echo "### $title (rc=$lint_rc)"
  echo "issues: $(printf '%s\n' "$lint_out" | grep -cE "$lint_issue_re" || true)"
  if printf '%s' "$lint_out" | grep -q '^panic:'; then
    echo "lint crashed:"; printf '%s\n' "$lint_out" | grep -m3 -E '^(panic|Error|level=error)'
  else
    printf '%s\n' "$lint_out" | grep -E "$lint_issue_re" | sed 's/^/  /' | head -80
  fi
}
if [[ -n $lint_bin ]]; then
  echo "bin: $lint_bin"
  echo "version: $("$lint_bin" --version 2>&1 | head -1)"
  run_lint "default linters (errcheck govet ineffassign staticcheck unused)"
  run_lint "extended (+revive gosec errorlint bodyclose noctx gocritic misspell)" \
    --enable=revive,gosec,errorlint,bodyclose,noctx,gocritic,misspell
else
  echo "golangci-lint: not installed"
fi

section "test (-race -count=1)"
test_out=$(go -C "$src" test -race -count=1 -coverprofile="$cover" ./... 2>&1); test_rc=$?
echo "test: $([[ $test_rc -eq 0 ]] && echo PASS || echo FAIL) (rc=$test_rc)"
echo "$test_out"

section "coverage (go tool cover -func)"
if [[ -s $cover ]]; then
  cov=$(go -C "$src" tool cover -func="$cover" 2>&1)
  echo "total: $(echo "$cov" | awk '/^total:/ {print $NF}')"
  echo "per-package (statement-weighted, from coverprofile):"
  awk -F'[: ,]+' 'NR>1 {
      file=$1; n=split(file,p,"/"); pkg=p[1]; for(i=2;i<n;i++) pkg=pkg"/"p[i];
      stmts[pkg]+=$(NF-1); if ($NF>0) hit[pkg]+=$(NF-1)
    } END { for (k in stmts) printf "  %-55s %6.1f%%\n", k, (stmts[k]?100*hit[k]/stmts[k]:0) }' "$cover" | sort
  echo "per-func:"
  echo "$cov" | sed 's/^/  /'
else
  echo "coverprofile missing"
fi

section "tests inventory"
echo "_test.go files: $(testgo | grep -c . || true)"
echo "func Test*:     $(count '^func Test[A-Z_]' all)"
echo "t.Run(:         $(count 't\.Run\(' all)"
echo "func Benchmark: $(count '^func Benchmark' all)"
echo "func Fuzz:      $(count '^func Fuzz' all)"
echo "func Example:   $(count '^func Example' all)"

section "size"
loc() { [[ -z $1 ]] && { echo 0; return; }; (cd "$src" && echo "$1" | xargs cat | wc -l | tr -d ' '); }
echo "files (all, excl. .git): $(cd "$src" && find . -type f ! -path './.git/*' | wc -l | tr -d ' ')"
echo "go files prod: $(prodgo | grep -c . || true)  test: $(testgo | grep -c . || true)"
echo "LOC prod: $(loc "$(prodgo)")"
echo "LOC test: $(loc "$(testgo)")"
echo "packages: $(go -C "$src" list ./... 2>/dev/null | wc -l | tr -d ' ')"
echo "modules (go list -m all): $(go -C "$src" list -m all 2>/dev/null | wc -l | tr -d ' ')"

section "imports graph"
go -C "$src" list -f '{{.ImportPath}}: {{join .Imports " "}}' ./... 2>&1
mod=$(go -C "$src" list -m 2>/dev/null)

section "layer checks"
pkgs=$(go -C "$src" list ./... 2>/dev/null)
if echo "$pkgs" | grep -q '/internal/domain$'; then
  dom_imp=$(go -C "$src" list -f '{{join .Imports "\n"}}' ./internal/domain)
  bad=$(echo "$dom_imp" | grep -E "^($mod/|net/http|log/slog|encoding/json)|\." || true)
  echo "domain imports: $(echo $dom_imp)"
  echo "domain stdlib-only w/o net/http,slog,json: $([[ -z $bad ]] && echo PASS || echo "FAIL ($bad)")"
else
  echo "domain package: absent"
fi
if echo "$pkgs" | grep -q '/internal/usecase$'; then
  uc_imp=$(go -C "$src" list -f '{{join .Imports "\n"}}' ./internal/usecase)
  bad=$(echo "$uc_imp" | grep -E "/internal/(delivery|repository)|^net/http$|^encoding/json$" || true)
  echo "usecase imports: $(echo $uc_imp)"
  echo "usecase not importing delivery/repository/http/json: $([[ -z $bad ]] && echo PASS || echo "FAIL ($bad)")"
else
  echo "usecase package: absent"
fi
echo "third-party imports (non-stdlib, non-module):"
go -C "$src" list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./... 2>/dev/null | grep -v "^$mod" | sed 's/^/  /' || true
echo "  (end)"

section "grep checklist (prod code, excl. _test.go)"
chk() { printf '%-42s %s\n' "$1" "$(count "$2")"; }
chk 'http.DefaultClient'               'http\.DefaultClient'
chk 'http.Get('                        'http\.(Get|Post)\('
chk 'url.Values'                       'url\.Values'
chk 'url.JoinPath / .JoinPath('        'JoinPath\('
chk 'io.LimitReader'                   'io\.LimitReader'
chk 'signal.NotifyContext'             'signal\.NotifyContext'
chk '%w'                               '%w'
chk 'errors.Is'                        'errors\.Is\('
chk 'errors.As'                        'errors\.As\('
chk 'errors.Join'                      'errors\.Join\('
chk 'func init('                       'func init\('
chk 'log.Fatal'                        'log\.Fatal'
chk 'panic('                           'panic\('
chk 'recover()'                        'recover\(\)'
chk 'os.Exit'                          'os\.Exit\('
chk 'os.Getenv/LookupEnv'              'os\.(Getenv|LookupEnv)'
chk 'slog'                             'slog\.'
chk 'redact/sanitize (case-insens.)'   '[Rr]edact|[Ss]anitiz'
chk 'rate limiter impl (limiter|bucket|perChat|allow)' '[Ll]imiter|[Bb]ucket|perChat[A-Z]|\) allow\('
chk 'RateLimited (OWM 429 mapping)'    'RateLimited'
chk 'semaphore (sem chan|MaxConcurrency)'   'sem +chan|MaxConcurrency'
chk 'retry_after'                      'retry_after|RetryAfter'
chk 'parse_mode'                       'parse_mode'
chk 'context.Context'                  'context\.Context'
chk 'Timeout:'                         'Timeout:'
echo
echo "fmt.Sprintf( on lines with http|?|appid|url|/bot:"
where 'fmt\.Sprintf\(.*(http|\?|appid|[Uu][Rr][Ll]|/bot)' | sed 's/^/  /'
echo "os.Getenv locations:"
where 'os\.(Getenv|LookupEnv)' | sed 's/^/  /'
echo "redact/sanitize locations:"
where '[Rr]edact|[Ss]anitiz' | grep -E 'func ' | sed 's/^/  /'
echo "rate-limit locations:"
where '[Ll]imiter|[Bb]ucket|perChat[A-Z]|\) allow\(|MaxConcurrency' | sed 's/^/  /'
echo "recover/panic/log.Fatal locations:"
where 'recover\(\)|panic\(|log\.Fatal' | sed 's/^/  /'
echo "global var blocks (prod):"
where '^var ' | sed 's/^/  /'

section "run.json"
if [[ -f $run_json ]]; then
  jq -r '"num_turns: \(.num_turns)\nduration_ms: \(.duration_ms)\ntotal_cost_usd: \(.total_cost_usd)\nis_error: \(.is_error)"' "$run_json" 2>&1
else
  echo "run.json: missing ($run_json)"
fi
