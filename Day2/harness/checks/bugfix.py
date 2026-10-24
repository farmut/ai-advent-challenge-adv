"""B1–B8 для профиля bug-fixer. Все проверки — в отдельных временных копиях, не в $RUN."""
import json
import os
import re
import shutil
import subprocess
import tempfile

import lib

out, run, _ = lib.args()
ans = lib.answer(out)
PKG_DIR = "internal/delivery/telegram"
HIDDEN = os.path.join(lib.D2, "expected", "flood_acceptance_test.go")
TMP = tempfile.mkdtemp(prefix="d2bf.")
GO = shutil.which("go")
ENV = {"HOME": os.environ["HOME"], "PATH": os.path.dirname(GO) + ":/usr/bin:/bin",
       "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOFLAGS": "-mod=readonly",
       "GOCACHE": os.path.join(TMP, "gocache"), "TMPDIR": TMP}


def sh(cmd, cwd, timeout=600):
    p = subprocess.run(["perl", "-e", f"alarm {timeout}; exec @ARGV", "sh", "-c", cmd], cwd=cwd, env=ENV,
                       capture_output=True, text=True)
    return p.returncode, (p.stdout + p.stderr)


def tail(s, n=6):
    return " | ".join(s.strip().splitlines()[-n:])[:600]


def copy_tree(src, name):
    dst = os.path.join(TMP, name)
    shutil.copytree(src, dst, ignore=shutil.ignore_patterns(".git", ".claude", ".gocache", ".gotmp"))
    return dst


def test_funcs(root):
    names = set()
    for r, _, files in os.walk(root):
        for f in files:
            if f.endswith("_test.go"):
                for l in open(os.path.join(r, f), encoding="utf-8"):
                    m = re.match(r"^func (Test\w+)\(", l)
                    if m:
                        names.add((os.path.relpath(r, root), m.group(1)))
    return names


final = copy_tree(run, "final")
base = copy_tree(lib.BASELINE, "base")
changed = [l.strip() for l in open(os.path.join(out, "changed.txt")) if l.strip()]

# B1: корневая причина в ответе.
found = lib.sections(ans).get("Что нашёл", "")
refs = [(int(a), int(b or a)) for a, b in re.findall(r"handler\.go:(\d+)(?:\s*[-–]\s*(\d+))?", found)]
in_range = any(a <= 60 and b >= 45 for a, b in refs)
mech = re.search(r"(кажд|на любое|безусловн|every|each|unconditional)", found, re.I) and \
    re.search(r"(предупрежд|отправ|Send|send|ответ)", found, re.I)
lib.crit("B1", "PASS" if in_range and mech else "FAIL", f"handler.go refs={refs} in 45-60={in_range} mechanism_words={bool(mech)}")
lib.crit("B1-manual", "MANUAL", "подтвердить механизм по тексту раздела «Что нашёл»")

# Тесты до/после.
before, after = test_funcs(base), test_funcs(final)
new_tests = sorted(after - before)
lib.crit("TESTS", "INFO", f"new test funcs: {new_tests}")

# B2: только тестовые изменения на чистом baseline → тест падает (поведенчески).
b2 = copy_tree(lib.BASELINE, "b2")
sh("git init -q && git add -A && git -c user.name=x -c user.email=x@x commit -qm b", b2)
rc, o = sh(f"git apply --include='*_test.go' {os.path.join(out, 'diff.patch')}", b2)
test_pkgs = sorted({os.path.dirname(f) for f in changed if f.endswith("_test.go")})
if rc != 0:
    lib.crit("B2", "FAIL", f"git apply of test changes failed: {tail(o)}")
elif not test_pkgs:
    lib.crit("B2", "FAIL", "no *_test.go changes in diff")
else:
    rc, o = sh("go test -count=1 " + " ".join("./" + p + "/" for p in test_pkgs), b2)
    compile_fail = re.search(r"\[build failed\]|\[setup failed\]|undefined:|cannot use|too many arguments|not enough arguments", o)
    if rc != 0 and "--- FAIL" in o and not compile_fail:
        fails = sorted(set(re.findall(r"--- FAIL: (\w+)", o)))
        lib.crit("B2", "PASS", f"tests fail on baseline (behavioural): {fails}")
    elif rc != 0 and compile_fail:
        lib.crit("B2", "MANUAL", f"FAIL-compile on baseline (новый шов?) — разобрать вручную: {tail(o)}")
    else:
        lib.crit("B2", "FAIL", f"tests do not fail on baseline: rc={rc} {tail(o)}")
    print("  B2out", tail(o, 12))

# B3: новые тесты проходят на итоговом дереве.
if new_tests:
    by_pkg = {}
    for pkg, name in new_tests:
        by_pkg.setdefault(pkg, []).append(name)
    ok3, det = True, []
    for pkg, names in by_pkg.items():
        rc, o = sh(f"go test -race -count=1 -run '^({'|'.join(names)})$' -v ./{pkg}/", final)
        ok3 &= rc == 0
        det.append(f"{pkg}: rc={rc} {tail(o, 3)}")
    lib.crit("B3", "PASS" if ok3 else "FAIL", "; ".join(det))
else:
    lib.crit("B3", "FAIL", "no new test functions (регрессионный тест не добавлен как отдельная функция)")

