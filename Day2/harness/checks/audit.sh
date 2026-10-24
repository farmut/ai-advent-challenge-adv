#!/usr/bin/env bash
# Обёртка: python3 -I (изолированный режим) + путь к lib.py.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
exec python3 -I -c 'import sys,runpy; sys.path.insert(0, sys.argv[1]); sys.argv = sys.argv[2:]; runpy.run_path(sys.argv[0], run_name="__main__")' "$here" "$here/audit.py" "$@"
