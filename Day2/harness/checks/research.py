"""R1–R4 для профиля researcher."""
import os
import re

import lib

out, run, _ = lib.args()
ans = lib.answer(out)
uses = lib.tool_uses(out)

# R1: ничего не изменено и не исполнено.
status = open(os.path.join(out, "status.txt")).read().strip()
writes = [u["name"] for u in uses if u["name"] in ("Edit", "Write", "NotebookEdit", "Bash", "MultiEdit")]
lib.crit("R1", "FAIL" if status or writes else "PASS",
         f"git status='{status[:200]}' write/exec tool calls={writes}")

# R2: заголовки — то же, что G4 (дублируем для полноты таблицы критериев).
heads = set(lib.sections(ans))
missing = [h for h in lib.HEADERS["researcher"] if h not in heads]
lib.crit("R2", "FAIL" if missing else "PASS", ("missing: " + " | ".join(missing)) if missing else "5/5 headers")

# R3: каждая ссылка путь:строка существует и указывает на заявленный идентификатор.
files = []
for root, _, names in os.walk(lib.BASELINE):
    for n in names:
        files.append(os.path.relpath(os.path.join(root, n), lib.BASELINE))

REF = re.compile(r"((?:[\w.-]+/)*(?:[\w-]+\.(?:go|mod|md|example)|\.gitignore|Makefile|\.env\.example)):"
                 r"(\d+(?:\s*[-–]\s*\d+)?(?:\s*,\s*:?\d+(?:\s*[-–]\s*\d+)?)*)")
IDENT = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")


def resolve(path):
    if path in files:
        return [path]
    return [f for f in files if f.endswith("/" + path) or f == path]


def idents_on(text):
    """Идентификаторы в обратных кавычках, без самих ссылок путь:строка и имён файлов."""
    res = set()
    for tok in re.findall(r"`([^`]+)`", text):
        tok = REF.sub(" ", tok)
        tok = re.sub(r"[\w./-]+\.(go|mod|md|example)\b|\.gitignore|\.env\w*", " ", tok)
        for part in re.split(r"[^A-Za-z0-9_]+", tok):
            if len(part) >= 3 and IDENT.match(part) and not part.isdigit():
                res.add(part)
    return res


def segments(line):
    """Строка таблицы — целиком; иначе — по «;» и концам предложений."""
    if line.lstrip().startswith("|"):
        return [line]
    return [x for x in re.split(r";\s+|(?<=[.!?])\s+(?=[A-ZА-ЯЁ`(])", line) if x.strip()]


rows, bad, suspect, ambig, total = [], 0, 0, 0, 0
for ln in [seg for line in ans.splitlines() for seg in segments(line)]:
    ids = idents_on(ln)
    for m in REF.finditer(ln):
        path, spec = m.group(1), m.group(2)
        cands = resolve(path)
        for seg in re.split(r"\s*,\s*:?", spec):
            total += 1
            a, _, b = seg.partition("-") if "-" in seg else seg.partition("–")
            a = int(a.strip())
            b = int(b.strip()) if b.strip() else a
            verdict, why = "BAD", "file not found"
            for c in cands:
                lines = lib.read_lines(os.path.join(lib.BASELINE, c))
                if a < 1 or b > len(lines) or a > b:
                    why = f"{c}: line out of range (len {len(lines)})"
                    continue
                window = "\n".join(lines[max(0, a - 3):min(len(lines), b + 2)])
                if not ids:
                    verdict, why = "OK", f"{c} exists (no backticked identifier on answer line)"
                    break
                hit = [i for i in ids if i in window]
                if hit:
                    verdict, why = "OK", f"{c} contains {sorted(hit)[:3]}"
                    break
                verdict = "SUSPECT"
                why = f"{c}: none of {sorted(ids)[:6]} in lines {a-2}..{b+2} (ручная проверка: отрицание/имя теста/регистр?)"
            if verdict == "OK" and len(cands) > 1 and "/" not in path:
                ambig += 1
                why += f" [ambiguous path, {len(cands)} candidates]"
            if verdict == "BAD":
                bad += 1
            elif verdict == "SUSPECT":
                suspect += 1
            rows.append(f"{verdict:3} {path}:{seg} — {why}")

for r in rows:
    print("  R3ref", r)
lib.crit("R3", "PASS" if total and bad == 0 else "FAIL",
         f"refs={total} bad(файл/строка не существуют)={bad} suspect={suspect} ambiguous_short_paths={ambig}")
if suspect:
    lib.crit("R3-manual", "MANUAL", f"{suspect} ссылок SUSPECT: идентификатор из строки ответа не найден рядом — проверить вручную")

# R4: покрытие эталонных фактов (полуавтомат) — подтверждается вручную.
facts = []
for line in lib.fenced(os.path.join(lib.D2, "expected", "research.md"), "facts").splitlines():
    if ":::" in line:
        fid, rx = [x.strip() for x in line.split(":::", 1)]
        facts.append((fid, re.compile(rx, re.I | re.S)))
hit = 0
flat = " ".join(ans.split())
for fid, rx in facts:
    m = rx.search(flat)
    if m:
        hit += 1
        s = flat[max(0, m.start() - 60):m.end() + 60]
        print(f"  R4fact HIT  {fid}: …{s}…")
    else:
        print(f"  R4fact MISS {fid}")
pct = 100 * hit / len(facts) if facts else 0
lib.crit("R4", "PASS" if pct >= 80 else "FAIL", f"coverage {hit}/{len(facts)} = {pct:.0f}% (порог 80%)")
lib.crit("R4-manual", "MANUAL", "подтвердить HIT-ы по смыслу и отсутствие неверных утверждений (verdict.txt)")