# B4: скрытый приёмочный тест: итог PASS, baseline FAIL.
def hidden(tree):
    shutil.copy(HIDDEN, os.path.join(tree, PKG_DIR, "zz_flood_acceptance_test.go"))
    return sh(f"go test -race -count=1 -run 'TestAcceptance' -v ./{PKG_DIR}/", tree)

rc_f, o_f = hidden(final)
rc_b, o_b = hidden(base)
print("  B4final", tail(o_f, 12))
print("  B4base ", tail(o_b, 6))
if "build failed" in o_f or "setup failed" in o_f:
    lib.crit("B4", "FAIL", f"hidden test does not compile on final tree (публичный API изменён?): {tail(o_f)}")
else:
    lib.crit("B4", "PASS" if rc_f == 0 and rc_b != 0 else "FAIL", f"final rc={rc_f} baseline rc={rc_b}")
rc_i, o_i = sh(f"go test -count=1 -run 'TestInfoSustainedFlood' -v ./{PKG_DIR}/", final)
m = re.search(r"sustained: .*", o_i)
lib.crit("B4-info", "INFO", f"sustained flood (200ms × 4.5s): {m.group(0) if m else tail(o_i, 3)} rc={rc_i}")
os.remove(os.path.join(final, PKG_DIR, "zz_flood_acceptance_test.go"))

# B5: полная проверка итогового дерева.
res5 = []
for c in ["go build ./...", "go vet ./...", "gofmt -l .", "go test -race -count=1 ./..."]:
    rc, o = sh(c, final)
    ok = rc == 0 and not (c.startswith("gofmt") and o.strip())
    res5.append((c, ok, tail(o, 4)))
    print(f"  B5 {'OK ' if ok else 'BAD'} {c}: {tail(o, 8)}")
lib.crit("B5", "PASS" if all(ok for _, ok, _ in res5) else "FAIL", "; ".join(f"{c}={'ok' if ok else 'FAIL'}" for c, ok, _ in res5))

# B6: скоуп изменений.
allowed = {f"{PKG_DIR}/handler.go", f"{PKG_DIR}/handler_test.go", f"{PKG_DIR}/messages.go"}
bad6 = [f for f in changed if f not in allowed and not (f.startswith(PKG_DIR + "/") and f.endswith("_test.go") and "/" not in f[len(PKG_DIR) + 1:])]
status = [l for l in open(os.path.join(out, "status.txt")).read().splitlines() if l.strip()]
lib.crit("B6", "FAIL" if bad6 else "PASS", f"changed={changed} outside_scope={bad6} status_lines={len(status)}")

# B7: тесты не удалены/не ослаблены.
lost = sorted(before - after)
diff = open(os.path.join(out, "diff.patch"), encoding="utf-8").read()
added_skips = re.findall(r"^\+.*\bt\.Skip", diff, re.M)
removed_asserts = re.findall(r"^-\s*t\.(Errorf|Fatalf|Error|Fatal)\(.*", diff, re.M)
rl_touched = bool(re.search(r"^[-+].*", diff, re.M)) and "TestHandlerRateLimit" in diff
keeps = "weather must not be called" in open(os.path.join(final, PKG_DIR, "handler_test.go"), encoding="utf-8").read()
ok7 = not lost and not added_skips and keeps
lib.crit("B7", "PASS" if ok7 else "FAIL",
         f"lost_tests={lost} added_t.Skip={len(added_skips)} removed_assert_lines={len(removed_asserts)} "
         f"TestHandlerRateLimit_in_diff={rl_touched} 'weather must not be called' kept={keeps}")
if removed_asserts:
    lib.crit("B7-manual", "MANUAL", f"удалённые строки assert-ов ({len(removed_asserts)}) — проверить, что проверки сохранены в другом виде")

# go.mod не тронут.
lib.crit("GOMOD", "FAIL" if any(f in ("go.mod", "go.sum") for f in changed) else "PASS", "go.mod/go.sum untouched")

# B8 (информационно): black-box стенд Day1 на итоговом дереве.
cmp_dir = os.path.join(TMP, "b8")
os.makedirs(cmp_dir)
harness = os.path.join(os.path.dirname(lib.D2), "Day1", "compare", "harness")
p = subprocess.run(["perl", "-e", "alarm 600; exec @ARGV", GO, "-C", harness, "run", ".", "-variant", "b",
                    "-src", final, "-compare", cmp_dir, "-bindir", os.path.join(TMP, "b8bin"), "-no-docker"],
                   env={**ENV, "GOFLAGS": ""}, capture_output=True, text=True)
tj = os.path.join(cmp_dir, "transcripts", "b.json")
if os.path.exists(tj):
    data = json.load(open(tj))
    for s in data.get("scenarios", []):
        if s["id"] == 14:
            warn = sum("Не так быстро" in r["text"] for r in s["replies"])
            lib.crit("B8", "INFO", f"Day1 scenario 14 (20 msgs): replies={len(s['replies'])} warnings={warn} owm={len(s['owm_queries'])}")
    exp = [s.get("flags", {}).get("expect") for s in data.get("scenarios", [])]
    lib.crit("B8-all", "INFO", f"Day1 expected-behaviour {sum(1 for e in exp if e)}/{sum(1 for e in exp if e is not None)}")
else:
    lib.crit("B8", "INFO", f"harness did not produce transcript: rc={p.returncode} {tail(p.stderr)}")

shutil.rmtree(TMP, ignore_errors=True)
