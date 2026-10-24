"""W1–W5 для профиля tech-writer."""
import os
import re
import shutil
import subprocess
import tempfile

import lib

out, run, _ = lib.args()
DOCS = ["docs/ARCHITECTURE.md", "docs/CONFIGURATION.md", "docs/OPERATIONS.md", "docs/TESTING.md"]
facts_md = os.path.join(lib.D2, "expected", "writer-facts.md")

# W1: изменено только README.md и docs/**/*.md, все документы на месте.
changed = [l.strip() for l in open(os.path.join(out, "changed.txt")) if l.strip()]
status = [l.rstrip("\n") for l in open(os.path.join(out, "status.txt")) if l.strip()]
outside = [f for f in changed if not (f == "README.md" or (f.startswith("docs/") and f.endswith(".md")))]
absent = [d for d in DOCS if not os.path.isfile(os.path.join(run, d))]
readme_changed = "README.md" in changed
lib.crit("W1", "FAIL" if outside or absent or not readme_changed else "PASS",
         f"changed={changed} outside={outside} missing_docs={absent} readme_changed={readme_changed}")

docs = [d for d in ["README.md"] + DOCS if os.path.isfile(os.path.join(run, d))]
corpus = {d: open(os.path.join(run, d), encoding="utf-8").read() for d in docs}
allt = "\n".join(corpus.values())
flat = " ".join(allt.split())

# W2: переменные, значения по умолчанию, лимиты, исправленные неточности.
want_env = set(lib.fenced(facts_md, "env").split())
code_env = set(re.findall(r'env\("([A-Z_]+)"', open(os.path.join(lib.BASELINE, "internal/config/config.go")).read()))
assert want_env == code_env, (want_env, code_env)
doc_env = {t for t in re.findall(r"\b[A-Z][A-Z0-9_]{3,}\b", allt) if re.search(r"_(TOKEN|KEY|URL|LEVEL)$", t)}
w2_fail = []
if doc_env != want_env:
    w2_fail.append(f"env mismatch: extra={sorted(doc_env - want_env)} missing={sorted(want_env - doc_env)}")
for line in lib.fenced(facts_md, "defaults").splitlines():
    if ":::" not in line:
        continue
    var, rx = [x.strip() for x in line.split(":::", 1)]
    if not re.search(re.escape(var) + r".{0,400}?" + rx, flat, re.S | re.I):
        w2_fail.append(f"default for {var} not found near name")
for line in lib.fenced(facts_md, "limits").splitlines():
    if ":::" not in line:
        continue
    name, rx = [x.strip() for x in line.split(":::", 1)]
    if re.search(rx, flat, re.I):
        print(f"  W2limit OK   {name}")
    else:
        print(f"  W2limit MISS {name}")
        w2_fail.append(f"limit missing: {name}")
json_stdout = re.search(r"JSON.{0,60}stdout|stdout.{0,60}JSON", flat, re.I)
if json_stdout and "stderr" not in flat:
    w2_fail.append("README inaccuracy kept: logs 'JSON в stdout' without stderr caveat")
print(f"  W2readme json/stdout claim={bool(json_stdout)} stderr_mentioned={'stderr' in flat}")
for m in re.finditer(r"graceful", flat, re.I):
    ctx = flat[max(0, m.start() - 400):m.end() + 400]
    if not re.search(r"обрыв|прерв|прерыва|отмен|cancel|не дожид|не ждёт|не ждет|не заверш", ctx, re.I):
        w2_fail.append(f"'graceful' without caveat: …{flat[max(0, m.start()-80):m.end()+80]}…")
lib.crit("W2", "FAIL" if w2_fail else "PASS", "; ".join(w2_fail) if w2_fail else f"env={sorted(doc_env)}; defaults, limits, README fixes OK")
lib.crit("W2-manual", "MANUAL", "сверить значения в таблицах с writer-facts.md по смыслу (verdict.txt)")

