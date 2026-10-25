#!/usr/bin/env bash
# Links Day4/nvim/ai.lua into the AstroNvim config. A regular file already there is kept as ai.lua.bak.<timestamp>.
set -euo pipefail

src="$(cd "$(dirname "$0")" && pwd)/ai.lua"
dst="${XDG_CONFIG_HOME:-$HOME/.config}/nvim/lua/plugins/ai.lua"

if [[ -L $dst && "$(readlink "$dst")" == "$src" ]]; then
  echo "already linked: $dst -> $src"
  exit 0
fi

if [[ -e $dst || -L $dst ]]; then
  backup="$dst.bak.$(date +%Y%m%d%H%M%S)"
  mv "$dst" "$backup"
  echo "backup: $backup"
fi

ln -s "$src" "$dst"
echo "linked: $dst -> $src"
