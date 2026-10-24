"""Общие функции проверок: разбор stream-json транскрипта и вывод критериев.

Формат вывода каждой проверки — строки
    CRIT <ID> <PASS|FAIL|INFO|MANUAL> <детали>
verdict.sh собирает их в verdict.txt: любой FAIL → FAIL попытки.
"""
import json
import os
import re
import sys

D2 = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
BASELINE = os.path.join(D2, "src")

HEADERS = {
    "bug-fixer": ["Что нашёл", "Что починил", "Регрессионный тест", "Что проверил", "Вне скоупа / риски"],
    "researcher": ["Краткий ответ", "Файлы (file:line)", "Связи / поток", "Выводы", "Неопределённости"],
    "tech-writer": [
        "Созданные/изменённые файлы",
        "Источники (ключевые факты → file:line)",
        "Исправленные неточности прежней документации",
        "Что не задокументировано и почему",
    ],
}


def crit(cid, status, detail=""):
    detail = " ".join(str(detail).split())
    print(f"CRIT {cid} {status} {detail}".rstrip())


def events(out):
    path = os.path.join(out, "transcript.jsonl")
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                yield json.loads(line)
            except json.JSONDecodeError:
                continue


def init_event(out):
    for e in events(out):
        if e.get("type") == "system" and e.get("subtype") == "init":
            return e
    return {}


def tool_uses(out):
    """Все вызовы инструментов: dict(id, name, input)."""
    res = []
    for e in events(out):
        if e.get("type") != "assistant":
            continue
        for c in (e.get("message") or {}).get("content") or []:
            if c.get("type") == "tool_use":
                res.append({"id": c.get("id"), "name": c.get("name"), "input": c.get("input") or {}})
    return res


def tool_results(out):
    """tool_use_id -> (is_error, text)."""
    res = {}
    for e in events(out):
        if e.get("type") != "user":
            continue
        content = (e.get("message") or {}).get("content")
        if not isinstance(content, list):
            continue
        for c in content:
            if c.get("type") != "tool_result":
                continue
            body = c.get("content")
            if isinstance(body, list):
                body = "\n".join(x.get("text", "") for x in body if isinstance(x, dict))
            res[c.get("tool_use_id")] = (bool(c.get("is_error")), body or "")
    return res


def result(out):
    try:
        with open(os.path.join(out, "run.json"), encoding="utf-8") as f:
            return json.loads(f.read() or "{}")
    except (OSError, json.JSONDecodeError):
        return {}


def answer(out):
    try:
        with open(os.path.join(out, "answer.md"), encoding="utf-8") as f:
            return f.read()
    except OSError:
        return ""


def sections(text):
    """Заголовок второго уровня -> тело."""
    res, cur = {}, None
    for line in text.splitlines():
        m = re.match(r"^##\s+(.+?)\s*$", line)
        if m:
            cur = m.group(1)
            res[cur] = ""
        elif cur is not None:
            res[cur] += line + "\n"
    return res


def read_lines(path):
    with open(path, encoding="utf-8", errors="replace") as f:
        return f.read().splitlines()


def fenced(path, lang):
    """Содержимое блока ```lang из markdown-файла."""
    text = open(path, encoding="utf-8").read()
    m = re.search(r"```" + re.escape(lang) + r"\n(.*?)```", text, re.S)
    return m.group(1) if m else ""


def args():
    if len(sys.argv) < 3:
        sys.exit("usage: <attempt-dir> <run-dir> [profile]")
    return sys.argv[1], sys.argv[2], (sys.argv[3] if len(sys.argv) > 3 else None)