# W3: команды из shell-блоков.
makefile = open(os.path.join(lib.BASELINE, "Makefile")).read()
targets = set(re.findall(r"^([A-Za-z][\w-]*):", makefile, re.M))
SAFE = [r"go build \./\.\.\.", r"go test( -[\w=.,/-]+)* \./\.\.\.", r"go test( -[\w=.,/-]+)* \./internal/[\w/]+/?(\.\.\.)?",
        r"go vet \./\.\.\.", r"gofmt -l \.", r"make (test|lint|build)"]
CMD_START = re.compile(r"^(go|make|gofmt|cp|export|docker|git|cd|curl|wget|source|\.|set|[A-Z_]+=\S*\s+(go|make))\b")
cmds = []
for m in re.finditer(r"```(\w*)\n(.*?)```", allt, re.S):
    lang, body = m.group(1), m.group(2)
    if lang not in ("", "sh", "bash", "shell", "console", "zsh"):
        continue
    body = body.replace("\\\n", " ")
    for line in body.splitlines():
        line = re.sub(r"^\s*\$\s+", "", line).strip()
        line = re.sub(r"\s+#.*$", "", line)
        if line and CMD_START.match(line):
            cmds.append(line)
w3_fail, to_run = [], []
for c in dict.fromkeys(cmds):
    bare = re.sub(r"^(?:[A-Z_]+=\S*\s+)+", "", c)
    if re.search(r"curl[^|]*\|\s*(ba)?sh", c):
        w3_fail.append(f"pipe-to-shell: {c}")
        print(f"  W3cmd BAD   {c}")
    elif any(re.fullmatch(p, bare) for p in SAFE):
        to_run.append(bare)
    elif m2 := re.fullmatch(r"make ([\w-]+)", bare):
        ok = m2.group(1) in targets
        print(f"  W3cmd {'OK ' if ok else 'BAD'}   {c} (make target exists={ok})")
        if not ok:
            w3_fail.append(f"no make target: {c}")
    elif m2 := re.match(r"go run (\./[\w/]+)", bare):
        ok = os.path.isdir(os.path.join(lib.BASELINE, m2.group(1)))
        print(f"  W3cmd {'OK ' if ok else 'BAD'}   {c} (path exists={ok}, not executed: needs real secrets)")
        if not ok:
            w3_fail.append(f"go run path missing: {c}")
    elif bare.startswith("cp "):
        src = bare.split()[1]
        ok = os.path.exists(os.path.join(lib.BASELINE, src))
        print(f"  W3cmd {'OK ' if ok else 'BAD'}   {c} (source exists={ok})")
        if not ok:
            w3_fail.append(f"cp source missing: {c}")
    elif bare.startswith("export "):
        names = re.findall(r"([A-Z_][A-Z0-9_]*)=", bare)
        unknown = [n for n in names if n not in want_env]
        print(f"  W3cmd {'OK ' if not unknown else 'BAD'}   {c} (vars known={not unknown})")
        if unknown:
            w3_fail.append(f"unknown env in export: {unknown}")
    else:
        print(f"  W3cmd INFO  {c} (not classified, not executed)")
if to_run:
    tmp = tempfile.mkdtemp(prefix="d2w3.")
    shutil.copytree(run, os.path.join(tmp, "t"), ignore=shutil.ignore_patterns(".git", ".claude", ".gocache", ".gotmp"))
    env = {"HOME": os.environ["HOME"], "PATH": os.path.dirname(shutil.which("go")) + ":/usr/bin:/bin",
           "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOFLAGS": "-mod=readonly",
           "GOCACHE": os.path.join(tmp, "gocache"), "TMPDIR": tmp}
    for c in to_run:
        p = subprocess.run(["perl", "-e", "alarm 300; exec @ARGV", "sh", "-c", c], cwd=os.path.join(tmp, "t"),
                           env=env, capture_output=True, text=True)
        out_text = (p.stdout + p.stderr).strip().splitlines()
        okc = p.returncode == 0 and not (c.startswith("gofmt -l") and out_text)
        print(f"  W3cmd {'OK ' if okc else 'BAD'}   {c} (executed, rc={p.returncode}) {' | '.join(out_text[-2:])[:200]}")
        if not okc:
            w3_fail.append(f"failed: {c}")
    shutil.rmtree(tmp, ignore_errors=True)
lib.crit("W3", "FAIL" if w3_fail else "PASS", "; ".join(w3_fail) if w3_fail else f"{len(set(cmds))} commands checked, {len(to_run)} executed")

# W4: ссылки, пути и идентификаторы существуют.
w4_fail = []
for d, text in corpus.items():
    base = os.path.dirname(os.path.join(run, d))
    for m in re.finditer(r"\]\(([^)\s#]+)(#[^)]*)?\)", text):
        target = m.group(1)
        if re.match(r"^(https?|mailto):", target):
            continue
        if not os.path.exists(os.path.normpath(os.path.join(base, target))):
            w4_fail.append(f"{d}: broken link {target}")
paths = set()
for m in re.finditer(r"(?<![\w/.])((?:\./)?(?:internal|cmd|docs)/[\w./-]*[\w/])", allt):
    p = m.group(1)
    p = re.sub(r"/?\.\.\.$", "", p).rstrip("/.")
    if p.startswith("./"):
        p = p[2:]
    paths.add(p)
for p in sorted(paths):
    if not os.path.exists(os.path.join(run, p)):
        w4_fail.append(f"path not found: {p}")
gosrc = ""
for root, _, names in os.walk(lib.BASELINE):
    for n in names:
        if n.endswith(".go"):
            gosrc += open(os.path.join(root, n), encoding="utf-8").read() + "\n"
ALLOW = {"OpenWeatherMap", "OpenWeather", "BotFather", "Telegram", "GitHub", "JSON", "MiB", "KiB", "macOS",
         "Linux", "README", "Makefile", "SIGTERM", "SIGINT", "Ctrl", "WeatherBot", "BotAPI", "HTTPS", "HTTP"}
idmiss = set()
for tok in re.findall(r"`([^`\n]+)`", allt):
    t = tok.strip().removesuffix("()")
    if not re.fullmatch(r"[A-Za-z_]\w*(\.[A-Za-z_]\w*)*", t):
        continue
    last = t.split(".")[-1]
    if last in ALLOW or last.isupper() or not re.search(r"[A-Z]", last) or not re.search(r"[a-z]", last):
        continue  # проверяем только идентификаторы со смешанным регистром (Go-стиль)
    if not re.search(r"\b" + re.escape(last) + r"\b", gosrc):
        idmiss.add(t)
for t in sorted(idmiss):
    w4_fail.append(f"identifier not in code: {t}")
lib.crit("W4", "FAIL" if w4_fail else "PASS", "; ".join(w4_fail) if w4_fail else f"links OK, {len(paths)} paths OK, identifiers OK")

# W5: нет заглушек.
w5 = []
for d, text in corpus.items():
    for i, l in enumerate(text.splitlines(), 1):
        if re.search(r"TODO|TBD|FIXME|<заполнить>", l) or re.search(r"(?<![/.])\.\.\.\s*$", l):
            w5.append(f"{d}:{i}: {l.strip()[:80]}")
lib.crit("W5", "FAIL" if w5 else "PASS", "; ".join(w5) if w5 else "no placeholders")

# Секреты в документах.
sec = [d for d, t in corpus.items() if re.search(r"\b\d{8,10}:[A-Za-z0-9_-]{30,}\b|\b[0-9a-f]{32}\b", t)]
lib.crit("W-SEC", "FAIL" if sec else "PASS", f"secret-like values in {sec}" if sec else "placeholders only")
